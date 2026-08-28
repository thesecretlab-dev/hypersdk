package chain

import (
	"encoding/hex"
	"testing"

	"github.com/ava-labs/hypersdk/codec"
	"github.com/stretchr/testify/require"
)

func testGossipSerializer(t *testing.T, keyHex string, required bool) *GossipTxSerializer {
	t.Helper()
	inner := &BatchedTransactionSerializer{
		Parser: NewTxTypeParser(codec.NewTypeParser[Action](), codec.NewTypeParser[Auth]()),
	}
	s, err := NewGossipTxSerializer(inner, keyHex, required)
	require.NoError(t, err)
	return s
}

func TestGossipEncryptionRoundtrip(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32))
	s := testGossipSerializer(t, key, true)
	out := s.Marshal([]*Transaction{})
	require.True(t, isEncryptedGossip(out))
	txs, err := s.Unmarshal(out)
	require.NoError(t, err)
	require.Empty(t, txs)
}

func TestGossipRejectsPlaintextWhenRequired(t *testing.T) {
	key := hex.EncodeToString(bytes32(1))
	s := testGossipSerializer(t, key, true)
	inner := &BatchedTransactionSerializer{
		Parser: NewTxTypeParser(codec.NewTypeParser[Action](), codec.NewTypeParser[Auth]()),
	}
	plain := inner.Marshal([]*Transaction{})
	_, err := s.Unmarshal(plain)
	require.ErrorIs(t, err, ErrGossipPlaintextRejected)
}

func TestGossipWrongKeyFails(t *testing.T) {
	s1 := testGossipSerializer(t, hex.EncodeToString(bytes32(1)), true)
	s2 := testGossipSerializer(t, hex.EncodeToString(bytes32(2)), true)
	out := s1.Marshal([]*Transaction{})
	_, err := s2.Unmarshal(out)
	require.ErrorIs(t, err, ErrGossipDecryptFailed)
}

func TestGossipRequiredEmptyKeyFails(t *testing.T) {
	inner := &BatchedTransactionSerializer{
		Parser: NewTxTypeParser(codec.NewTypeParser[Action](), codec.NewTypeParser[Auth]()),
	}
	_, err := NewGossipTxSerializer(inner, "", true)
	require.ErrorIs(t, err, ErrGossipEncryptionKey)
}

func bytes32(v byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = v
	}
	return b
}
