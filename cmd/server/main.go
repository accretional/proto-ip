// Command server runs the LocalLookup gRPC service backed by the
// localip package. It binds a single TCP port and exits on SIGINT.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"github.com/accretional/proto-ip/localip"
	pb "github.com/accretional/proto-ip/proto/ippb"
)

func main() {
	port := flag.Int("port", 50097, "TCP port to bind the LocalLookup gRPC server")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	srv := grpc.NewServer()
	pb.RegisterLocalLookupServer(srv, &localLookupServer{})

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		<-sigs
		srv.GracefulStop()
	}()

	log.Printf("LocalLookup listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

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
