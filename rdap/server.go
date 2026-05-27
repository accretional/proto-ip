package rdap

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// Server implements the RDAPLookup gRPC service.
type Server struct {
	pb.UnimplementedRDAPLookupServer
	client *Client
}

// NewServer returns a Server backed by client.
func NewServer(client *Client) *Server {
	return &Server{client: client}
}

func (s *Server) LookupIP(ctx context.Context, ip *pb.IP) (*pb.RDAPResponse, error) {
	resp, err := s.client.LookupIP(ctx, ip)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "RDAP lookup: %v", err)
	}
	return resp, nil
}

func (s *Server) LookupCIDR(ctx context.Context, cidr *pb.CIDR) (*pb.RDAPResponse, error) {
	if cidr.GetIp() == nil {
		return nil, status.Error(codes.InvalidArgument, "CIDR.ip must be set")
	}
	if cidr.GetSubnet() == nil {
		return nil, status.Error(codes.InvalidArgument, "CIDR.subnet must be set")
	}
	resp, err := s.client.LookupCIDR(ctx, cidr)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "RDAP lookup: %v", err)
	}
	return resp, nil
}

func (s *Server) LookupAutnum(ctx context.Context, asn *pb.ASN) (*pb.RDAPAutnumResponse, error) {
	if asn.GetNumber() == 0 {
		return nil, status.Error(codes.InvalidArgument, "ASN.number must be non-zero")
	}
	resp, err := s.client.LookupAutnum(ctx, asn)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "RDAP autnum lookup: %v", err)
	}
	return resp, nil
}

// RenderCIDR returns the text form of a CIDR proto for logging.
func RenderCIDR(cidr *pb.CIDR) string {
	ip := ipFromProto(cidr.GetIp())
	prefix, err := subnetPrefixLen(cidr.GetSubnet())
	if err != nil {
		return fmt.Sprintf("<invalid CIDR: %v>", err)
	}
	return fmt.Sprintf("%s/%d", renderNetIP(ip), prefix)
}
