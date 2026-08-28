package chain

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShamir2of3(t *testing.T) {
	secret := bytes32(9)
	shares, err := shamirSplit(secret, 2, 3)
	require.NoError(t, err)
	require.Len(t, shares, 3)

	got, err := shamirCombine([]shamirShare{shares[0], shares[2]}, 2)
	require.NoError(t, err)
	require.Equal(t, secret, got)
}

func TestShamir1of1(t *testing.T) {
	secret := bytes32(3)
	shares, err := shamirSplit(secret, 1, 1)
	require.NoError(t, err)
	got, err := shamirCombine(shares, 1)
	require.NoError(t, err)
	require.True(t, bytes.Equal(secret, got))
}
