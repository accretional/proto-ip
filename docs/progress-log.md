# proto-ip progress log

Append-only notebook. Newest entries at the bottom.

## 2026-04-29

- Read CLAUDE.md. Plan: EBNF grammars (gluon v2) for IPv4/IPv6/CIDR;
  LocalLookup gRPC server backed by procfs (Linux) / getifaddrs
  (Darwin); initial proto-fixedlength impl.
- Surveyed sibling repos: `gluon` (v2 metaparser pipeline ready),
  `sysctl` (template for setup/build/test/LET_IT_RIP), `proto-sqlite`
  (working example of `ParseEBNF → GrammarToAST → Compile`). No
  existing `proto-fixedlength` repo on disk — we're scaffolding it
  inline under `fixedlength/`.
- Reorganised root protos into `proto/ippb/`. Fixed:
    - `subnet.proto` `SubNet` → `Subnet`
    - `local_lookup.proto` referenced undefined `InterfaceType`
    - missing imports for `IP`, `Subnet`
    - go_package now `github.com/accretional/proto-ip/proto/ippb;ippb`
  Expanded oneofs to cover the formats the EBNF grammars will need
  to round-trip.
- Wrote `setup.sh`, `build.sh`, `test.sh`, `LET_IT_RIP.sh` (idempotent
  per CLAUDE.md). `go.mod` pinned to go 1.26 with a local `replace`
  pointing at the on-disk gluon checkout (we're tracking gluon's tip
  while v2 stabilises).
- Drafted `docs/impl-notes.md` with the layout, gluon v2 integration
  plan, grammar coverage targets, fuzz strategy, and cross-references.
- Next: write the three EBNF grammars and the corpus + fuzz tests
  before moving on to the procfs/sysctl backends.

## 2026-04-29 (continued, post-bootstrap-commit)

- Bootstrapped commit landed: 41 files, three .ebnf grammars, fuzz
  harness, LocalLookup gRPC server + client, all green via
  LET_IT_RIP.sh.
- Fuzz round-2: tightened the harness, removing two exceptions that
  had been masking real gluon bugs.
    - First: the `hasCommentMarker` skip from earlier in the day.
      Root cause was gluon's EOF check reusing the EBNF-source
      comment skipper on user input, so unterminated `(*` ate to
      EOF. Fixed in gluon `74d04c3` (EOF skips whitespace only,
      not comments).
    - Second: the `hasWhitespace` skip. Looking deeper, the grammar
      was actually accepting `"1 .2.3.4"`, `"1\n.2.3.4"`, etc. —
      not because of trailing-WS tolerance (which is reasonable)
      but because gluon's `EBNFParseOptions` hardcodes IsLexical to
      always return false, putting every production into syntactic
      mode and skipping whitespace between every terminal. The user
      pointed at the LexDescriptor as the right knob: if the grammar
      lex doesn't have WHITESPACE symbols, the parser should skip
      none. Implemented via a v1 `LexConfig.Whitespace` field +
      lex-driven skip in `parse_ast.go`, plus v2's
      `convertGrammarToV1` preserving WHITESPACE delimiters from
      the v2 lex. proto-ip's loader strips the WHITESPACE symbols
      from each grammar's lex before calling `ParseCST`.
- After the lex-driven WS fix, the fuzz harness no longer needs ANY
  whitespace exception: gluon and net.ParseIP / netip agree on
  whitespace inputs. ~452k IPv4 / 185k IPv6 / 108k CIDR execs, zero
  disagreements.
