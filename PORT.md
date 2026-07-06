# thesecretlab-dev/hypersdk — avalanchego v1.14.2 port

This is a fork of [ava-labs/hypersdk](https://github.com/ava-labs/hypersdk),
forked at upstream commit `d6a97266` and ported to **avalanchego v1.14.2
(RPC protocol 45)** on branch `port/avalanchego-v1.14.2`. Upstream hypersdk
pins avalanchego v1.13.x (protocol 39), which can no longer register with
current Fuji/mainnet nodes; this fork exists to run hypersdk-based VMs against
live avalanchego.

The port was executed against a pre-verified breakage inventory
([INVENTORY.md](./INVENTORY.md)): ~18 mechanical error sites across 5 upstream
changes, plus two semantic items (B1, B2 below) and one forced serialization
change (C below). hypersdk's action/state abstractions took zero hits.

## Port commits

| Commit | Scope |
|---|---|
| `bb1e77fc` | **A — mechanical.** `cache/lru` move, `logging.Logger` args (`proposer.New`, `corruptabledb.New`), `trace.Config` restructure, canoto v0.18 DSL + codegen regeneration, `NewHTTPHandler` (return nil; path-based `CreateHandlers` retained), p2p `NewClient` NodeSampler plumbing, `version.Current`. |
| `529ebd3a` | **B2 — state sync deferred loudly** (see below). |
| `5d96efd3` | **B1 — toEngine channel → `WaitForEvent` pull model** (ref: subnet-evm #1598). The VM owns a buffered(1) channel the builders push `common.PendingTxs` to; `WaitForEvent` drains it, preserving wake-on-tx latency. The vmtest harness drives `WaitForEvent` directly, so the suite exercises the pull path. |
| `d174ab1e` | **C — block golden vector regenerated for canoto v0.18, by attested diff** (see "Wire format" below). |

## Known limitations

### Wire format tracks canoto; cross-version data compatibility is not maintained

hypersdk's block/tx wire format is defined by the canoto version that
avalanchego pins, and it moves with upstream. avalanchego v1.14.2 requires
canoto v0.18, which changed the encoding of repeated message fields (each
element of a `repeated pointer` field gained a length-delimited wrapper).
Consequences, accepted deliberately in this port:

- **Block bytes — and therefore block IDs — differ between canoto versions
  for identical logical content.** For the v0.15 → v0.18 bump specifically,
  transaction bytes and tx IDs are unchanged (Transaction contains no
  repeated-message fields); that invariance is *not* guaranteed for future
  bumps.
- **Every future avalanchego bump is potentially a hard fork:** all
  validators must move in lockstep, and chain data serialized under one
  format does not replay across the boundary. Today protocol-version
  enforcement already forces validator lockstep, so this costs nothing for a
  small validator set — but a deployment with outside validators must plan
  upgrades around it.
- The serialization canary (`chain.TestParseHardcodedBlock`) stays. Its
  golden vector was regenerated **by attested diff, not overwrite**:
  `chain.TestWireFormatMigrationCanotoV015ToV018` retains the old vector and
  proves the new one encodes the same block, with the byte delta exclusively
  the documented v0.18 nesting change. Future format bumps should be
  re-attested the same way — the canary exists to catch *unintended* drift,
  and an upstream-forced change is accepted only with equivalent evidence.

### Merkledb state sync is deferred (bootstrap by replay)

avalanchego v1.14.x removed `x/sync` (the merkledb network state-sync
manager) in its Firewood-era state-layer rework, with no drop-in
replacement. Rather than vendoring dead code against a moving merkledb API,
this port defers state sync: nodes bootstrap by full block replay from
genesis. The deferral is loud — any attempt to construct a merkle syncer or
register its handlers returns `ErrMerkleStateSyncUnsupported`
(`statesync/merkledb_adapter.go`, which carries the full rationale).

**Re-evaluation triggers:** (a) chain state grows large enough that genesis
replay is too slow for new validators, or (b) avalanchego's Firewood-era
state sync stabilizes into an adoptable API.

### Windows: test fixtures don't build

avalanchego v1.14.2's `utils/ulimit` is Unix-only (lost `ulimit_windows.go`),
which breaks `tests/fixture` / `tests/e2e` compilation on Windows via the
`config` import. Core packages build and test fine on Windows; run the full
suite on Linux.

## Verification status

- `go build ./...`: green (69 core packages) against avalanchego v1.14.2,
  except the Windows-only fixture issue above.
- Unit suite: green, including the regenerated serialization canary, the
  wire-format migration attestation, and round-trip tests
  (`TestBlockSerialization`, `TestResultMarshalIdentity`) across every block
  shape.
- Behavioral guard: the vmtest harness drives the B1 `WaitForEvent` path
  with a bounded wait, so a wiring regression that degraded block building
  to timer-only would fail the existing suite.
