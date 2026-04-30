# proto-ip implementation notes

Living document. Captures decisions, gotchas, and pointers to other
repos so future sessions can resume quickly.

## Repo layout

```
proto-ip/
├── proto/ippb/                  proto schemas + generated *.pb.go
│   ├── ip.proto                 128-bit canonical IP wire form
│   ├── ipv4.proto               IPv4Address (formatting oneof)
│   ├── ipv6.proto               IPv6Address (formatting oneof)
│   ├── subnet.proto             Subnet (prefix length / netmask oneof)
│   ├── cidr.proto               IP + Subnet
│   └── local_lookup.proto       LocalLookup gRPC service
├── lang/                        EBNF grammars + grammar-driven validators
│   ├── ipv4.ebnf
│   ├── ipv6.ebnf
│   ├── cidr.ebnf
│   ├── grammar.go               loads + parses grammars via gluon v2
│   ├── validate.go              ParseCST-driven accept/reject
│   ├── corpus_test.go           valid/invalid examples
│   └── fuzz_test.go             go test -fuzz targets
├── procfs/                      Linux /proc/net IP discovery (build-tag linux)
├── sysctlip/                    Darwin getifaddrs/route discovery (build-tag darwin)
├── localip/                     OS-agnostic shim that picks procfs vs sysctlip
├── fixedlength/                 initial proto-fixedlength impl using IP/CIDR
├── cmd/server/                  LocalLookup gRPC server
├── cmd/client/                  LocalLookup gRPC client (drives LET_IT_RIP)
├── docs/                        notes + progress log
└── {setup,build,test,LET_IT_RIP}.sh
```

## Wire format invariants

- Every `IP` is encoded as 128 bits split into two `sint64`s
  (`network_prefix` = high 64, `interface_identifier` = low 64).
- IPv4 addresses are stored as IPv4-mapped IPv6 (`::ffff:0:0/96`):
  high 64 bits = `0`, low 64 bits = `0x0000_FFFF_<32-bit v4>`.
- The `version` oneof preserves whichever client form was supplied
  (textual, numeric, octets) so renderers can round-trip without
  guessing the original family.

## Gluon v2 integration

We follow the proto-sqlite pattern:

```
ebnf source ─► metaparser.WrapString ─► metaparser.ParseEBNF
                                                ▼
                                     GrammarDescriptor
                                                ▼
                            metaparser.ParseCST(grammar+doc) ─► ASTDescriptor
```

The grammar files live in `lang/` and are loaded at process start (or
`go:embed`). For *validation* we don't need the full
`compiler.GrammarToAST → Compile` lowering — we only need the parser
to accept/reject each candidate string. So our `lang.Validate` builds
a `CstRequest{Grammar, Document}` and reports whether parsing
succeeded.

Useful gluon entry points (all in `github.com/accretional/gluon/v2`):

| Need | Function |
|---|---|
| Wrap a Go string as a `DocumentDescriptor` | `metaparser.WrapString` |
| Parse EBNF text → grammar | `metaparser.ParseEBNF` |
| Parse text against a grammar | `metaparser.ParseCST` |
| Concatenate `DocumentDescriptor.text` chunks | `metaparser.TextOf` |

`ParseEBNF` requires ISO 14977 syntax: rules use `,` for concatenation,
`|` for alternation, `[ ]` for optional, `{ }` for repetition,
`( )` for grouping, double or single quotes for terminals, and
`(* ... *)` for comments. Identifiers are letter/digit/underscore.

## Grammar coverage targets

### IPv4

- Dotted-decimal canonical: `192.0.2.1`, `0.0.0.0`, `255.255.255.255`
- Leading zeros in octets: `001.002.003.004` (accepted but tagged
  by inet_aton as octal — not implemented in v1, just documented)
- Inet-aton variants:
    - 4-part dotted decimal (above)
    - 3-part `a.b.c` where c is 16-bit
    - 2-part `a.b` where b is 24-bit
    - 1-part `n` 32-bit integer
    - hex `0x` and octal `0` prefixes per part

