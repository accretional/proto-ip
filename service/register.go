// Package service is the importable entrypoint for composing proto-ip's
// LocalLookup, RDAPLookup and GeoLookup services onto a shared *grpc.Server (the
// proto-go "Register" convention). The stock cmd/server defines its LocalLookup
// impl in package main; this package re-hosts it so it can be composed.
package service

import (
	"context"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/accretional/proto-ip/geoip"
	"github.com/accretional/proto-ip/localip"
	pb "github.com/accretional/proto-ip/proto/ippb"
	"github.com/accretional/proto-ip/rdap"
)

// GeoDataDirEnv names the env var pointing at the geo data directory (iptoasn
// tsvs, DB-IP mmdb, anycast prefix lists, rpki-vrps.json). Files are loaded
// best-effort — whatever is present is used; missing files degrade gracefully.
const GeoDataDirEnv = "PROTO_IP_GEO_DATA"

// Register registers all three proto-ip services on s.
//
//   - LocalLookup is always registered (reads local kernel state).
//   - RDAPLookup + GeoLookup use the IANA RDAP bootstrap registry (network). If
//     bootstrap fails, RDAPLookup is skipped and RDAP-derived geo enrichment is
//     disabled, but GeoLookup still registers with any file-backed sources.
//
// GeoLookup loads every geo source found under $PROTO_IP_GEO_DATA (default
// ./data/geoip): DB-IP City Lite + RIPE IPmap + IP2Location LITE (coordinates),
// iptoasn (ASN + network name — this is what unlocks the RDAP autnum enrichment
// and RPKI), geofeeds (RDAP-discovered), plus anycast classification and RPKI
// origin validation, and reverse DNS. Mirrors cmd/geo-server.
func Register(s *grpc.Server) {
	pb.RegisterLocalLookupServer(s, &localLookupServer{})

	dataDir := os.Getenv(GeoDataDirEnv)
	if dataDir == "" {
		dataDir = "data/geoip"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	boot, bootErr := rdap.NewBootstrap(ctx)
	cancel()
	var rdapClient *rdap.Client
	if bootErr != nil {
		log.Printf("proto-ip: RDAP bootstrap failed (%v); RDAPLookup + geo AS enrichment/geofeeds disabled", bootErr)
	} else {
		rdapClient = rdap.NewClient(boot)
		pb.RegisterRDAPLookupServer(s, rdap.NewServer(rdapClient))
	}

	var sources []geoip.Source
	if p, err := geoip.FindIPMapDatabase(dataDir); err == nil {
		if src, err := geoip.NewIPMapSource(p); err == nil {
			sources = append(sources, src)
			log.Printf("proto-ip geo: RIPE IPmap loaded (%s)", p)
		}
	}
	if p, err := geoip.FindDBIPDatabase(dataDir); err == nil {
		if src, err := geoip.NewDBIPSource(p); err == nil {
			sources = append(sources, src)
			log.Printf("proto-ip geo: DB-IP City Lite loaded (%s)", p)
		}
	}
	if p, err := geoip.FindIP2LocationDatabase(dataDir); err == nil {
		if src, err := geoip.NewIP2LocationSource(p); err == nil {
			sources = append(sources, src)
			log.Printf("proto-ip geo: IP2Location LITE loaded (%s)", p)
		}
	}
	if v4, v6, ok := geoip.FindIPtoASNDatabases(dataDir); ok {
		if src, err := geoip.NewIPtoASNSource(v4, v6); err != nil {
			log.Printf("proto-ip geo: iptoasn load failed: %v", err)
		} else {
			sources = append(sources, src)
			log.Printf("proto-ip geo: iptoasn loaded (%s)", src.Summary())
		}
	} else {
		log.Printf("proto-ip geo: iptoasn tsvs not found in %s — ASN/network + RDAP autnum enrichment will be empty", dataDir)
	}
	if rdapClient != nil {
		sources = append(sources, geoip.NewGeofeedSource(rdapClient))
	}

	geo := geoip.NewServer(sources...)
	if v4, v6, ok := geoip.FindAnycastFiles(dataDir); ok {
		if a, err := geoip.NewAnycastSet(v4, v6); err == nil {
			geo = geo.WithAnycast(a)
			log.Printf("proto-ip geo: anycast classifier enabled (%d prefixes)", a.Len())
		}
	}
	if p, err := geoip.FindRPKIDatabase(dataDir); err == nil {
		if r, err := geoip.NewRPKISet(p); err == nil {
			geo = geo.WithRPKI(r)
			log.Printf("proto-ip geo: RPKI validation enabled (%d VRPs)", r.Len())
		}
	}
	if rdapClient != nil {
		geo = geo.WithASNEnrichment(rdapClient)
	}
	geo = geo.WithReverseDNS(nil) // net.DefaultResolver
	pb.RegisterGeoLookupServer(s, geo)
}

// localLookupServer serves the LocalLookup service backed by the localip
// package. Mirrors the impl in cmd/server/main.go, which lives in package main
// and is therefore not importable.
type localLookupServer struct {
	pb.UnimplementedLocalLookupServer
}

func (s *localLookupServer) ListInterfaces(req *pb.LookupFilter, stream grpc.ServerStreamingServer[pb.Interface]) error {
	ifs, err := localip.List(req)
	if err != nil {
		return err
	}
	for _, iface := range ifs {
		if err := stream.Send(iface); err != nil {
			return err
		}
	}
	return nil
}

func (s *localLookupServer) ListIPs(req *pb.LookupFilter, stream grpc.ServerStreamingServer[pb.IP]) error {
	ifs, err := localip.List(req)
	if err != nil {
		return err
	}
	for _, iface := range ifs {
		for _, c := range iface.GetAddresses() {
			if err := stream.Send(c.GetIp()); err != nil {
				return err
			}
		}
	}
	return nil
}
