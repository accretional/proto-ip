package geoip

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

type fakeEnricher struct {
	facts autnumFacts
	err   error
}

func (e fakeEnricher) Enrich(_ context.Context, _ uint32) (autnumFacts, error) {
	return e.facts, e.err
}

type fakeResolver struct {
	ptr string
	err error
}

func (r fakeResolver) LookupPTR(_ context.Context, _ netip.Addr) (string, error) {
	return r.ptr, r.err
}

func TestBuildNetworkInfo_AllSources(t *testing.T) {
	rpki, err := parseVRPs(strings.NewReader(vrpsJSON))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		rpki: rpki,
		enricher: fakeEnricher{facts: autnumFacts{
			name: "CLOUDFLARENET", org: "Cloudflare, Inc.", country: "US",
			abuse: "abuse@cloudflare.com", handle: "AS13335",
		}},
		resolver: fakeResolver{ptr: "one.one.one.one"},
	}

	resp := &pb.GeoResponse{Asn: 13335, Network: "CLOUDFLARENET"}
	s.buildNetworkInfo(context.Background(), netip.MustParseAddr("1.1.1.1"), resp)

	ni := resp.GetNetworkInfo()
	if ni == nil {
		t.Fatal("NetworkInfo not populated")
	}
	if ni.GetAsn() != 13335 || ni.GetNetwork() != "CLOUDFLARENET" {
		t.Errorf("asn/network = %d/%q", ni.GetAsn(), ni.GetNetwork())
	}
	if ni.GetAsName() != "CLOUDFLARENET" || ni.GetOrg() != "Cloudflare, Inc." ||
		ni.GetRegistryCountry() != "US" || ni.GetAbuseEmail() != "abuse@cloudflare.com" ||
		ni.GetRdapHandle() != "AS13335" {
		t.Errorf("RDAP facts not mapped: %+v", ni)
	}
	if ni.GetRpkiStatus() != pb.RPKIStatus_RPKI_STATUS_VALID {
		t.Errorf("rpki_status = %v, want VALID", ni.GetRpkiStatus())
	}
	if len(ni.GetRpkiCoveringRoas()) != 1 {
		t.Errorf("covering roas = %d, want 1", len(ni.GetRpkiCoveringRoas()))
	}
	if ni.GetReverseDns() != "one.one.one.one" {
		t.Errorf("reverse_dns = %q", ni.GetReverseDns())
	}
}

func TestBuildNetworkInfo_DegradesGracefully(t *testing.T) {
	rpki, err := parseVRPs(strings.NewReader(vrpsJSON))
	if err != nil {
		t.Fatal(err)
	}
	// Enricher and resolver both fail; RPKI must still populate.
	s := &Server{
		rpki:     rpki,
		enricher: fakeEnricher{err: errors.New("rdap down")},
		resolver: fakeResolver{err: errors.New("no ptr")},
	}

	resp := &pb.GeoResponse{Asn: 13335}
	s.buildNetworkInfo(context.Background(), netip.MustParseAddr("1.1.1.1"), resp)

	ni := resp.GetNetworkInfo()
	if ni == nil {
		t.Fatal("NetworkInfo not populated")
	}
	if ni.GetAsName() != "" || ni.GetAbuseEmail() != "" {
		t.Errorf("failed enricher should leave RDAP fields empty: %+v", ni)
	}
	if ni.GetReverseDns() != "" {
		t.Errorf("failed resolver should leave reverse_dns empty: %q", ni.GetReverseDns())
	}
	if ni.GetRpkiStatus() != pb.RPKIStatus_RPKI_STATUS_VALID {
		t.Errorf("rpki_status = %v, want VALID (independent of other enrichers)", ni.GetRpkiStatus())
	}
}

func TestBuildNetworkInfo_NoEnrichers(t *testing.T) {
	s := &Server{} // no rpki / enricher / resolver
	resp := &pb.GeoResponse{Asn: 13335}
	s.buildNetworkInfo(context.Background(), netip.MustParseAddr("1.1.1.1"), resp)
	if resp.GetNetworkInfo() != nil {
		t.Errorf("expected nil NetworkInfo when no enrichers configured")
	}
}

func TestAutnumFactsFromRDAP(t *testing.T) {
	a := &pb.RDAPAutnum{
		Handle:  "AS15169",
		Name:    "GOOGLE",
		Country: "US",
		Entities: []*pb.RDAPEntity{
			{
				Roles: []pb.RDAPRole{pb.RDAPRole_RDAP_ROLE_REGISTRANT},
				Org:   "Google LLC",
			},
			{
				Roles:  []pb.RDAPRole{pb.RDAPRole_RDAP_ROLE_ABUSE},
				Emails: []string{"network-abuse@google.com", "second@google.com"},
			},
		},
	}
	f := autnumFactsFromRDAP(a)
	if f.name != "GOOGLE" || f.country != "US" || f.handle != "AS15169" {
		t.Errorf("direct fields wrong: %+v", f)
	}
	if f.org != "Google LLC" {
		t.Errorf("org = %q, want Google LLC", f.org)
	}
	if f.abuse != "network-abuse@google.com" {
		t.Errorf("abuse = %q, want first abuse email", f.abuse)
	}
}

func TestAutnumFactsFromRDAP_OrgFallsBackToFn(t *testing.T) {
	a := &pb.RDAPAutnum{
		Entities: []*pb.RDAPEntity{{
			Roles: []pb.RDAPRole{pb.RDAPRole_RDAP_ROLE_REGISTRANT},
			Fn:    "Example Org (via Fn)",
		}},
	}
	if f := autnumFactsFromRDAP(a); f.org != "Example Org (via Fn)" {
		t.Errorf("org = %q, want Fn fallback", f.org)
	}
}
