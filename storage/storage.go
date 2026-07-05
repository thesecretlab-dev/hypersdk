// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package storage

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/hypersdk/internal/pebble"
	"github.com/ava-labs/hypersdk/utils"
)

func New(cfg pebble.Config, chainDataDir string, namespace string, registerer prometheus.Registerer) (database.Database, error) {
	path, err := utils.InitSubDirectory(chainDataDir, namespace)
	if err != nil {
		return nil, err
	}

	db, err := pebble.New(path, cfg, registerer)
	if err != nil {
		return nil, err
	}

	// v1.14.x: corruptabledb.New gained a logging.Logger arg. storage.New has no
	// logger in scope; NoLog preserves corruption detection (only its warning log
	// is suppressed). Thread a real logger here if these warnings become needed.
	return corruptabledb.New(db, logging.NoLog{}), nil
}
