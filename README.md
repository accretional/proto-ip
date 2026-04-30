# proto-ip

Protobuf-native representation of IPv4 / IPv6 / CIDR addresses, plus a
local IP introspection gRPC service (`LocalLookup`) and grammar-driven
validators that ride on
[`accretional/gluon`](https://github.com/accretional/gluon)'s v2
`Metaparser` pipeline.

> **Heads up:** if you're driving this repo with Claude Code, switch
> to Opus 4.6 (`/fast`) before you start. See
> [OPUS47_GO_AWAY.md](OPUS47_GO_AWAY.md) for why.

## What's in here

| Path | What it is |
|---|---|
| `proto/ippb/*.proto` | Wire definitions: `IP` (128-bit canonical), `IPv4Address`, `IPv6Address`, `Subnet`, `CIDR`, `Interface`, `LookupFilter`, `LocalLookup` service. |
| `lang/*.ebnf` | EBNF grammars for IPv4 dotted-decimal, IPv6 (RFC 4291 §2.2 with zone IDs), CIDR. **The grammars are the spec** — there is no Go-side IP validation logic. |
| `lang/gluon_grammar_test.go` | Drives gluon's `Metaparser` gRPC service over bufconn. Asserts each grammar accepts/rejects a representative corpus. |
| `lang/fuzz_test.go` | Cross-checks each grammar against `net.ParseIP` / `netip.ParseAddr` / `netip.ParsePrefix` as oracles. ~150–450k execs/sec depending on grammar. |
| `localip/` | Reads local interfaces (rides on Go stdlib `net.Interfaces()`, which already wraps `/proc/net` on Linux and `getifaddrs(3)` on Darwin). Returns `[]*ippb.Interface`. |
| `cmd/server/` | gRPC `LocalLookup` server (default port 50097). |
| `cmd/client/` | Tiny CLI: `client interfaces` and `client ips`. Used by `LET_IT_RIP.sh`. |
| `setup.sh`, `build.sh`, `test.sh`, `LET_IT_RIP.sh` | Idempotent build/test/run scripts. **Never build, test, or run outside these scripts**, per [CLAUDE.md](CLAUDE.md). |
| `docs/impl-notes.md` | Living design notes. |
| `docs/progress-log.md` | Append-only journal. |
| `AGENTS.md`, `CLAUDE.md` | Agent / human entry points. |

## Wire format invariants

- Every `ippb.IP` is 128 bits split into two `sint64`s (`network_prefix`
  = high 64, `interface_identifier` = low 64).
- IPv4 addresses are stored as IPv4-mapped IPv6 (`::ffff:0:0/96`):
  high 64 = 0, low 64 = `0x0000_FFFF_<v4 octets>`.
- The `version` oneof preserves the client-supplied textual form so
  rendering can round-trip without guessing the original family.

## How to run

```bash
bash LET_IT_RIP.sh
```

That's setup → build → unit/integration/grammar tests → ~5s fuzz
smoke per grammar → start `LocalLookup` server on port 50097 → query
via client → ~15s long fuzz per grammar. If it passes, the project
is healthy.

You can also run pieces individually:

```bash
bash setup.sh   # protoc plugins, proto stubs, go mod tidy
bash build.sh   # builds bin/server, bin/client (calls setup)
bash test.sh    # runs all tests + 3s fuzz per grammar (calls build)
```

## Current state (2026-04-29)

**Shipping:**

- ✅ Project scaffold + idempotent build/test/run scripts
- ✅ Cleaned protos in `proto/ippb/` (fixed inconsistencies in the
  initial drafts: `SubNet→Subnet`, missing imports, undefined
  `InterfaceType→InterfaceClass`, expanded format oneofs)
- ✅ EBNF grammars for IPv4 / IPv6 / CIDR. Octet 0..255, prefix
  0..32 / 0..128, "no leading zeros," IPv6's nine RFC 4291 forms
  (factored by `K`, the count of h16 groups before `::`), v4-mapped
  variants, zone IDs — all expressed structurally in EBNF, no
  Go-side validation
- ✅ Grammar correctness: corpus tests via gluon's gRPC `Metaparser`
  service over bufconn (`lang/gluon_grammar_test.go`)
- ✅ Fuzzing: cross-checks against `net.ParseIP` / `netip.ParseAddr` /
  `netip.ParsePrefix`. Last clean run: 428k IPv4 / 151k IPv6 / 125k
  CIDR execs, zero disagreements (excluding the documented
  whitespace-input limitation — see below)
- ✅ `localip` package + `LocalLookup` gRPC server + client CLI
- ✅ `LET_IT_RIP.sh` exercises the whole flow end-to-end against
  the local host's actual interfaces

**Three upstream gluon patches** landed in support of this work, all
on `github.com/accretional/gluon` `main`:

- [`e121e84`](https://github.com/accretional/gluon/commit/e121e84) —
  `lexkit.ParseAST` now requires the start rule to consume the entire
  input. Before, trailing junk after the start rule was silently
  dropped (`"1.2.3.4junk"` parsed as `"1.2.3.4"`).
- [`74d04c3`](https://github.com/accretional/gluon/commit/74d04c3) —
  EOF check no longer skips trailing comments. Before, an
  unterminated `(*` / `/*` / `//` in user input was treated as an
  EBNF-source comment opener and consumed to EOF
  (`"0::(*"` was silently accepted as IPv6).
- **lex-driven whitespace skipping** (commit pending) — `ParseAST`'s
  whitespace skip now consults the grammar's
  `LexDescriptor.whitespace` instead of hardcoding `' \t\n\r'`. This
  lets a grammar express "no internal whitespace" by shipping a lex
  with no WHITESPACE delimiters, which is what proto-ip's loader does
  (see `stripWhitespaceSymbols` in `lang/gluon_grammar_test.go`).
  Without this fix, `"1 .2.3.4"` and `"1\n.2.3.4"` parsed
  successfully because gluon's default options force every production
  into syntactic mode and skip whitespace between every terminal.

**Deferred:**

- ⏳ `proto-fixedlength` initial implementation. CLAUDE.md spec:
  walk an `ippb.IP` proto via `protoreflect`, copy bits MSB-first
  into a 16-byte buffer, recover via the inverse. Use gluon's
  `astkit` for AST→AST transformation. Not yet started.

## Next steps

1. **proto-fixedlength** — initial implementation using `IP` / `CIDR`
   as the guiding use case. Goal: convert any `ippb.IP` to a single
   16-byte (128-bit) buffer and back. Hook to gluon's
   `GrammarDescriptorProto` + `astkit` AST-AST transformations.
2. **Optionally** split `localip/` into `procfs/` (Linux) and
   `sysctlip/` (Darwin) if a future requirement needs data stdlib
   doesn't surface (FIB LOCAL vs link, link-type detection beyond
   name patterns, etc.). Not needed today.

## References

- Initial intent and invariants: [CLAUDE.md](CLAUDE.md)
- Agent guidance: [AGENTS.md](AGENTS.md)
- Session retro: [OPUS47_GO_AWAY.md](OPUS47_GO_AWAY.md)
- Living design notes: [docs/impl-notes.md](docs/impl-notes.md)
- Append-only progress journal: [docs/progress-log.md](docs/progress-log.md)
- gluon integration notes (in the gluon repo):
  [`v2/proto-ip-integration-notes.md`](https://github.com/accretional/gluon/blob/main/v2/proto-ip-integration-notes.md)
