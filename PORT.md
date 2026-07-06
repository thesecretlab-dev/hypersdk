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
| `73ef109e` | Pebble `Compact(nil, nil)` nil-bounds fix, re-verified against v1.14.2 merkledb and carried forward from the pre-port VEIL tree. |
| `5d6307d6` | **B2 follow-up** — found by the first real protocol-45 node start: the deferral's loudness was at construction (which the VM calls unconditionally in `initStateSync`), so it blocked VM init entirely, including on the replay-bootstrap path B2 exists to support. Moved to `Start()`, the point actually reachable only when a sync is genuinely initiated. |

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

### gnark / gnark-crypto pinned below avalanchego's MVS preference

avalanchego v1.14.2 pulls `github.com/consensys/gnark-crypto v0.18.1` as an
indirect dependency. veilvm (the example VM this port validates against)
pins **gnark v0.10.0** / **gnark-crypto v0.12.2** for its zk shielded-ledger
circuit and holds gnark-crypto there via `replace` in its own `go.mod`,
overriding Go's MVS resolution. This is a deliberate decision, not an
oversight:

- **Why:** gnark v0.10.0's circuit/proving-key fixtures are compiled and
  fingerprinted artifacts. Taking gnark-crypto v0.18.1 without a matching
  gnark bump risks (or outright breaks, given the API deltas across six
  minor versions) fixture incompatibility mid-campaign, for a dependency
  bump that has nothing to do with the avalanchego port. The `replace` keeps
  the zk stack byte-identical — verified: veilvm's zk test suite (fixture
  load + proof verify) passes unchanged against the pin.
- **Cost:** VEIL runs older gnark/gnark-crypto than upstream avalanchego's
  own dependency graph prefers, and older than current upstream releases.
  That is a security-relevant gap, not just a build note — see the survey
  below.
- **Resolution trigger:** the upgrade happens at **circuit v2** (a separate,
  later campaign), when the shielded-ledger circuit is rewritten and its
  fixtures (proving/verifying keys, gnark-crypto-dependent serialized
  artifacts) are regenerated anyway. Bumping gnark/gnark-crypto opportunistically
  before then would be exactly the kind of change this port's own
  discipline argues against — moving a foundational dependency without a
  reason tied to the task at hand.

**Security survey (gnark v0.10.0 → current, gnark-crypto v0.12.2 → current),
performed 2026-07-06, no upgrade taken:**

| Advisory | Component | Affected | Fixed | Applies to VEIL? |
|---|---|---|---|---|
| GHSA-fj2x-735w-74vq — `Vector.ReadFrom` unchecked length field, OOM DoS | gnark-crypto | v0.9.1 – v0.18.0 | v0.18.1 / v0.19.1 | **Unconfirmed, treat as live risk.** VEIL's `SubmitBatchProof`/`ClearBatch` actions deserialize attacker-submitted proof + public-witness bytes — exactly the untrusted-input path this advisory targets. Not confirmed whether gnark v0.10.0's `groth16bn254.Proof.ReadFrom` / witness `UnmarshalBinary` actually route through the vulnerable `bn254/fr/vector.go` `Vector.ReadFrom`, since a Groth16 proof itself is fixed-size (no vector), but witness deserialization plausibly is. The existing `MaxProofBytesSize` (131072 bytes) cap at the action layer bounds the *total submission size* but does **not** confirmedly stop a forged internal length field from triggering an oversized allocation attempt before the read fails — the two checks are independent. **Recommended next step (not done here): a targeted fuzz/oversized-length-prefix test against the actual witness-deserialization call, before circuit v2, independent of the library bump.** |
| GHSA-fr8m-434r-g3xp (CVE-2023-44273) — ECDSA/EdDSA deserialization missing range check | gnark-crypto | < v0.12.0 | v0.12.0 | **Not affected** — our pin (v0.12.2) is already past the fix. |
| GHSA-pffg-92cg-xf5c — `ExpGLV` incorrect results in pairing target group GT | gnark-crypto | ≤ v0.12.0 | v0.12.1 | **Not affected** — our pin (v0.12.2) is already past the fix. Also explicitly not Groth16-verification-relevant per the advisory (G1/G2 GLV unaffected; standard `Exp`/`ExpCyclotomic` unaffected). |
| GHSA-q3hw-3gm4-w5cr — Groth16 multi-commitment soundness break | gnark | ≤ v0.10.0 | v0.11.0 | **Version-vulnerable but not exploitable in our circuit.** Confirmed by grep: `shielded_ledger_circuit.go`/`clearhash_circuit.go` call zero `api.Commit(...)` — the circuit uses only `std/hash/sha2` and `std/math/uints` gadgets. No commitments, multiple or otherwise. |
| GHSA-9xcg-3q8v-7fq6 — Groth16 single-commitment breaks zero-knowledge property | gnark | ≤ v0.10.0 | v0.11.0 | **Not exploitable**, same basis — no `api.Commit(...)` usage at all. |
| GHSA-95v9-hv42-pwrj — in-circuit EdDSA/ECDSA signature malleability (missing `0 ≤ S < order` check) | gnark | < v0.13.0 | v0.14.0 | **Not exploitable in our circuit.** Confirmed by grep: no `std/signature/{eddsa,ecdsa}` usage anywhere in VEIL's circuits. hypersdk's own ed25519 auth (outside the SNARK) is unaffected — this advisory is specifically about signature checks done *inside* a gnark circuit. |
| GHSA-9fvj-xqr2-xwg8 — fake-GLV scalar multiplication DoS/incorrectness | gnark | ≤ v0.12.0 | v0.13.0 | **Likely not applicable, not fully confirmed.** Fake-GLV is gnark's fallback for curves without native GLV endomorphism support; VEIL's circuits and verifier are BN254 (`groth16bn254`/`plonkbn254`), which has true-GLV support in gnark, so the fake-GLV code path likely isn't exercised — but the advisory text doesn't enumerate affected curves explicitly, so this is an inference, not a confirmed non-issue. |
| GHSA-cph5-3pgr-c82g — OOM during verifying/proving-key deserialization | gnark | ≤ v0.11.0 | v0.11.1 | **Low practical exploitability.** VEIL's verifier loads its Groth16 verifying key from a local fixture path at startup (`loadGroth16VK`), not from attacker-submitted input at runtime; proving keys are prover-side/local-only and never touch the verifier's untrusted-input path. |

**Net read:** the two items that matter are (1) the `Vector.ReadFrom` OOM DoS,
which touches VEIL's actual untrusted-input surface and deserves a cheap,
independent verification step before Fuji regardless of the gnark-crypto
version question, and (2) the fake-GLV item, which is probably fine given
BN254 but wasn't nailed down with full confidence. Everything else pinned
below is either already fixed by our exact pin or structurally inapplicable
to a circuit that does no in-circuit commitments and no in-circuit signature
verification. None of this changes the circuit-v2 upgrade trigger; it does
mean the OOM-DoS check on witness deserialization is worth doing sooner,
independently, on the current pin.

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
