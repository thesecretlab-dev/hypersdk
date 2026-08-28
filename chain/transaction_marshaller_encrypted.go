package chain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	txGossipMagic     = "VTG1"
	txGossipKeyLen    = 32
	txGossipNonceSize = 12
)

var (
	ErrGossipEncryptionKey     = errors.New("invalid tx gossip encryption key")
	ErrGossipPlaintextRejected = errors.New("plaintext tx gossip rejected")
	ErrGossipDecryptFailed     = errors.New("tx gossip decrypt failed")
)

// GossipTxSerializer optionally wraps batched tx gossip in AES-256-GCM.
type GossipTxSerializer struct {
	inner    *BatchedTransactionSerializer
	key      []byte
	required bool
}

type GossipCodec interface {
	Marshal([]*Transaction) []byte
	Unmarshal([]byte) ([]*Transaction, error)
}

func NewConfiguredTxGossipSerializer(
	inner *BatchedTransactionSerializer,
	keyHex string,
	required bool,
	thresh ThresholdGossipConfig,
) (GossipCodec, error) {
	thresholdRequested := len(thresh.CommitteePublicHex) > 0 || strings.TrimSpace(thresh.NodePrivateKeyHex) != ""
	if required || thresholdRequested {
		if thresh.MinShares < 2 {
			thresh.MinShares = 2
		}
		return NewThresholdGossipSerializer(inner, thresh)
	}
	return NewGossipTxSerializer(inner, keyHex, required)
}

func NewGossipTxSerializer(
	inner *BatchedTransactionSerializer,
	keyHex string,
	required bool,
) (*GossipTxSerializer, error) {
	keyHex = strings.TrimSpace(keyHex)
	if required && keyHex == "" {
		return nil, fmt.Errorf("%w: required but empty", ErrGossipEncryptionKey)
	}
	if keyHex == "" {
		return &GossipTxSerializer{inner: inner, required: required}, nil
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(keyHex, "0x"), "0X"))
	if err != nil || len(raw) != txGossipKeyLen {
		return nil, fmt.Errorf("%w: want 32-byte hex", ErrGossipEncryptionKey)
	}
	return &GossipTxSerializer{inner: inner, key: raw, required: required}, nil
}

func (s *GossipTxSerializer) Marshal(txs []*Transaction) []byte {
	plain := s.inner.Marshal(txs)
	if len(s.key) == 0 {
		return plain
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil
	}
	ct := gcm.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, len(txGossipMagic)+len(nonce)+len(ct))
	out = append(out, txGossipMagic...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out
}

func (s *GossipTxSerializer) Unmarshal(b []byte) ([]*Transaction, error) {
	if isEncryptedGossip(b) {
		if len(s.key) == 0 {
			return nil, ErrGossipDecryptFailed
		}
		plain, err := openGossipEnvelope(s.key, b)
		if err != nil {
			return nil, err
		}
		return s.inner.Unmarshal(plain)
	}
	if s.required {
		return nil, ErrGossipPlaintextRejected
	}
	return s.inner.Unmarshal(b)
}

func isEncryptedGossip(b []byte) bool {
	return len(b) > len(txGossipMagic) && string(b[:len(txGossipMagic)]) == txGossipMagic
}

func openGossipEnvelope(key, env []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	body := env[len(txGossipMagic):]
	if len(body) < gcm.NonceSize() {
		return nil, ErrGossipDecryptFailed
	}
	nonce, ct := body[:gcm.NonceSize()], body[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrGossipDecryptFailed
	}
	return plain, nil
}
