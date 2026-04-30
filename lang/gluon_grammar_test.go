// Package lang holds the IPv4 / IPv6 / CIDR EBNF grammars that
// describe textual IP / CIDR forms. The grammars are the spec — there
// is no Go validator wrapper. This file embeds the three .ebnf
// sources, drives gluon v2's Metaparser gRPC service over an
// in-memory bufconn, and asserts each grammar accepts/rejects a
// representative corpus.
package lang

import (
	"context"
	_ "embed"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/accretional/gluon/v2/metaparser"
	pb "github.com/accretional/gluon/v2/pb"
)

//go:embed ipv4.ebnf
var ipv4EBNF string

//go:embed ipv6.ebnf
var ipv6EBNF string

//go:embed cidr.ebnf
var cidrEBNF string

// startMetaparser brings up gluon's Metaparser gRPC service over an
// in-memory bufconn listener — same pattern gluon's own *_e2e_test.go
// files use — and returns a client bound to it.
func startMetaparser(t *testing.T) (pb.MetaparserClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterMetaparserServer(srv, metaparser.New())
	go func() {
		if err := srv.Serve(lis); err != nil {
			t.Logf("metaparser server exited: %v", err)
		}
	}()
	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	teardown := func() {
		conn.Close()
		srv.Stop()
		lis.Close()
	}
	return pb.NewMetaparserClient(conn), teardown
}

// loadGrammar uploads ebnfSrc through ReadString → EBNF and returns
// the resulting GrammarDescriptor with all WHITESPACE delimiters
// dropped from its lex. IP / CIDR strings are token-tight: ANY
// internal whitespace is wrong, so the grammar's lex must not skip
// whitespace between terminals. ParseEBNF returns a grammar with
// the standard EBNF lex (space / tab / newline / CR as WHITESPACE);
// we strip those symbols so gluon's ParseCST consumes input
// strictly. (Implementation note: this rides on gluon's lex-driven
// skipWSAndComments — see lexkit/expr.go LexConfig.Whitespace and
// v2/metaparser/cst.go whitespaceFromV2Lex.)
//
// A grammar that fails to parse is a programming error in our
// .ebnf, so this fatals the test.
func loadGrammar(t *testing.T, c pb.MetaparserClient, name, ebnfSrc string) *pb.GrammarDescriptor {
	t.Helper()
	ctx := context.Background()
	doc, err := c.ReadString(ctx, &wrapperspb.StringValue{Value: ebnfSrc})
	if err != nil {
		t.Fatalf("ReadString(%s.ebnf): %v", name, err)
	}
	doc.Name = name + ".ebnf"
	gd, err := c.EBNF(ctx, doc)
	if err != nil {
		t.Fatalf("EBNF(%s.ebnf): %v", name, err)
	}
	stripWhitespaceSymbols(gd)
	return gd
}

// stripWhitespaceSymbols removes every WHITESPACE delimiter from
// the grammar's LexDescriptor in place. The remaining symbols
// (DEFINITION, CONCATENATION, ALTERNATION, brackets, comments) are
// what the rule-body re-parser needs; whitespace is the only role
// we actively want gluon's CST parser to NOT skip.
func stripWhitespaceSymbols(gd *pb.GrammarDescriptor) {
	lex := gd.GetLex()
	if lex == nil {
		return
	}
	kept := lex.Symbols[:0]
	for _, sym := range lex.GetSymbols() {
		if d := sym.GetDelimiter(); d != nil && d.GetKind() == pb.Delimiter_WHITESPACE {
			continue
		}
		kept = append(kept, sym)
	}
	lex.Symbols = kept
}

// runCorpus asserts that gluon's CST RPC accepts each entry whose
// `valid` flag is true and rejects the rest. The grammar is taken as
// the source of truth — there is no Go-side validator.
func runCorpus(t *testing.T, c pb.MetaparserClient, gd *pb.GrammarDescriptor, label string, cases []corpusCase) {
	t.Helper()
	ctx := context.Background()
	for _, tc := range cases {
		doc, err := c.ReadString(ctx, &wrapperspb.StringValue{Value: tc.in})
		if err != nil {
			t.Fatalf("%s: ReadString(%q): %v", label, tc.in, err)
		}
		_, err = c.CST(ctx, &pb.CstRequest{Grammar: gd, Document: doc})
		got := err == nil
		if got != tc.valid {
			t.Errorf("%s(%q) accepted=%v, want %v (err=%v)", label, tc.in, got, tc.valid, err)
		}
	}
}

