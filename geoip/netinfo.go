package geoip

import (
	"context"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/accretional/proto-ip/rdap"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// autnumFacts is the subset of RDAP autnum registration data NetworkInfo
// surfaces for an origin AS.
type autnumFacts struct {
	name    string
	org     string
	country string
	abuse   string
	handle  string
}

// asnEnricher resolves AS-level registration facts for an origin ASN. It is an
// interface so tests can stub it without network access.
type asnEnricher interface {
	Enrich(ctx context.Context, asn uint32) (autnumFacts, error)
}

// ptrResolver resolves the reverse-DNS (PTR) name of an address.
type ptrResolver interface {
	LookupPTR(ctx context.Context, addr netip.Addr) (string, error)
}

// rdapEnricher enriches an ASN via RDAP autnum lookups, caching one result per
// distinct AS (registration data is stable over a server's lifetime, so each
// AS is queried at most once).
type rdapEnricher struct {
	client *rdap.Client
	mu     sync.Mutex
	cache  map[uint32]autnumFacts
}

func newRDAPEnricher(c *rdap.Client) *rdapEnricher {
	return &rdapEnricher{client: c, cache: make(map[uint32]autnumFacts)}
}

func (e *rdapEnricher) Enrich(ctx context.Context, asn uint32) (autnumFacts, error) {
	e.mu.Lock()
	if f, ok := e.cache[asn]; ok {
		e.mu.Unlock()
		return f, nil
	}
	e.mu.Unlock()

	resp, err := e.client.LookupAutnum(ctx, &pb.ASN{Number: asn})
	if err != nil {
		return autnumFacts{}, err
	}
	f := autnumFactsFromRDAP(resp.GetAutnum())

	e.mu.Lock()
	e.cache[asn] = f
	e.mu.Unlock()
	return f, nil
}

// autnumFactsFromRDAP extracts the registration subset from an RDAP autnum:
// name/country/handle directly, org from the registrant entity, and the abuse
// email from the abuse-role entity.
func autnumFactsFromRDAP(a *pb.RDAPAutnum) autnumFacts {
	f := autnumFacts{
		name:    a.GetName(),
		country: a.GetCountry(),
		handle:  a.GetHandle(),
	}
	for _, ent := range a.GetEntities() {
		for _, role := range ent.GetRoles() {
			switch role {
			case pb.RDAPRole_RDAP_ROLE_REGISTRANT:
				if f.org == "" {
					f.org = firstNonEmpty(ent.GetOrg(), ent.GetFn())
				}
			case pb.RDAPRole_RDAP_ROLE_ABUSE:
				if f.abuse == "" && len(ent.GetEmails()) > 0 {
					f.abuse = ent.GetEmails()[0]
				}
			}
		}
	}
	return f
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// netResolver implements ptrResolver over a *net.Resolver with a per-call
// timeout so a slow PTR lookup cannot stall a geo response.
type netResolver struct {
	r       *net.Resolver
	timeout time.Duration
}

func newNetResolver(r *net.Resolver) *netResolver {
	if r == nil {
		r = net.DefaultResolver
	}
	return &netResolver{r: r, timeout: 3 * time.Second}
}

func (n *netResolver) LookupPTR(ctx context.Context, addr netip.Addr) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	names, err := n.r.LookupAddr(ctx, addr.String())
	if err != nil || len(names) == 0 {
		return "", err
	}
	return strings.TrimSuffix(names[0], "."), nil
}

// buildNetworkInfo assembles the reliable AS-level spine for addr and attaches
// it to resp. Every enricher is optional and best-effort: a failure logs and
// leaves its fields zero without affecting the rest of the response. The origin
// ASN/network are taken from what Merge already lifted from the BGP source.
func (s *Server) buildNetworkInfo(ctx context.Context, addr netip.Addr, resp *pb.GeoResponse) {
	if s.enricher == nil && s.rpki == nil && s.resolver == nil {
		return
	}
	ni := &pb.NetworkInfo{
		Asn:     resp.GetAsn(),
		Network: resp.GetNetwork(),
	}

	if s.enricher != nil && ni.Asn != 0 {
		if f, err := s.enricher.Enrich(ctx, ni.Asn); err != nil {
			log.Printf("geoip: RDAP autnum enrichment for AS%d failed: %v", ni.Asn, err)
		} else {
			ni.AsName = f.name
			ni.Org = f.org
			ni.RegistryCountry = f.country
			ni.AbuseEmail = f.abuse
			ni.RdapHandle = f.handle
		}
	}

	if s.rpki != nil {
		status, covering := s.rpki.Validate(addr, ni.Asn)
		ni.RpkiStatus = status
		ni.RpkiCoveringRoas = vrpsToProto(covering)
	}

	if s.resolver != nil {
		if ptr, err := s.resolver.LookupPTR(ctx, addr); err == nil && ptr != "" {
			ni.ReverseDns = ptr
		}
	}

	resp.NetworkInfo = ni
}
