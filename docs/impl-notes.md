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

## Strict-whitespace grammar contract

Every grammar in `lang/` is **token-tight**: an IP / CIDR string
contains no whitespace anywhere — not internal, not trailing, not
leading. The grammar enforces this through its
`LexDescriptor.whitespace`, not by Go-side validation.

`lang/gluon_grammar_test.go`'s `loadGrammar` calls
`stripWhitespaceSymbols(gd)` after `ParseEBNF`, removing every
`Delimiter.WHITESPACE` symbol from the grammar's lex. gluon's
`ParseAST` (via `LexConfig.Whitespace` and the lex-driven check in
`skipWSAndComments`) then skips no whitespace at all when matching
input — so `"1 .2.3.4"`, `"1.2.3.4 "`, `" 1.2.3.4"`,
`"1\n.2.3.4"` all fail.

This contract relies on three pieces of gluon machinery, all
on `accretional/gluon` `main`:

- v1: `LexConfig.Whitespace []rune` + `LexConfig.IsWhitespace`
  (`lexkit/expr.go`).
- v1: `skipWSAndComments` consults `ap.lex.IsWhitespace` (and the
  EOF check at the end of `ParseASTWithOptions` does too).
- v2: `convertGrammarToV1` (`v2/metaparser/cst.go`) carries the v2
  lex's WHITESPACE delimiters into the v1 lex via
  `whitespaceFromV2Lex`, so the v1 parser sees what the v2 grammar
  declared.

If you write a new `.ebnf` in this repo and load it through gluon's
`Metaparser`, **call `stripWhitespaceSymbols(gd)` before passing it
to `ParseCST`** — otherwise the grammar's lex inherits the standard
EBNF whitespace symbols and internal whitespace silently slips
through. The corpus and fuzz tests in `lang/` are the canonical
example.

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

## RDAP Lookup service

### Proto shape

`proto/ippb/rdap.proto` adds:

| Message | Key fields |
|---|---|
| `RDAPEntity` | handle, fn (vCard FN), roles, emails |
| `RDAPEvent` | action (eventAction), date (eventDate) |
| `RDAPNetwork` | handle, name, type, start/end address, ip_version, country, status, entities, events, links, rdap_server |
| `RDAPResponse` | network, raw_json |

Service `RDAPLookup` exposes two **unary** RPCs:
- `LookupIP(IP) → RDAPResponse`
- `LookupCIDR(CIDR) → RDAPResponse`

### Bootstrap (RFC 7484)

`rdap/bootstrap.go` fetches `data.iana.org/rdap/ipv4.json` and
`data.iana.org/rdap/ipv6.json` once at server startup. Each bootstrap
file maps IP prefixes to `[service_url, ...]` lists. `Resolve(ip)` finds
the most-specific (longest prefix) match and returns the first URL.
The returned URL always ends with `/`.

Bootstrap is in-memory only — restart the server to refresh.

### HTTP client and JSON parsing

`rdap/client.go` constructs:
- `GET {baseURL}ip/{ip}` for single-IP lookups
- `GET {baseURL}ip/{ip}/{prefix}` for CIDR lookups

RDAP vCard parsing: `vcardArray` is `["vcard", [[prop, params, type, value], ...]]`.
`parseVCard` extracts `fn` and `email` entries from this structure.

`raw_json` is the full response body preserved verbatim in
`RDAPResponse` for callers that need fields not modelled in proto.

### IP text rendering in rdap package

`rdap.ipFromProto` reconstructs `net.IP` from the two `sint64` halves
(same logic as `cmd/client/main.go:renderIP`). `renderNetIP` calls
`.To4()` before `.String()` so IPv4-mapped addresses print as dotted
decimal, not `::ffff:...` notation.

### Default ports

| Service | Default port |
|---|---|
| LocalLookup | 50097 |
| RDAPLookup | 50098 |

## GeoLookup service (IP geolocation)

### Goal

Best-effort IP → physical location, returning the most granular data
available (ideally lat/lon) while being honest about gaps. No single
authoritative source exists, so we combine and merge several.