type corpusCase struct {
	in    string
	valid bool
}

func TestIPv4Grammar(t *testing.T) {
	c, teardown := startMetaparser(t)
	defer teardown()
	gd := loadGrammar(t, c, "ipv4", ipv4EBNF)

	runCorpus(t, c, gd, "ipv4", []corpusCase{
		// canonical
		{"0.0.0.0", true},
		{"255.255.255.255", true},
		{"127.0.0.1", true},
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"1.2.3.4", true},
		{"169.254.1.1", true},
		{"249.249.249.249", true},
		{"250.250.250.250", true},
		// out-of-range octets
		{"256.0.0.1", false},
		{"1.2.3.256", false},
		{"999.0.0.0", false},
		{"300.0.0.0", false},
		// wrong shape
		{"1.2.3", false},
		{"1.2.3.4.5", false},
		{"1.2.3.", false},
		{".1.2.3", false},
		{"", false},
		// non-digits
		{"abc.def.ghi.jkl", false},
		{"1.2.3.a", false},
		// leading zeros — strict (Go's net.ParseIP rejects them)
		{"01.2.3.4", false},
		{"1.02.3.4", false},
		{"00.0.0.0", false},
		// whitespace anywhere is rejected — the grammar's lex has no
		// WHITESPACE symbols (stripped in loadGrammar), so gluon's
		// CST parser doesn't skip ANY whitespace.
		{"1.2.3.4 ", false},
		{" 1.2.3.4", false},
		{"1 .2.3.4", false},
		{"1.\n2.3.4", false},
		{"1.\t2.3.4", false},
		// IPv6 input
		{"::1", false},
	})
}

func TestIPv6Grammar(t *testing.T) {
	c, teardown := startMetaparser(t)
	defer teardown()
	gd := loadGrammar(t, c, "ipv6", ipv6EBNF)

	runCorpus(t, c, gd, "ipv6", []corpusCase{
		// shortest forms
		{"::", true},
		{"::1", true},
		{"1::", true},
		{"1::8", true},
		// full 8-group form
		{"1:2:3:4:5:6:7:8", true},
		{"2001:db8:0:0:0:0:0:1", true},
		{"FFFF:FFFF:FFFF:FFFF:FFFF:FFFF:FFFF:FFFF", true},
		// link-local + zone
		{"fe80::1", true},
		{"fe80::1%eth0", true},
		{"fe80::1%en0", true},
		// v4-mapped
		{"::ffff:1.2.3.4", true},
		{"::ffff:192.168.1.1", true},
		{"::1.2.3.4", true},
		{"1:2:3:4:5:6:1.2.3.4", true},
		// hex case
		{"FE80::1", true},
		{"fE80::AbCd", true},
		// invalid: too few groups, no compression
		{"1:2:3:4:5:6:7", false},
		// invalid: too many groups
		{"1:2:3:4:5:6:7:8:9", false},
		{"1:2:3:4:5:6:7:8::", false},
		// invalid: multiple ::
		{":::", false},
		{"1::2::3", false},
		// invalid: h16 too long
		{"12345::1", false},
		// invalid: non-hex
		{"g::1", false},
		// invalid: trailing v4 with bad octet
		{"::ffff:256.0.0.1", false},
		// invalid: degenerate
		{":", false},
		{"", false},
		// invalid: pure IPv4
		{"1.2.3.4", false},
	})
}

func TestCIDRGrammar(t *testing.T) {
	c, teardown := startMetaparser(t)
	defer teardown()
	gd := loadGrammar(t, c, "cidr", cidrEBNF)

	runCorpus(t, c, gd, "cidr", []corpusCase{
		// v4 valid
		{"0.0.0.0/0", true},
		{"192.168.1.0/24", true},
		{"10.0.0.0/8", true},
		{"172.16.0.0/12", true},
		{"1.2.3.4/32", true},
		{"1.2.3.4/0", true},
		// v4 invalid prefix
		{"1.2.3.4/33", false},
		{"1.2.3.4/99", false},
		{"1.2.3.4/032", false},
		{"1.2.3.4/", false},
		// v4 invalid address
		{"256.0.0.0/8", false},
		// v6 valid
		{"::/0", true},
		{"::1/128", true},
		{"fe80::/10", true},
		{"2001:db8::/32", true},
		{"::ffff:0:0/96", true},
		// v6 invalid prefix
		{"::1/129", false},
		{"::1/200", false},
		{"::1/0128", false},
		// missing prefix
		{"1.2.3.4", false},
		{"::1", false},
		// empty
		{"", false},
	})
}
