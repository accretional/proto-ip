// Command client is a tiny CLI for the LocalLookup gRPC service.
// Used by LET_IT_RIP.sh to verify the server returns real local IPs.
//
// Usage:
//
//	client -addr localhost:50097 interfaces
//	client -addr localhost:50097 ips
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

func main() {
	addr := flag.String("addr", "localhost:50097", "LocalLookup server address")
	timeout := flag.Duration("timeout", 5*time.Second, "request timeout")
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: client [-addr HOST:PORT] [-timeout D] (interfaces|ips)")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()
	client := pb.NewLocalLookupClient(conn)

	switch args[0] {
	case "interfaces":
		if err := printInterfaces(ctx, client); err != nil {
			log.Fatalf("interfaces: %v", err)
		}
	case "ips":
		if err := printIPs(ctx, client); err != nil {
			log.Fatalf("ips: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func printInterfaces(ctx context.Context, c pb.LocalLookupClient) error {
	stream, err := c.ListInterfaces(ctx, &pb.LookupFilter{})
	if err != nil {
		return err
	}
	for {
		iface, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		mac := ""
		if hw := iface.GetHardwareAddress(); len(hw) > 0 {
			mac = " mac=" + hex.EncodeToString(hw)
		}
		fmt.Printf("%-20s class=%s up=%t%s\n",
			iface.GetName(),
			strings.TrimPrefix(iface.GetClass().String(), "INTERFACE_CLASS_"),
			iface.GetUp(),
			mac)
		for _, c := range iface.GetAddresses() {
			fmt.Printf("    %s/%d\n", renderIP(c.GetIp()), c.GetSubnet().GetPrefixLength())
		}
	}
}

func printIPs(ctx context.Context, c pb.LocalLookupClient) error {
	stream, err := c.ListIPs(ctx, &pb.LookupFilter{})
	if err != nil {
		return err
	}
	for {
		ip, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		family := "IPv6"
		if _, ok := ip.GetVersion().(*pb.IP_V4); ok {
			family = "IPv4"
		}
		fmt.Printf("%s %s\n", family, renderIP(ip))
	}
}

// renderIP rebuilds a net.IP from the 128-bit halves and prints its
// canonical text form (mirrors net.IP.String for both families).
func renderIP(ip *pb.IP) string {
	buf := make(net.IP, 16)
	binary.BigEndian.PutUint64(buf[0:8], uint64(ip.GetNetworkPrefix()))
	binary.BigEndian.PutUint64(buf[8:16], uint64(ip.GetInterfaceIdentifier()))
	if v4 := buf.To4(); v4 != nil {
		return v4.String()
	}
	return buf.String()
}
