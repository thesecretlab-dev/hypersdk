# hypersdk → avalanchego v1.14.2 — Phase 1 breakage inventory

> **Outcome (2026-07-06):** the port is complete; see [PORT.md](./PORT.md) for
> the commit map and resolution of each item below. Of note: the A4 flag
> ("confirm canoto's wire output is unchanged") **did** escalate — canoto
> v0.18 changed the repeated-message wire encoding. It was resolved by
> regenerating the serialization canary's golden vector via attested diff
> (`chain.TestWireFormatMigrationCanotoV015ToV018`); the wire-format posture
> is documented in PORT.md. B1 and B2 resolved as recommended.

Date: 2026-07-05
Method: forked hypersdk at pinned commit `d6a97266`, branch
`port/avalanchego-v1.14.2`; `go mod edit -go=1.24`,
`-require avalanchego@v1.14.2`; `go mod tidy`; `go build ./...`. **No fixes
applied.** One file (`statesync/merkledb_adapter.go`) was transiently stubbed
*only* to get past a load-blocking removed package and enumerate the rest of the
surface; it was reverted before this inventory was written.

Target note (amendment 1): inventoried against v1.14.2 (protocol 45). The
v1.14.x pattern is stable within the minor; Phase 2 re-queries live Fuji
`rpcProtocolVersion` before committing the final pin.

## Headline

The bump is **far smaller than a minor-version jump suggests**. hypersdk's core
compiles against v1.14.2 with **~18 mechanical error sites across 5 changes**,
plus **exactly 2 semantic items** — and both have reference implementations
(one in avalanchego itself, one in subnet-evm). No category-C unknowns. This
contradicts the "39→45 = large port" worry: the surface is mostly renames,
added `logging.Logger` args, and generated-code regeneration.

## Category A — mechanical (renames / signature arity / regeneration)

| # | Change | Sites | Fix |
|---|---|---|---|
| A1 | `cache.LRU` / `avacache.LRU` removed; LRU is now the `cache/lru` subpackage (`lru.Cache`) | `internal/validators/proposer_monitor.go:41,55`; `snow/vm.go:168,306` | swap import to `cache/lru`, type to `lru.Cache` |
| A2 | `proposer.New` gained a trailing `logging.Logger` arg | `internal/validators/proposer_monitor.go:53` | pass the logger |
| A3 | `corruptabledb.New` gained a trailing `logging.Logger` arg | `storage/storage.go:26` | pass the logger |
| A4 | `canoto.FieldTypeFromField` gained a `bool` arg (canoto codegen bump) | 8 sites across `chain/*.canoto.go` (all generated) | regenerate with the newer `canoto` tool — do not hand-edit |
| A5 | `trace.Config.Enabled` field removed (trace config restructured) | `snow/config.go:53` | drop/replace per new `trace.Config` |

A1–A3 and A5 are a few lines each. A4 is 8 sites but they are **generated
files** — the fix is bumping the canoto generator and regenerating, not editing.
(Confirm canoto's wire output is unchanged when regenerating; if it changed,
A4 escalates toward B — flagged for Phase 2 verification.)

## Category B — semantic (design decision) — BOTH are stop-and-report

### B1 — `block.ChainVM.Initialize` dropped the `chan<- common.Message` param
- Site: `snow/snow_vm.go:15` (`*SnowVM` no longer satisfies `block.ChainVM`).
- v1.14.2 removed the "toEngine" message channel from `Initialize` and replaced
  the VM→engine signal with a **pull model**: `common.VM` now has
  `WaitForEvent(ctx context.Context) (Message, error)` — "blocks until the
  context is cancelled or a message is returned."
- **Touches consensus lifecycle → stop-and-report.**
- **Reference implementation:** subnet-evm commit **#1598 (2025-07-09) "Use
  explicit subscriptions instead of toEngine channel."** hypersdk's block-build
  trigger (currently pushed onto the channel) must be re-expressed as a
  `WaitForEvent` the engine pulls. Tractable with the reference; the design
  question is how hypersdk's builder wakes the `WaitForEvent` loop.

### B2 — `avalanchego/x/sync` removed entirely
- Site: `statesync/merkledb_adapter.go` (wraps `sync.Manager`,
  `sync.NewManager`, `NewGet{Range,Change}ProofHandler`, `ErrAlreadyClosed`).
- The `x/` tree in v1.14.2 is only `archivedb / blockdb / merkledb`; the
  merkledb **network state-sync manager package was deleted** (part of
  avalanchego's Firewood-era state-layer rework — cf. coreth #1402 "Firewood
  upgrade", subnet-evm #1883 "Helicon").
- **Touches state serialization / sync → stop-and-report.**
- **Weakest reference:** coreth/subnet-evm use EVM state sync, not merkledb
  `x/sync`, so there is no drop-in port. Options for Phase 2 review: (a) vendor
  the last-known `x/sync` against the current `merkledb` API, (b) adopt whatever
  sync avalanchego now exposes for merkledb, or (c) confirm whether HyperSDK
  state sync is even required for our deployment (VEIL is small; a node can
  bootstrap by full replay, deferring merkle state sync). **This is the one item
  that genuinely needs a decision, not a pattern.**

## Category C — unknown
None. Every error resolves to a named change above.

## Environmental (not an API breakage)
- `utils/ulimit` lost `ulimit_windows.go` in v1.14.2 (v1.13.1-rc had it), so it
  is Unix-only. Only hypersdk's **test fixtures** (`tests/fixture`, `tests/e2e`)
  hit it, via `config`. Irrelevant on the Linux CI where a real port/build runs;
  a non-issue for the port itself. Noted so it isn't mistaken for a code break.

## Toolchain
- `go` directive bumped 1.23.7 → 1.24 (v1.14.2 needs ≥1.24.11; we build with
  1.26.4). subnet-evm did the same (#1821).

## Recommendation for Phase 2

Proceed — but the two stop-and-report items gate it:
- **B1** is mechanical-with-a-reference (WaitForEvent, subnet-evm #1598). Low risk.
- **B2** (x/sync) is the real decision and should be resolved *before* touching
  code: decide sync strategy (vendor / adopt-new / defer-to-replay). Everything
  else (A1–A5, B1) is a day of work with references.

Net: this is a **tractable adopt-and-maintain**, not a rewrite — consistent with
the fact that hypersdk's own action/state abstractions (where VEIL lives) took
**zero** hits in the compile.
