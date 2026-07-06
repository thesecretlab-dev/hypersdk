// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

// DEFERRED STATE SYNC (avalanchego v1.14.x port — see INVENTORY.md item B2).
//
// avalanchego removed the `x/sync` package (the merkledb network state-sync
// manager this adapter wrapped) as part of its Firewood-era state-layer rework.
// There is no drop-in replacement: coreth/subnet-evm use EVM state sync, not
// merkledb's, so nothing upstream ports cleanly here.
//
// This port DEFERS merkledb state sync rather than vendoring dead code against a
// moving merkledb API. A node without state sync bootstraps by full block
// replay from genesis — acceptable and fast at small state sizes (e.g. VEIL,
// whose entire chain state is days old). State sync is a scale optimization for
// joiners of networks with deep state, which this deployment does not have.
//
// The deferral is LOUD at the right boundary: STARTING a merkledb state sync
// fails with ErrMerkleStateSyncUnsupported, so any attempt to *exercise* it
// fails clearly instead of silently no-op'ing. Construction and handler
// registration succeed (with warnings) because the VM performs both
// unconditionally during Initialize — erroring there would break every node,
// including the supported replay-bootstrap path. (Phase 3 finding: the
// original construction-time error prevented VM init on a fresh chain.)
//
// RE-EVALUATION TRIGGER: revisit when either (a) chain state grows large enough
// that genesis replay is too slow for new validators, or (b) avalanchego's
// Firewood-era state sync stabilizes into an adoptable API. Until then this is a
// tracked known limitation (see the fork README).

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var _ Syncer[MerkleSyncerBlock] = (*MerkleSyncer[MerkleSyncerBlock])(nil)

// ErrMerkleStateSyncUnsupported is returned by every merkledb-state-sync entry
// point in this port. See the file header (INVENTORY.md B2).
var ErrMerkleStateSyncUnsupported = errors.New(
	"merkledb state sync is not supported in this avalanchego v1.14.x port: " +
		"avalanchego removed x/sync (Firewood-era rework); bootstrap by full " +
		"block replay instead. See fork README / INVENTORY.md item B2",
)

type MerkleSyncerBlock interface {
	fmt.Stringer
	GetStateRoot() ids.ID
}

// MerkleSyncer preserves the pre-port API surface so dependents compile. It is
// a disabled syncer: construction succeeds (the VM constructs it during every
// Initialize, including on the supported replay-bootstrap path), but actually
// STARTING a merkle sync fails loudly. The loudness boundary is the point
// where merkledb state sync would be exercised, not VM init.
type MerkleSyncer[T MerkleSyncerBlock] struct {
	log logging.Logger
}

// NewMerkleSyncer returns a disabled syncer and warns. It must not error: the
// VM constructs it unconditionally in initStateSync, and returning an error
// here would prevent the VM from initializing at all — including on the
// replay-bootstrap path this port explicitly supports.
func NewMerkleSyncer[T MerkleSyncerBlock](
	log logging.Logger, _ merkledb.MerkleDB, _ *p2p.Network,
	_ uint64, _ uint64, _ merkledb.BranchFactor, _ int, _ prometheus.Registerer,
) (*MerkleSyncer[T], error) {
	log.Warn("merkledb state sync is deferred in this port; a state sync attempt will fail loudly (INVENTORY.md B2)")
	return &MerkleSyncer[T]{log: log}, nil
}

// Start is the loud gate: the engine only calls it when a state sync has
// actually been initiated (Client.Accept past the min-blocks threshold).
func (m *MerkleSyncer[T]) Start(_ context.Context, target T) error {
	m.log.Error("refusing to start merkledb state sync", zap.Stringer("target", target))
	return ErrMerkleStateSyncUnsupported
}

// Wait is only reachable while a sync is active, which Start prevents.
func (*MerkleSyncer[T]) Wait(context.Context) error { return ErrMerkleStateSyncUnsupported }
func (*MerkleSyncer[T]) Close() error               { return nil }

// UpdateSyncTarget is invoked for every pre-ready accepted block via the
// VM's subscription, regardless of whether a sync is active. Since Start
// always refuses, there is never an active merkle sync to retarget — this
// must be a silent no-op or replay bootstrap would abort.
func (*MerkleSyncer[T]) UpdateSyncTarget(context.Context, T) error { return nil }

// RegisterHandlers no-ops with a warning: this node cannot serve merkledb
// range/change proofs to syncing peers (x/sync removed). Not serving is
// honest — peers see no handler, the same as talking to a node without state
// sync. It must not error, for the same reason as NewMerkleSyncer.
func RegisterHandlers(log logging.Logger, _ *p2p.Network, _ uint64, _ uint64, _ merkledb.MerkleDB) error {
	log.Warn("merkledb state-sync proof handlers not registered — unsupported in this port (INVENTORY.md B2)")
	return nil
}