The MVP grammar handles canonical 4-part dotted-decimal only;
`inet_aton` is a stretch goal and lives in a separate rule so we can
toggle it on without breaking strict parsers.

### IPv6

Per RFC 4291 §2.2 and RFC 5952:

- Full form: 8 groups of 1-4 hex digits, colon-separated
- `::` zero compression at most once
- IPv4-mapped form: `::ffff:192.0.2.1`
- Zone identifier: `fe80::1%eth0` (RFC 4007)

### CIDR

- `<ipv4>/<0..32>`
- `<ipv6>/<0..128>`

## Test corpus structure

`lang/corpus_test.go` ships:

- `validIPv4`, `invalidIPv4`
- `validIPv6`, `invalidIPv6`
- `validCIDR`, `invalidCIDR`

…each running through `lang.Validate*` with table-driven assertions.
Every entry has a brief `note` that explains why it should accept or
reject.

## Fuzzing

`lang/fuzz_test.go` exposes three `Fuzz*` targets. The corpus seeds
include both valid and invalid examples; the fuzz invariant is
"`net.ParseIP` (or `net.ParseCIDR`) and our grammar agree, modulo
the documented permissive forms".

When the grammar is *more* permissive than `net.ParseIP` (e.g.
`inet_aton`), the fuzz invariant skips the `net` cross-check by
explicitly classifying the candidate.

## Local IP lookup strategy

| OS | Source of truth | Implementation |
|---|---|---|
| Linux | `/proc/net/fib_trie` (LOCAL entries), `/proc/net/if_inet6` | `procfs/` |
| Darwin | `getifaddrs(3)` via cgo, fallback to `route` netlink-equivalent (`sysctl` `NET_RT_IFLIST`) | `sysctlip/` |
| Other | Go stdlib `net.Interfaces()` fallback | `localip/` |

The Go stdlib `net.Interfaces` already wraps `getifaddrs` on Darwin
and reads /proc on Linux, so for the MVP we lean on it directly and
keep `procfs/` and `sysctlip/` as **explicit, low-level** alternatives
for cases where stdlib is too coarse (e.g. distinguishing LOCAL vs
LINK in the FIB).

`localip.List(filter)` returns `[]*ippb.Interface` and is what the
gRPC server calls.

### Cross-references

- `/Volumes/wd_office_1/repos/sysctl/` — Darwin sysctl wrapper, has
  `internal/macosasmsysctl/` for raw syscalls. Not directly reused
  yet; the stdlib `net.Interfaces` is sufficient for v1.
- `/Volumes/wd_office_1/repos/gluon/v2/` — grammar tooling.
- `/Volumes/wd_office_1/repos/proto-sqlite/lang/cmd/genproto/main.go`
  — reference for the EBNF→grammar→AST pipeline.

## proto-fixedlength (initial sketch)

Goal: convert any `IP` proto message to a single 16-byte (128-bit)
buffer in canonical IPv4-mapped IPv6 form, and back.

Approach:

1. Walk the `ip.IP` message via `protoreflect`.
2. For each field with a `[fixedlength.bits = N]` field option, copy
   N bits in MSB-first order into the output buffer.
3. The `version` oneof maps to a 1-bit family tag (0 = v4, 1 = v6),
   recovered on decode.
4. AST-AST transformation (gluon `astkit.ReplaceKind`) drops the oneof
   wrapper and inlines the underlying scalar before encoding.

For v1 we ship a hand-coded encoder/decoder against the IP message
specifically (no annotations / reflection yet). The interface lives in
`fixedlength/ip.go` so the path to a generic version is short.

## Open questions

- IPv6 "::ffff:1.2.3.4" v4-mapped form — should it route through the
  IPv4 grammar or the IPv6 grammar? Currently the IPv6 grammar accepts
  it inline (per RFC 4291); the IPv4 grammar does not.
- inet_aton parts in CIDR — `10/8` is sometimes written without the
  trailing octets. Not in MVP.
- Zone identifiers are usually attached to *link-local* addresses
  only, but the grammar permits them anywhere. We accept the looser
  form for symmetry with `getifaddrs` output.
