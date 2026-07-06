// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestCompactNilLimit verifies Compact follows the database.Database spec for
// nil bounds: nil [limit] means "after all keys", not pebble's native "before
// all keys". merkledb depends on this — it calls Compact(nil, nil) after a
// rebuild — and raw pebble rejects the call (start must be < limit).
func TestCompactNilLimit(t *testing.T) {
	r := require.New(t)

	db, err := New(t.TempDir(), NewDefaultConfig(), prometheus.NewRegistry())
	r.NoError(err)
	defer func() {
		r.NoError(db.Close())
	}()

	// Empty database: nothing to compact, must not error.
	r.NoError(db.Compact(nil, nil))

	r.NoError(db.Put([]byte("a"), []byte("1")))
	r.NoError(db.Put([]byte("m"), []byte("2")))
	r.NoError(db.Put([]byte("z"), []byte("3")))

	// The merkledb rebuild call shape.
	r.NoError(db.Compact(nil, nil))

	// Nil limit with a non-nil start.
	r.NoError(db.Compact([]byte("m"), nil))

	// Empty key range: no-op, must not error.
	r.NoError(db.Compact([]byte("z"), []byte("a")))

	// Explicit bounds still work.
	r.NoError(db.Compact([]byte("a"), []byte("z")))
}
