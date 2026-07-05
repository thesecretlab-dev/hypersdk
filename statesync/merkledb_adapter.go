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
// The deferral is LOUD, not silent: constructing a merkle syncer or registering
// its handlers returns ErrMerkleStateSyncUnsupported so any attempt to *enable*
// merkledb state sync fails clearly instead of silently no-op'ing. VMs that do
// not use merkle state sync (the replay-bootstrap path) are unaffected.
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

// MerkleSyncer preserves the pre-port API surface so dependents compile, but
// every operation fails loudly — merkledb state sync is deferred (see header).
type MerkleSyncer[T MerkleSyncerBlock] struct {
	log        logging.Logger
	registerer prometheus.Registerer
	merkleDB   merkledb.MerkleDB
	network    *p2p.Network
}

// NewMerkleSyncer fails loudly: enabling merkledb state sync is unsupported in
// this port. A VM that does not use merkle state sync never calls this.
func NewMerkleSyncer[T MerkleSyncerBlock](
	_ logging.Logger, _ merkledb.MerkleDB, _ *p2p.Network,
	_ uint64, _ uint64, _ merkledb.BranchFactor, _ int, _ prometheus.Registerer,
) (*MerkleSyncer[T], error) {
	return nil, ErrMerkleStateSyncUnsupported
}

func (*MerkleSyncer[T]) Start(context.Context, T) error            { return ErrMerkleStateSyncUnsupported }
func (*MerkleSyncer[T]) Wait(context.Context) error                { return ErrMerkleStateSyncUnsupported }
func (*MerkleSyncer[T]) Close() error                              { return nil }
func (*MerkleSyncer[T]) UpdateSyncTarget(context.Context, T) error { return ErrMerkleStateSyncUnsupported }

// RegisterHandlers fails loudly: this node cannot serve merkledb range/change
// proofs to syncing peers (x/sync removed). Serving is unnecessary on the
// replay-bootstrap path.
func RegisterHandlers(_ logging.Logger, _ *p2p.Network, _ uint64, _ uint64, _ merkledb.MerkleDB) error {
	return ErrMerkleStateSyncUnsupported
}
