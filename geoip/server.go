package geoip

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net/netip"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// Server implements the GeoLookup gRPC service over a set of sources.
type Server struct {
	pb.UnimplementedGeoLookupServer
	sources []Source
}

// NewServer returns a Server that queries sources in order. A source that
// errors is logged and skipped; a source that returns (nil, nil) simply has
// no data for the address.
func NewServer(sources ...Source) *Server {
	return &Server{sources: sources}
}

func (s *Server) LookupIP(ctx context.Context, ip *pb.IP) (*pb.GeoResponse, error) {
	addr, err := addrFromProto(ip)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return s.lookup(ctx, addr), nil
}

func (s *Server) LookupCIDR(ctx context.Context, cidr *pb.CIDR) (*pb.GeoResponse, error) {
	if cidr.GetIp() == nil {
		return nil, status.Error(codes.InvalidArgument, "CIDR.ip must be set")
	}
	addr, err := addrFromProto(cidr.GetIp())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return s.lookup(ctx, addr), nil
}

// lookup runs every source, collects the results, and merges them. It never
// returns an error: a missing or failing source degrades coverage but the
// query still succeeds with whatever data the other sources provide.
func (s *Server) lookup(ctx context.Context, addr netip.Addr) *pb.GeoResponse {
	var results []*pb.GeoSourceResult
	for _, src := range s.sources {
		r, err := src.Lookup(ctx, addr)
		if err != nil {
			log.Printf("geoip: source %s failed for %s: %v", src.Kind(), addr, err)
			continue
		}
		if r != nil {
			results = append(results, r)
		}
	}
	best, bestSource := Merge(results)
	return &pb.GeoResponse{
		Best:       best,
		BestSource: bestSource,
		Sources:    results,
	}
}

// addrFromProto reconstructs a netip.Addr from the two sint64 halves of the
// proto IP wire form, unmapping IPv4-in-IPv6 so v4 addresses lookup as v4.
func addrFromProto(ip *pb.IP) (netip.Addr, error) {
	if ip == nil {
		return netip.Addr{}, fmt.Errorf("nil IP message")
	}
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], uint64(ip.GetNetworkPrefix()))
	binary.BigEndian.PutUint64(b[8:16], uint64(ip.GetInterfaceIdentifier()))
	return netip.AddrFrom16(b).Unmap(), nil
}
