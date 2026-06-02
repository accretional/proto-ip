// Command geo-server runs the GeoLookup gRPC service. It combines two free,
// open geolocation sources: the DB-IP City Lite database (coordinates) and
// RFC 8805 geofeeds discovered via RDAP (authoritative, coarse). A missing
// source degrades coverage but does not stop the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/accretional/proto-ip/geoip"
	"github.com/accretional/proto-ip/rdap"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

func main() {
	port := flag.Int("port", 50099, "TCP port to bind the GeoLookup gRPC server")
	dataDir := flag.String("data-dir", "data/geoip", "directory holding the DB-IP City Lite MMDB")
	flag.Parse()

	var sources []geoip.Source

	// RIPE IPmap source (optional). Listed first so its measured coordinates
	// win granularity ties over the estimate-based DB-IP for the infrastructure
	// addresses it covers.
	if path, err := geoip.FindIPMapDatabase(*dataDir); err != nil {
		log.Printf("RIPE IPmap dump unavailable (%v)", err)
	} else if src, err := geoip.NewIPMapSource(path); err != nil {
		log.Printf("loading RIPE IPmap: %v", err)
	} else {
		log.Printf("RIPE IPmap source loaded from %s (%d addresses)", path, src.Len())
		sources = append(sources, src)
	}

	// DB-IP coordinate source (optional — geofeed-only if absent).
	if path, err := geoip.FindDBIPDatabase(*dataDir); err != nil {
		log.Printf("DB-IP database unavailable (%v); running without coordinates", err)
	} else if src, err := geoip.NewDBIPSource(path); err != nil {
		log.Printf("opening DB-IP database: %v; running without coordinates", err)
	} else {
		log.Printf("DB-IP source loaded from %s", path)
		sources = append(sources, src)
	}

	// IP2Location LITE DB9 (optional, opt-in; CC BY-SA 4.0). Loaded only when
	// the MMDB is present in the cache (downloaded by setup.sh given a token).
	if path, err := geoip.FindIP2LocationDatabase(*dataDir); err == nil {
		if src, err := geoip.NewIP2LocationSource(path); err != nil {
			log.Printf("loading IP2Location LITE: %v", err)
		} else {
			log.Printf("IP2Location LITE source loaded from %s", path)
			sources = append(sources, src)
		}
	}

	// iptoasn BGP-derived source (ASN + country floor; public domain).
	if v4, v6, ok := geoip.FindIPtoASNDatabases(*dataDir); ok {
		if src, err := geoip.NewIPtoASNSource(v4, v6); err != nil {
			log.Printf("loading iptoasn: %v", err)
		} else {
			log.Printf("iptoasn source loaded (%s)", src.Summary())
			sources = append(sources, src)
		}
	}

	// Geofeed source via the IANA RDAP bootstrap registry.
	log.Println("Fetching IANA RDAP bootstrap registry…")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	boot, err := rdap.NewBootstrap(ctx)
	cancel()
	if err != nil {
		log.Printf("RDAP bootstrap failed (%v); geofeed discovery disabled", err)
	} else {
		sources = append(sources, geoip.NewGeofeedSource(rdap.NewClient(boot)))
		log.Println("Geofeed source enabled.")
	}

	if len(sources) == 0 {
		log.Fatal("no geolocation sources available; cannot start")
	}

	server := geoip.NewServer(sources...)

	// Optional anycast classifier (bgp.tools prefix lists): flags anycast
	// addresses and forces their confidence to LOW.
	if v4, v6, ok := geoip.FindAnycastFiles(*dataDir); ok {
		if a, err := geoip.NewAnycastSet(v4, v6); err != nil {
			log.Printf("loading anycast prefixes: %v", err)
		} else {
			server = server.WithAnycast(a)
			log.Printf("anycast classifier enabled (%d prefixes)", a.Len())
		}
	}

	srv := grpc.NewServer()
	pb.RegisterGeoLookupServer(srv, server)

	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		<-sigs
		srv.GracefulStop()
	}()

	log.Printf("GeoLookup listening on %s (%d source(s))", addr, len(sources))
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
