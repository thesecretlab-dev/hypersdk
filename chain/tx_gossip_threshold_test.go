package chain

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/ava-labs/hypersdk/codec"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/curve25519"
)

func thresholdInner(t *testing.T) *BatchedTransactionSerializer {
	t.Helper()
	return &BatchedTransactionSerializer{
		Parser: NewTxTypeParser(codec.NewTypeParser[Action](), codec.NewTypeParser[Auth]()),
	}
}

func TestConfiguredRequiredImpliesThreshold(t *testing.T) {
	inner := thresholdInner(t)
	_, err := NewConfiguredTxGossipSerializer(inner, hex.EncodeToString(bytes32(1)), true, ThresholdGossipConfig{})
	require.Error(t, err)
}

func TestThresholdRejectsVTG1(t *testing.T) {
	privs, pubs := testCommittee(t, 3)
	s, err := NewThresholdGossipSerializer(thresholdInner(t), ThresholdGossipConfig{
		MinShares: 2, NodePrivateKeyHex: hex.EncodeToString(privs[0]), CommitteePublicHex: pubs,
	})
	require.NoError(t, err)
	aes := testGossipSerializer(t, hex.EncodeToString(bytes32(3)), true)
	vtg1 := aes.Marshal([]*Transaction{})
	require.True(t, isEncryptedGossip(vtg1))
	_, err = s.Unmarshal(vtg1)
	require.Error(t, err)
}

func testCommittee(t *testing.T, n int) ([][]byte, []string) {
	t.Helper()
	privs := make([][]byte, n)
	pubs := make([]string, n)
	for i := 0; i < n; i++ {
		var p [32]byte
		_, err := rand.Read(p[:])
		require.NoError(t, err)
		privs[i] = append([]byte(nil), p[:]...)
		pub, err := curve25519.X25519(p[:], curve25519.Basepoint)
		require.NoError(t, err)
		pubs[i] = hex.EncodeToString(pub)
	}
	return privs, pubs
}

func TestThresholdGossipRejectsT1(t *testing.T) {
	var priv [32]byte
	_, err := rand.Read(priv[:])
	require.NoError(t, err)
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	require.NoError(t, err)
	_, err = NewThresholdGossipSerializer(thresholdInner(t), ThresholdGossipConfig{
		MinShares:          1,
		NodePrivateKeyHex:  hex.EncodeToString(priv[:]),
		CommitteePublicHex: []string{hex.EncodeToString(pub), hex.EncodeToString(pub)},
	})
	require.Error(t, err)
}

func TestThresholdGossipShareExchange2of3(t *testing.T) {
	privs := make([][]byte, 3)
	pubs := make([]string, 3)
	for i := 0; i < 3; i++ {
		var p [32]byte
		_, err := rand.Read(p[:])
		require.NoError(t, err)
		privs[i] = append([]byte(nil), p[:]...)
		pub, err := curve25519.X25519(p[:], curve25519.Basepoint)
		require.NoError(t, err)
		pubs[i] = hex.EncodeToString(pub)
	}
	s0, err := NewThresholdGossipSerializer(thresholdInner(t), ThresholdGossipConfig{
		MinShares: 2, NodePrivateKeyHex: hex.EncodeToString(privs[0]), CommitteePublicHex: pubs,
	})
	require.NoError(t, err)
	s1, err := NewThresholdGossipSerializer(thresholdInner(t), ThresholdGossipConfig{
		MinShares: 2, NodePrivateKeyHex: hex.EncodeToString(privs[1]), CommitteePublicHex: pubs,
	})
	require.NoError(t, err)
	out := s0.Marshal([]*Transaction{})
	require.True(t, isThresholdGossip(out))
	txs, err := s0.Unmarshal(out)
	require.NoError(t, err)
	require.Empty(t, txs)
	ann0 := s0.TakeOutbound()
	require.True(t, isShareGossip(ann0))
	txs, err = s1.Unmarshal(out)
	require.NoError(t, err)
	require.Empty(t, txs)
	ann1 := s1.TakeOutbound()
	require.True(t, isShareGossip(ann1))
	require.Equal(t, 1, len(s0.pending))
	txs, err = s0.Unmarshal(ann1)
	require.NoError(t, err)
	require.Empty(t, txs)
	require.Equal(t, 0, len(s0.pending))
	require.Equal(t, 1, len(s1.pending))
	txs, err = s1.Unmarshal(ann0)
	require.NoError(t, err)
	require.Empty(t, txs)
	require.Equal(t, 0, len(s1.pending))
}