### Sources (v1)

| Source | Granularity | Authority | License | Pkg |
|---|---|---|---|---|
| RFC 8805/9632 geofeeds | country/region/city/postal (NO coords) | operator self-published | per-publisher | `geoip/geofeed.go` |
| DB-IP City Lite (MMDB) | + lat/lon | aggregated estimate | CC BY 4.0 | `geoip/dbip.go` |
| RIPE IPmap | country/city + lat/lon (exact-IP only) | measured (Atlas) | RIPE NCC ToS | `geoip/ipmap.go` |

**Key fact:** RFC 8805 geofeeds carry no coordinates — only country
(ISO 3166-1), region (ISO 3166-2), city, postal. So coordinates always come
from DB-IP; geofeeds contribute authoritative admin fields. This drives the
merge policy.

IP2Location LITE (CC-BY-**SA**, share-alike) and MaxMind GeoLite2
(account-gated, redistribution-restricted) were deliberately excluded from v1.
The `geoip.Source` interface (`Lookup(ctx, netip.Addr) → *GeoSourceResult`,
`Kind()`) makes adding them later a drop-in.

A full survey of candidate sources (RIPE IPmap, IP2Location LITE, GeoLite2,
`sapics/ip-location-db`, RIR delegated stats, IPtoASN, …) with licensing,
formats, and a prioritised recommendation lives in
[geo-sources.md](geo-sources.md). Top pick to add next: **RIPE IPmap** —
measured, coordinate-bearing data for core infrastructure that complements the
estimate-based DB-IP.

### Proto shape (`proto/ippb/geo.proto`)

- `GeoLocation` — `optional double latitude/longitude` (optional so a real
  `0,0` is distinguishable from absent), country, region, city, postal_code,
  time_zone, `GeoGranularity` (COUNTRY < REGION < CITY < COORDINATES).
- `GeoSourceResult` — source, location, matched_prefix, `authoritative`
  (true for geofeeds), `attribution` (license credit).
- `GeoResponse` — `best` (merged) + `best_source` + repeated `sources`.
- Service `GeoLookup` — unary `LookupIP(IP)` / `LookupCIDR(CIDR)`. Port 50099.

### Merge policy (`geoip/merge.go`, pure + unit-tested)

1. Base = result with highest granularity.
2. Admin fields (country/region/city/postal) prefer the first authoritative
   (geofeed) result.
3. Coordinates + time_zone filled from the first coordinate-bearing result
   (→ DB-IP) if the base lacks them; granularity upgrades to COORDINATES.
4. `best_source` = the coordinate provider when `best` ends up with coords,
   else the base source.

### Geofeed discovery (RFC 9632)

Empirically, the major RIRs differ on where the geofeed URL lives, so
`geoip/geofeed.go:discover` tries two channels (one `geofeedURLRe` regex
matches all forms across both):

1. **RDAP body** — ARIN-style `Geofeed <url>` remarks appear inline in the
   RDAP JSON we already fetch via `rdap.Client.LookupIP`.
2. **RPSL whois (port 43)** — RFC 9632's *normative* location is the
   inetnum `geofeed:` attribute. **RIPE/APNIC serve this over whois and do
   NOT echo it into RDAP** (RDAP only declares the `geofeed1` conformance, not
   the URL). The RDAP response's `port43` field gives the whois host, so we
   do one `whoisQuery` (RFC 3912: dial :43, send `<ip>\r\n`, read) and regex
   the result.

Verified live: `2a05:b0c6:a200::1` (Pfcloud /39) →
`geofeed: https://api.geofeed.space/pfcloud/geofeed.csv` via RIPE whois → the
authoritative geofeed supplies `region=NL-LI` (which DB-IP City Lite lacks)
while DB-IP supplies coordinates; both merge into `best`.

