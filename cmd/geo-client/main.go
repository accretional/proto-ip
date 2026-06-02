// Command geo-client is a CLI driver for the GeoLookup gRPC service.
// Used by LET_IT_RIP.sh to verify the server returns geolocation data.
//
// Usage:
//
//	geo-client [-addr HOST:PORT] ip   <address>   # e.g. 8.8.8.8
//	geo-client [-addr HOST:PORT] cidr <prefix>    # e.g. 1.1.1.0/24
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
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
	addr := flag.String("addr", "localhost:50099", "GeoLookup server address")
	timeout := flag.Duration("timeout", 30*time.Second, "request timeout")
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: geo-client [-addr HOST:PORT] (ip <addr> | cidr <prefix>)")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()
	client := pb.NewGeoLookupClient(conn)

	var resp *pb.GeoResponse
	switch args[0] {
	case "ip":
		resp, err = client.LookupIP(ctx, parseIP(args[1]))
	case "cidr":
		resp, err = client.LookupCIDR(ctx, parseCIDR(args[1]))
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s %s: %v", args[0], args[1], err)
	}
	printResponse(resp)
}

func printResponse(resp *pb.GeoResponse) {
	fmt.Println("=== best ===")
	printLocation(resp.GetBest())
	fmt.Printf("best_source:     %s\n", shortEnum(resp.GetBestSource().String(), "GEO_SOURCE_"))
	fmt.Printf("confidence:      %s\n", shortEnum(resp.GetConfidence().String(), "GEO_CONFIDENCE_"))
	fmt.Printf("anycast:         %t\n", resp.GetAnycast())
	if resp.GetAsn() != 0 {
		fmt.Printf("asn:             AS%d (%s)\n", resp.GetAsn(), resp.GetNetwork())
	}

	fmt.Println("=== sources ===")
	if len(resp.GetSources()) == 0 {
		fmt.Println("(no source returned data for this address)")
		return
	}
	for _, sr := range resp.GetSources() {
		fmt.Printf("- source:        %s (authoritative=%t, confidence=%s)\n",
			shortEnum(sr.GetSource().String(), "GEO_SOURCE_"), sr.GetAuthoritative(),
			shortEnum(sr.GetConfidence().String(), "GEO_CONFIDENCE_"))
		fmt.Printf("  matched_prefix: %s\n", sr.GetMatchedPrefix())
		if sr.GetAsn() != 0 {
			fmt.Printf("  asn:           AS%d (%s)\n", sr.GetAsn(), sr.GetNetwork())
		}
		printLocationIndented(sr.GetLocation())
		fmt.Printf("  attribution:   %s\n", sr.GetAttribution())
	}
}

func printLocation(loc *pb.GeoLocation) {
	if loc == nil {
		fmt.Println("(no location)")
		return
	}
	fmt.Printf("coordinates:     %s\n", coords(loc))
	fmt.Printf("country:         %s\n", loc.GetCountry())
	fmt.Printf("region:          %s\n", loc.GetRegion())
	fmt.Printf("city:            %s\n", loc.GetCity())
	fmt.Printf("postal_code:     %s\n", loc.GetPostalCode())
	fmt.Printf("time_zone:       %s\n", loc.GetTimeZone())
	fmt.Printf("granularity:     %s\n", shortEnum(loc.GetGranularity().String(), "GEO_GRANULARITY_"))
}

func printLocationIndented(loc *pb.GeoLocation) {
	if loc == nil {
		fmt.Println("  location:      (none)")
		return
	}
	fmt.Printf("  coordinates:   %s\n", coords(loc))
	fmt.Printf("  admin:         country=%q region=%q city=%q postal=%q tz=%q\n",
		loc.GetCountry(), loc.GetRegion(), loc.GetCity(), loc.GetPostalCode(), loc.GetTimeZone())
	fmt.Printf("  granularity:   %s\n", shortEnum(loc.GetGranularity().String(), "GEO_GRANULARITY_"))
}

func coords(loc *pb.GeoLocation) string {
	if loc.Latitude == nil || loc.Longitude == nil {
		return "(none)"
	}
	return fmt.Sprintf("%.4f, %.4f", loc.GetLatitude(), loc.GetLongitude())
}

// shortEnum strips the generated enum prefix and lowercases the result.
func shortEnum(s, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(s, prefix))
}

func parseIP(s string) *pb.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		log.Fatalf("invalid IP address: %q", s)
	}
	return netIPToProto(ip)
}

func parseCIDR(s string) *pb.CIDR {
	ip, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		log.Fatalf("invalid CIDR: %q: %v", s, err)
	}
	ones, _ := ipnet.Mask.Size()
	return &pb.CIDR{
		Ip: netIPToProto(ip),
		Subnet: &pb.Subnet{
			Format: &pb.Subnet_PrefixLength{PrefixLength: uint32(ones)},
		},
	}
}

func netIPToProto(ip net.IP) *pb.IP {
	out := &pb.IP{}
	if v4 := ip.To4(); v4 != nil {
		low := uint64(0xFFFF)<<32 |
			uint64(v4[0])<<24 | uint64(v4[1])<<16 |
			uint64(v4[2])<<8 | uint64(v4[3])
		out.NetworkPrefix = 0
		out.InterfaceIdentifier = int64(low)
		out.Version = &pb.IP_V4{V4: &pb.IPv4Address{
			Format: &pb.IPv4Address_DottedDecimal{DottedDecimal: v4.String()},
		}}
		return out
	}
	v6 := ip.To16()
	out.NetworkPrefix = int64(binary.BigEndian.Uint64(v6[0:8]))
	out.InterfaceIdentifier = int64(binary.BigEndian.Uint64(v6[8:16]))
	out.Version = &pb.IP_V6{V6: &pb.IPv6Address{
		Format: &pb.IPv6Address_Text{Text: v6.String()},
	}}
	return out
}