func TestThresholdGossip2of3TwoKeysReconstruct(t *testing.T) {
	privs := make([][]byte, 3)
	pubs := make([]string, 3)
	for i := 0; i < 3; i++ {
		var p [32]byte
		_, err := rand.Read(p[:])
		require.NoError(t, err)
		privs[i] = append([]byte(nil), p[:]...)
		pub, err := curve25519.X25519(p[:], curve25519.Basepoint)
		require.NoError(t, err)
		pubs[i] = hex.EncodeToString(pub)
	}
	s0, err := NewThresholdGossipSerializer(thresholdInner(t), ThresholdGossipConfig{
		MinShares: 2, NodePrivateKeyHex: hex.EncodeToString(privs[0]), CommitteePublicHex: pubs,
	})
	require.NoError(t, err)
	out := s0.Marshal([]*Transaction{})
	require.True(t, isThresholdGossip(out))
	require.Greater(t, len(out), 6)
	tShare, n := int(out[4]), int(out[5])
	require.Equal(t, 2, tShare)
	require.Equal(t, 3, n)
	rest := out[6:]
	shareBlob := n * (x25519KeyLen + 12 + wrappedShareLen)
	require.Greater(t, len(rest), shareBlob)
	inner := rest[:len(rest)-shareBlob]
	shareBytes := rest[len(rest)-shareBlob:]
	var recovered []shamirShare
	for _, priv := range [][]byte{privs[0], privs[1]} {
		for i := 0; i < n; i++ {
			off := i * (x25519KeyLen + 12 + wrappedShareLen)
			eph := shareBytes[off : off+x25519KeyLen]
			wrapped := shareBytes[off+x25519KeyLen : off+x25519KeyLen+12+wrappedShareLen]
			y, err := unwrapShare(priv, eph, wrapped)
			if err != nil {
				continue
			}
			recovered = append(recovered, shamirShare{X: byte(i + 1), Y: y})
		}
	}
	require.GreaterOrEqual(t, len(recovered), 2)
	dataKey, err := shamirCombine(recovered, 2)
	require.NoError(t, err)
	plain, err := openWithKey(dataKey, inner)
	require.NoError(t, err)
	txs, err := thresholdInner(t).Unmarshal(plain)
	require.NoError(t, err)
	require.Empty(t, txs)
}

func TestThresholdGossip2of3SingleKeyInsufficient(t *testing.T) {
	privs := make([][]byte, 3)
	pubs := make([]string, 3)
	for i := 0; i < 3; i++ {
		var p [32]byte
		_, err := rand.Read(p[:])
		require.NoError(t, err)
		privs[i] = append([]byte(nil), p[:]...)
		pub, err := curve25519.X25519(p[:], curve25519.Basepoint)
		require.NoError(t, err)
		pubs[i] = hex.EncodeToString(pub)
	}
	s0, err := NewThresholdGossipSerializer(thresholdInner(t), ThresholdGossipConfig{
		MinShares: 2, NodePrivateKeyHex: hex.EncodeToString(privs[0]), CommitteePublicHex: pubs,
	})
	require.NoError(t, err)
	out := s0.Marshal([]*Transaction{})
	txs, err := s0.Unmarshal(out)
	require.NoError(t, err)
	require.Empty(t, txs)
}