- Three gluon commits now in main: e121e84 (require full input),
  74d04c3 (don't skip trailing comments), and the lex-driven WS
  change (committed alongside this proto-ip work).
- Lessons:
    - Two pieces of the user's feedback today were essential:
      "do you not understand the fucking point of fuzzing" steered
      me away from masking the `(*` finding; "can't the lexdescriptor
      for this grammar just not include newlines" steered me to the
      right lex-level fix instead of the failed DefaultIsLexical
      attempt.
    - When fuzz finds something, fix the underlying bug. Always.
- Next: proto-fixedlength initial implementation per CLAUDE.md.

## 2026-05-26

- Added `RDAPLookup` gRPC service for RDAP registration lookups on IP
  addresses and CIDR blocks.
- New `proto/ippb/rdap.proto` defines: `RDAPEntity`, `RDAPEvent`,
  `RDAPNetwork`, `RDAPResponse`, and the `RDAPLookup` service
  (`LookupIP(IP)`, `LookupCIDR(CIDR)`) — both unary RPCs.
- New `rdap/` package:
    - `bootstrap.go`: fetches IANA IPv4/IPv6 bootstrap files (RFC 7484)
      on startup; resolves any IP to the correct RIR RDAP base URL via
      most-specific prefix match.
    - `client.go`: HTTP RDAP client; parses vCard entities (fn, emails),
      events, links, status from the JSON response into `RDAPNetwork`
      proto; preserves raw JSON in `RDAPResponse.raw_json`.
    - `server.go`: gRPC server adapter implementing `RDAPLookupServer`.
- New `cmd/rdap-server/` — binds port 50098 by default.
- New `cmd/rdap-client/` — CLI driver for `ip` and `cidr` subcommands,
  used by `LET_IT_RIP.sh`.
- Smoke tests verified live against ARIN (8.8.8.8, 2001:4860:4860::8888)
  and APNIC (1.1.1.0/24) — correct RIR routing and structured response
  fields confirmed.
- `LET_IT_RIP.sh` updated with RDAP smoke test section.

## 2026-06-01

- Added `GeoLookup` gRPC service for best-effort IP geolocation, combining
  two free/open sources and merging them (most granular wins).
- New `proto/ippb/geo.proto`: `GeoLocation` (optional lat/lon + admin fields +
  `GeoGranularity`), `GeoSourceResult` (provenance/attribution/authoritative),
  `GeoResponse` (`best` + per-source `sources`), service `GeoLookup`
  (`LookupIP`, `LookupCIDR`), default port 50099.
- New `geoip/` package:
    - `geofeed_csv.go`: RFC 8805 CSV parser (5 cols, `#` comments incl. inline,
      blank lines, missing trailing fields, malformed-row skip) + longest-prefix
      match. Table-tested.
    - `geofeed.go`: RFC 9632 geofeed discovery via TWO channels — inline
      `Geofeed <url>` in the RDAP body (ARIN), and the RPSL `geofeed:`
      attribute over whois port 43 (RIPE/APNIC, found via RDAP's `port43`).
      The whois channel was added after confirming RDAP does NOT carry the URL
      on RIPE (it only declares the `geofeed1` conformance) — without it the
      source would be dead. Fetches+caches the CSV. No RPKI verification yet.
      Verified live against the Pfcloud `2a05:b0c6:a200::/39` feed.
    - `dbip.go`: DB-IP City Lite MMDB source via
      `oschwald/maxminddb-golang/v2` (only new dep). Decodes the GeoIP2-City
      schema; `(0,0)` → no coordinates.
    - `merge.go`: pure merge — authoritative geofeed admin fields win,
      coordinates filled from DB-IP, `best_source` = granularity contributor.
    - `cache.go`, `source.go`, `server.go`.
- New `cmd/geo-server` (port 50099, `-data-dir`) + `cmd/geo-client` (ip/cidr).
- `setup.sh`: idempotent DB-IP City Lite download into gitignored `data/geoip/`
  (current/previous month, warn-not-fail on failure); `geo.proto` registered.
  `.gitignore` ignores `/data/`. `build.sh` builds the geo binaries.
- Decisions confirmed with user: sources = geofeeds + DB-IP Lite (excluded
  IP2Location LITE share-alike + GeoLite2 account-gating); download-at-setup
  cache; merged-best-plus-per-source response.
- Tests: CSV parser, longest-prefix, merge precedence/no-mutation, and a live
  DB-IP decode (`8.8.8.8` → US + coords) that skips when the DB is absent. All
  green. DB-IP 2026-06 fetched and verified locally.
- README documents the service + the required DB-IP CC BY 4.0 attribution.
- Next: optionally add geofeed RPKI verification and more source DBs.