Fetched CSVs are cached in-memory with a 1h TTL (`geoip/cache.go`); discovery
itself is not cached in v1. RPKI authentication of the feed (RFC 9632 §3) is
**not** implemented — `authoritative` means "self-published", not
"cryptographically verified". Discovery uses the most-specific whois object
only; walking up to a less-specific parent that carries the geofeed is future
work.

> Note: this whois channel is a deliberate addition beyond the originally
> approved RDAP-only plan, made after confirming RDAP does not carry the URL
> on RIPE — without it the geofeed source would be effectively dead.

### RIPE IPmap source (`geoip/ipmap.go`)

Measured locations for core infrastructure from the RIPE IPmap daily dump
(`https://ftp.ripe.net/ripe/ipmap/geolocations-latest`). Specifics:

- The dump is **exact-IP only** (`/32` and `/128`, ~600k rows), so the source
  answers only when the queried address is itself a known node — sparse for
  end-user IPs, but measured (not estimated) for the infra it covers.
- Loaded into a `map[netip.Addr]ipmapEntry` at startup. The file is kept
  **bzip2-compressed** in the cache (~5 MB) and decoded in-process via Go's
  `compress/bzip2` (no `bunzip2` CLI dependency; the real-dump test asserts the
  full multi-stream file decodes by requiring >400k loaded rows).
- CSV columns: `prefix,geolocation_id,city,state,country_name,cc2,cc3,lat,lon,score`.
  `country_name` is **unquoted and may contain commas** (e.g. "Bonaire, Saint
  Eustatius and Saba"), so numeric/code fields are read by **offset from the end**
  of the row; `city` stays at index 2 (geolocation_id has no comma).
- `region` is left empty: IPmap's `state` is a name, not an ISO 3166-2 code.
- `score` is a **relative sort factor, not accuracy** (RIPE docs), so it is not
  surfaced; weighting sources by confidence is a documented follow-up.
- **Ordering:** geo-server lists IPmap *before* DB-IP, so on a granularity tie
  (both COORDINATES) the merge keeps IPmap's measured coordinates as `best`.
  Verified live: `1.1.1.1` → IPmap "Johannesburg" wins over DB-IP "Sydney".

See [geo-sources.md](geo-sources.md) for the full source survey.

### MMDB reader

`github.com/oschwald/maxminddb-golang/v2` (the only new dependency). v2 API:
`maxminddb.Open(path)` → `db.Lookup(netip.Addr)` → `Result.{Err,Found,Decode,Prefix}`.
DB-IP City Lite uses the GeoIP2-City schema, so we decode into a minimal struct
(`country.iso_code`, `subdivisions[].iso_code`, `city.names.en`,
`location.{latitude,longitude,time_zone}`, `postal.code`). `(0,0)` is treated as
"no coordinates" (Null Island, not a real estimate).

### DB acquisition

`setup.sh` downloads `https://download.db-ip.com/free/dbip-city-lite-YYYY-MM.mmdb.gz`
into gitignored `data/geoip/` (tries current then previous month; idempotent;
download failure only warns). `geoip.FindDBIPDatabase(dir)` globs for the newest
`dbip-city-lite-*.mmdb`. **Attribution is a license requirement** (CC BY 4.0):
credited in `README.md`, the `DBIPAttribution` constant, and `geo-client` output.

### Address conversion

`server.go:addrFromProto` reconstructs a `netip.Addr` from the two sint64 halves
and `.Unmap()`s v4-mapped. `geofeed.go:protoFromAddr` does the reverse
(`netip.Addr.As16()` yields the v4-in-v6 mapped form matching the wire format)
to feed `rdap.Client`.

## Open questions

- IPv6 "::ffff:1.2.3.4" v4-mapped form — should it route through the
  IPv4 grammar or the IPv6 grammar? Currently the IPv6 grammar accepts
  it inline (per RFC 4291); the IPv4 grammar does not.
- inet_aton parts in CIDR — `10/8` is sometimes written without the
  trailing octets. Not in MVP.
- Zone identifiers are usually attached to *link-local* addresses
  only, but the grammar permits them anywhere. We accept the looser
  form for symmetry with `getifaddrs` output.
