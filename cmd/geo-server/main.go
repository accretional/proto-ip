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

	// DB-IP coordinate source (optional — geofeed-only if absent).
	if path, err := geoip.FindDBIPDatabase(*dataDir); err != nil {
		log.Printf("DB-IP database unavailable (%v); running without coordinates", err)
	} else if src, err := geoip.NewDBIPSource(path); err != nil {
		log.Printf("opening DB-IP database: %v; running without coordinates", err)
	} else {
		log.Printf("DB-IP source loaded from %s", path)
		sources = append(sources, src)
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

	srv := grpc.NewServer()
	pb.RegisterGeoLookupServer(srv, geoip.NewServer(sources...))

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
