// Command rdap-server runs the RDAPLookup gRPC service. It fetches
// the IANA bootstrap registry on startup, then serves LookupIP and
// LookupCIDR RPCs that route to the appropriate Regional Internet
// Registry and return structured RDAP data.
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

	"github.com/accretional/proto-ip/rdap"
	pb "github.com/accretional/proto-ip/proto/ippb"
)

func main() {
	port := flag.Int("port", 50098, "TCP port to bind the RDAPLookup gRPC server")
	flag.Parse()

	log.Println("Fetching IANA RDAP bootstrap registry…")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	boot, err := rdap.NewBootstrap(ctx)
	cancel()
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	log.Println("Bootstrap loaded.")

	client := rdap.NewClient(boot)
	srv := grpc.NewServer()
	pb.RegisterRDAPLookupServer(srv, rdap.NewServer(client))

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

	log.Printf("RDAPLookup listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
