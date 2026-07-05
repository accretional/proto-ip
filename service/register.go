// Package service is the importable entrypoint for composing proto-ip's
// LocalLookup, RDAPLookup and GeoLookup services onto a shared *grpc.Server (the
// proto-go "Register" convention). The stock cmd/server defines its LocalLookup
// impl in package main; this package re-hosts it so it can be composed.
package service

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"

	"github.com/accretional/proto-ip/geoip"
	"github.com/accretional/proto-ip/localip"
	pb "github.com/accretional/proto-ip/proto/ippb"
	"github.com/accretional/proto-ip/rdap"
)

// Register registers all three proto-ip services on s.
//
//   - LocalLookup is always registered (reads local kernel state).
//   - RDAPLookup and GeoLookup depend on the IANA RDAP bootstrap registry
//     (network). If bootstrap fails, RDAPLookup is skipped and GeoLookup is
//     registered with no sources (best-effort/empty) so the pipeline still runs.
//
// GeoLookup uses network-only enrichers (geofeed discovery via RDAP, RDAP autnum
// enrichment, reverse DNS) — no local geo data files are required.
func Register(s *grpc.Server) {
	pb.RegisterLocalLookupServer(s, &localLookupServer{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	boot, err := rdap.NewBootstrap(ctx)
	cancel()
	if err != nil {
		log.Printf("proto-ip: RDAP bootstrap failed (%v); RDAPLookup skipped, GeoLookup empty", err)
		pb.RegisterGeoLookupServer(s, geoip.NewServer())
		return
	}

	client := rdap.NewClient(boot)
	pb.RegisterRDAPLookupServer(s, rdap.NewServer(client))

	geo := geoip.NewServer(geoip.NewGeofeedSource(client)).
		WithASNEnrichment(client).
		WithReverseDNS(nil) // net.DefaultResolver
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
