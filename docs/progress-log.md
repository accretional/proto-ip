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
