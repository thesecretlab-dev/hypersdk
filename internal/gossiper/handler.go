// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossiper

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

func flushOutbound[T Tx](ctx context.Context, client *p2p.Client, ser Serializer[T]) {
	if client == nil {
		return
	}
	ob, ok := any(ser).(OutboundMessages)
	if !ok {
		return
	}
	for i := 0; i < 8; i++ {
		b := ob.TakeOutbound()
		if len(b) == 0 {
			return
		}
		_ = client.AppGossip(ctx, common.SendConfig{Validators: 10}, b)
	}
}

var _ p2p.Handler = (*TxGossipHandler)(nil)

type TxGossipHandler struct {
	p2p.NoOpHandler
	log      logging.Logger
	gossiper Gossiper
}

func NewTxGossipHandler(
	log logging.Logger,
	gossiper Gossiper,
) *TxGossipHandler {
	return &TxGossipHandler{
		log:      log,
		gossiper: gossiper,
	}
}

func (t *TxGossipHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) {
	if err := t.gossiper.HandleAppGossip(ctx, nodeID, msg); err != nil {
		t.log.Warn("handle app gossip failed", zap.Error(err))
	}
}
