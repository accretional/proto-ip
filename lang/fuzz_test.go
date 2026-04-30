package lang

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/accretional/gluon/v2/metaparser"
	pb "github.com/accretional/gluon/v2/pb"
)

// Fuzz targets call gluon's pure-Go metaparser.ParseCST directly
// (rather than through bufconn) to keep iteration fast. The
// gRPC-driven corpus tests in gluon_grammar_test.go cover the wire
// surface; these target the grammars themselves under random
// inputs, with net.ParseIP / netip.ParsePrefix as the agreement
// oracle.
//
// Invariant: net agrees with our grammar on every well-formed v4 /
// v6 / CIDR string. Where they disagree, the disagreement is a
// grammar bug worth reporting — the fuzzer fails the test.

var (
	fuzzGrammarsOnce  sync.Once
	fuzzIPv4Grammar   *pb.GrammarDescriptor
	fuzzIPv6Grammar   *pb.GrammarDescriptor
	fuzzCIDRGrammar   *pb.GrammarDescriptor
	fuzzGrammarsError error
)

func loadFuzzGrammars(tb testing.TB) (*pb.GrammarDescriptor, *pb.GrammarDescriptor, *pb.GrammarDescriptor) {
	tb.Helper()
	fuzzGrammarsOnce.Do(func() {
		ctx := context.Background()
		srv := metaparser.New()
		parse := func(name, src string) *pb.GrammarDescriptor {
			doc, err := srv.ReadString(ctx, &wrapperspb.StringValue{Value: src})
			if err != nil {
				fuzzGrammarsError = err
				return nil
			}
			doc.Name = name + ".ebnf"
			gd, err := srv.EBNF(ctx, doc)
			if err != nil {
				fuzzGrammarsError = err
				return nil
			}
			return gd
		}
		fuzzIPv4Grammar = parse("ipv4", ipv4EBNF)
		fuzzIPv6Grammar = parse("ipv6", ipv6EBNF)
		fuzzCIDRGrammar = parse("cidr", cidrEBNF)
	})
	if fuzzGrammarsError != nil {
		tb.Fatalf("loading grammars: %v", fuzzGrammarsError)
	}
	return fuzzIPv4Grammar, fuzzIPv6Grammar, fuzzCIDRGrammar
}

func grammarAccepts(tb testing.TB, gd *pb.GrammarDescriptor, s string) bool {
	tb.Helper()
	doc := metaparser.WrapString(s)
	_, err := metaparser.ParseCST(&pb.CstRequest{Grammar: gd, Document: doc})
	return err == nil
}

// FuzzIPv4 cross-checks the IPv4 grammar against net.ParseIP. Any
// disagreement (grammar accepts and net rejects, or vice versa) is
// reported as a fuzz failure.
func FuzzIPv4(f *testing.F) {
	gd, _, _ := loadFuzzGrammars(f)

	// Seed with known-good and known-bad cases so the corpus has
	// shape from the start.
	for _, s := range []string{
		"0.0.0.0", "255.255.255.255", "192.168.1.1", "1.2.3.4",
		"256.0.0.1", "01.2.3.4", "1.2.3", "abc", "",
		"1.2.3.4.5", "1..2.3", " 1.2.3.4", "1.2.3.4 ",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		// Skip inputs that contain whitespace or non-printable
		// chars: gluon tolerates trailing whitespace by design and
		// net.ParseIP doesn't, so they disagree on those by spec.
		if hasWhitespace(s) {
			t.Skip()
		}
		grammarOK := grammarAccepts(t, gd, s)
		netOK := net.ParseIP(s).To4() != nil && !strings.Contains(s, ":")
		if grammarOK != netOK {
			t.Errorf("disagree on %q: grammar=%v net=%v", s, grammarOK, netOK)
		}
	})
}

// FuzzIPv6 cross-checks the IPv6 grammar against netip.ParseAddr,
// which (unlike net.ParseIP) rejects leading zeros and other
// non-canonical forms — closer to RFC 4291.
func FuzzIPv6(f *testing.F) {
	_, gd, _ := loadFuzzGrammars(f)

	for _, s := range []string{
		"::", "::1", "1::", "1::8", "fe80::1", "2001:db8::1",
		"::ffff:1.2.3.4", "1:2:3:4:5:6:7:8", "1:2:3:4:5:6:1.2.3.4",
		"1:2:3:4:5:6:7", "1:2:3:4:5:6:7:8:9", ":::", "1::2::3",
		"12345::1", "g::1", "::ffff:256.0.0.1", "", ":",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if hasWhitespace(s) {
			t.Skip()
		}
		// Skip zone-bearing inputs: the grammar requires at least
		// one zone_char after "%" (RFC 4007 leaves the zone format
		// implementation-defined), but netip accepts a bare "%".
		// The two disagree on zone semantics, not on address
		// validity, so don't let zones poison the address-grammar
		// fuzz.
		if strings.ContainsRune(s, '%') {
			t.Skip()
		}
		grammarOK := grammarAccepts(t, gd, s)
		addr, err := netip.ParseAddr(s)
		// netip accepts both pure-v6 and v4-in-v6. Both should be
		// accepted by our grammar (the latter via the v4-mapped
		// post_M alternatives). Pure-v4 strings (no colon) must
		// not match the IPv6 grammar.
		netOK := err == nil && (addr.Is6() || addr.Is4In6())
		if grammarOK != netOK {
			t.Errorf("disagree on %q: grammar=%v netip=%v", s, grammarOK, netOK)
		}
	})
}

// FuzzCIDR cross-checks the CIDR grammar against netip.ParsePrefix.
func FuzzCIDR(f *testing.F) {
	_, _, gd := loadFuzzGrammars(f)

	for _, s := range []string{
		"0.0.0.0/0", "1.2.3.4/32", "192.168.1.0/24", "10.0.0.0/8",
		"::/0", "::1/128", "fe80::/10", "2001:db8::/32",
		"1.2.3.4/33", "1.2.3.4/", "::1/129", "1.2.3.4", "::1",
		"1.2.3.4/032", "::/0128", "",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if hasWhitespace(s) {
			t.Skip()
		}
		grammarOK := grammarAccepts(t, gd, s)
		_, err := netip.ParsePrefix(s)
		netOK := err == nil
		if grammarOK != netOK {
			t.Errorf("disagree on %q: grammar=%v netip=%v", s, grammarOK, netOK)
		}
	})
}

// hasWhitespace reports whether s contains any ASCII whitespace.
// Gluon tolerates trailing whitespace by design (matching
// lexkit.Parse for EBNF source); net / netip don't, so the
// agreement invariant doesn't apply to those inputs.
func hasWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return true
		}
	}
	return false
}

