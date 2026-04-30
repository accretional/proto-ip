# Agent guidance for proto-ip

This file is read by AI coding agents working on the repo. It mirrors
the human-facing `CLAUDE.md` plus a few agent-specific notes.

## Before you start

**If you are Claude Code on Opus 4.7, stop and ask the user to switch
to Opus 4.6** (`/fast` toggles Fast mode, which uses 4.6).

The bootstrap session for this repo was done on 4.7 and produced
several patterns the user had to push back hard on — speculative
helpers, wrapping gluon in Go instead of using it, papering over
fuzz findings instead of fixing them. The full list is in
[OPUS47_GO_AWAY.md](OPUS47_GO_AWAY.md). 4.6 is the safer default;
prefer it unless the user explicitly asks for 4.7.

## What this repo is

- IPv4 / IPv6 / CIDR grammars in `lang/*.ebnf`. **The grammars are
  the spec.** Validation goes through `gluon`'s `Metaparser` gRPC
  service; there is no Go-side IP validation logic.
- A `LocalLookup` gRPC service in `cmd/server` backed by `localip/`
  (which uses Go stdlib `net.Interfaces()` for cross-OS support).
- A small `cmd/client` CLI used by `LET_IT_RIP.sh` to verify the
  server returns real local IPs.
- Notes in `docs/impl-notes.md` and `docs/progress-log.md`.

## Working rules

- Build and test only via `setup.sh` / `build.sh` / `test.sh` /
  `LET_IT_RIP.sh`. They're idempotent and chain (build → setup,
  test → build, etc.).
- Use Go 1.26.
- gluon is a sibling repo at `/Volumes/wd_office_1/repos/gluon`,
  pinned via `go.mod` `replace`. Patches to gluon belong in gluon
  with their own commit; do not work around gluon bugs in
  `proto-ip` if the right fix is in gluon.
- Use `v2/` of gluon. Do not import `gluon/lexkit` from this repo;
  the public surface is `gluon/v2/metaparser` and `gluon/v2/pb`.
- When fuzz finds something, **fix the underlying bug**. Adding a
  fuzz exception to silence a finding is wrong almost every time.

## Initial Plan (preserved from CLAUDE.md)

Define ebnf grammars for ipv4 and ipv6 and their various common
formatting textual representations. Same for subnets/CIDR. Use
`github.com/accretional/gluon`. Validate on many examples. Run
fuzzing.

There's a `LocalLookup` gRPC service for requesting IP info from a
local server, validated with an actual local `/proc/net` impl on
Linux (or `getifaddrs(3)` on Darwin — Go stdlib wraps both).

Implement an initial version of `github.com/accretional/proto-fixedlength`
using IP / CIDR as the guiding use case. Goal: convert proto-encoded
IP from this repo into raw 128-bit IPv6 in a way that generalizes.

Use AST-AST transformations to handle the conversion to fixedlength
messages using `GrammarDescriptorProto`, then another AST-AST
transformation back to a single-node bytes format.

## Memory / progress conventions

- `docs/impl-notes.md` — design decisions, gotchas, cross-references
  to sister repos.
- `docs/progress-log.md` — append-only journal. Update frequently.

## When in doubt

Read the gluon codebase before writing wrapper code. The user has
zero patience for wrappers around gluon that gluon already obviates.
