package chain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/curve25519"
)

const (
	txGossipV2Magic     = "VTG2"
	txGossipShareMagic  = "VTGS"
	x25519KeyLen        = 32
	wrappedShareLen     = 32 + 16 // plaintext share + GCM tag
	maxPendingGossipEnv = 256
)

var ErrGossipThreshold = errors.New("tx gossip threshold")

type ThresholdGossipConfig struct {
	MinShares          int
	NodePrivateKeyHex  string
	CommitteePublicHex []string
}

type thresholdGossipSerializer struct {
	inner     *BatchedTransactionSerializer
	t         int
	n         int
	priv      []byte
	committee [][]byte

	mu       sync.Mutex
	pending  map[string]*pendingGossip
	outbound [][]byte
}

type pendingGossip struct {
	inner  []byte
	t      int
	n      int
	shares map[byte][]byte
}

func NewThresholdGossipSerializer(
	inner *BatchedTransactionSerializer,
	cfg ThresholdGossipConfig,
) (*thresholdGossipSerializer, error) {
	if cfg.MinShares < 2 {
		return nil, fmt.Errorf("%w: private mempool requires t>=2", ErrGossipThreshold)
	}
	priv, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(cfg.NodePrivateKeyHex), "0x"), "0X"))
	if err != nil || len(priv) != x25519KeyLen {
		return nil, fmt.Errorf("%w: node private key", ErrGossipThreshold)
	}
	pubs := make([][]byte, 0, len(cfg.CommitteePublicHex))
	for _, h := range cfg.CommitteePublicHex {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(h, "0x"), "0X"))
		if err != nil || len(b) != x25519KeyLen {
			return nil, fmt.Errorf("%w: committee public key", ErrGossipThreshold)
		}
		pubs = append(pubs, b)
	}
	if len(pubs) < 2 {
		return nil, fmt.Errorf("%w: committee n>=2 required", ErrGossipThreshold)
	}
	if cfg.MinShares > len(pubs) {
		return nil, fmt.Errorf("%w: t>n", ErrGossipThreshold)
	}
	return &thresholdGossipSerializer{
		inner:     inner,
		t:         cfg.MinShares,
		n:         len(pubs),
		priv:      priv,
		committee: pubs,
		pending:   make(map[string]*pendingGossip),
	}, nil
}

func (s *thresholdGossipSerializer) Marshal(txs []*Transaction) []byte {
	plain := s.inner.Marshal(txs)
	dataKey := make([]byte, txGossipKeyLen)
	if _, err := rand.Read(dataKey); err != nil {
		return nil
	}
	inner, err := sealWithKey(dataKey, plain)
	if err != nil {
		return nil
	}
	shares, err := shamirSplit(dataKey, s.t, s.n)
	if err != nil {
		return nil
	}
	out := make([]byte, 0, 4+2+len(inner)+s.n*(x25519KeyLen+12+wrappedShareLen))
	out = append(out, txGossipV2Magic...)
	out = append(out, byte(s.t), byte(s.n))
	out = append(out, inner...)
	for i, sh := range shares {
		wrapped, eph, err := wrapShare(s.committee[i], sh.Y)
		if err != nil {
			return nil
		}
		out = append(out, eph...)
		out = append(out, wrapped...)
	}
	return out
}

func (s *thresholdGossipSerializer) Unmarshal(b []byte) ([]*Transaction, error) {
	if isShareGossip(b) {
		return s.ingestShare(b)
	}
	if isEncryptedGossip(b) && !isThresholdGossip(b) {
		return nil, fmt.Errorf("%w: VTG1 is not a private mempool", ErrGossipThreshold)
	}
	if !isThresholdGossip(b) {
		return nil, ErrGossipPlaintextRejected
	}
	if len(b) < 6 {
		return nil, ErrGossipThreshold
	}
	t, n := int(b[4]), int(b[5])
	if t < 2 || n < t {
		return nil, ErrGossipThreshold
	}
	rest := b[6:]
	shareBlob := n * (x25519KeyLen + 12 + wrappedShareLen)
	if len(rest) <= shareBlob {
		return nil, ErrGossipThreshold
	}
	inner := rest[:len(rest)-shareBlob]
	shareBytes := rest[len(rest)-shareBlob:]
	envHash := sha256.Sum256(inner)
	id := hex.EncodeToString(envHash[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending[id]
	if p == nil {
		if len(s.pending) >= maxPendingGossipEnv {
			return nil, ErrGossipThreshold
		}
		p = &pendingGossip{inner: append([]byte(nil), inner...), t: t, n: n, shares: make(map[byte][]byte)}
		s.pending[id] = p
	}
	for i := 0; i < n; i++ {
		off := i * (x25519KeyLen + 12 + wrappedShareLen)
		eph := shareBytes[off : off+x25519KeyLen]
		wrapped := shareBytes[off+x25519KeyLen : off+x25519KeyLen+12+wrappedShareLen]
		y, err := unwrapShare(s.priv, eph, wrapped)
		if err != nil {
			continue
		}
		p.shares[byte(i+1)] = y
		if ann, err := s.announceShare(envHash[:], byte(i+1), y); err == nil {
			s.outbound = append(s.outbound, ann)
		}
	}
	return s.tryOpenLocked(id, p)
}

func (s *thresholdGossipSerializer) ingestShare(b []byte) ([]*Transaction, error) {
	if len(b) < 4+32+1 {
		return nil, ErrGossipThreshold
	}
	envHash := b[4:36]
	x := b[36]
	if x == 0 {
		return nil, ErrGossipThreshold
	}
	rest := b[37:]
	slot := x25519KeyLen + 12 + wrappedShareLen
	if len(rest)%slot != 0 || len(rest) == 0 {
		return nil, ErrGossipThreshold
	}
	var y []byte
	for i := 0; i < len(rest)/slot; i++ {
		off := i * slot
		eph := rest[off : off+x25519KeyLen]
		wrapped := rest[off+x25519KeyLen : off+slot]
		got, err := unwrapShare(s.priv, eph, wrapped)
		if err != nil {
			continue
		}
		y = got
		break
	}
	if y == nil {
		return []*Transaction{}, nil
	}
	id := hex.EncodeToString(envHash)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending[id]
	if p == nil {
		if len(s.pending) >= maxPendingGossipEnv {
			return nil, ErrGossipThreshold
		}
		p = &pendingGossip{shares: make(map[byte][]byte)}
		s.pending[id] = p
	}
	p.shares[x] = y
	if p.inner == nil {
		return []*Transaction{}, nil
	}
	return s.tryOpenLocked(id, p)
}

func (s *thresholdGossipSerializer) tryOpenLocked(id string, p *pendingGossip) ([]*Transaction, error) {
	if p == nil || p.inner == nil || p.t < 2 {
		return []*Transaction{}, nil
	}
	if len(p.shares) < p.t {
		return []*Transaction{}, nil
	}
	recovered := make([]shamirShare, 0, p.t)
	for x, y := range p.shares {
		recovered = append(recovered, shamirShare{X: x, Y: y})
		if len(recovered) == p.t {
			break
		}
	}
	dataKey, err := shamirCombine(recovered, p.t)
	if err != nil {
		return nil, err
	}
	plain, err := openWithKey(dataKey, p.inner)
	if err != nil {
		return nil, err
	}
	delete(s.pending, id)
	return s.inner.Unmarshal(plain)
}

func (s *thresholdGossipSerializer) announceShare(envHash []byte, x byte, y []byte) ([]byte, error) {
	out := make([]byte, 0, 4+32+1+len(s.committee)*(x25519KeyLen+12+wrappedShareLen))
	out = append(out, txGossipShareMagic...)
	out = append(out, envHash...)
	out = append(out, x)
	for _, pub := range s.committee {
		wrapped, eph, err := wrapShare(pub, y)
		if err != nil {
			return nil, err
		}
		out = append(out, eph...)
		out = append(out, wrapped...)
	}
	return out, nil
}

// TakeOutbound returns one pending share announcement for validator gossip.
func (s *thresholdGossipSerializer) TakeOutbound() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.outbound) == 0 {
		return nil
	}
	b := s.outbound[0]
	s.outbound = s.outbound[1:]
	return b
}

func isThresholdGossip(b []byte) bool {
	return len(b) > len(txGossipV2Magic) && string(b[:len(txGossipV2Magic)]) == txGossipV2Magic
}

func isShareGossip(b []byte) bool {
	return len(b) > len(txGossipShareMagic) && string(b[:len(txGossipShareMagic)]) == txGossipShareMagic
}

func sealWithKey(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, len(txGossipMagic)+len(nonce)+len(ct))
	out = append(out, txGossipMagic...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func openWithKey(key, env []byte) ([]byte, error) {
	if !isEncryptedGossip(env) {
		return nil, ErrGossipPlaintextRejected
	}
	return openGossipEnvelope(key, env)
}

func wrapShare(peerPub, share []byte) (wrapped, ephPub []byte, err error) {
	var ephPriv [32]byte
	if _, err = rand.Read(ephPriv[:]); err != nil {
		return nil, nil, err
	}
	ephPub, err = curve25519.X25519(ephPriv[:], curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	shared, err := curve25519.X25519(ephPriv[:], peerPub)
	if err != nil {
		return nil, nil, err
	}
	k := sha256.Sum256(shared)
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, 12)
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ct := gcm.Seal(nil, nonce, share, nil)
	wrapped = append(nonce, ct...)
	return wrapped, ephPub, nil
}

func unwrapShare(priv, ephPub, wrapped []byte) ([]byte, error) {
	if len(wrapped) != 12+wrappedShareLen {
		return nil, ErrGossipThreshold
	}
	shared, err := curve25519.X25519(priv, ephPub)
	if err != nil {
		return nil, err
	}
	k := sha256.Sum256(shared)
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, ct := wrapped[:12], wrapped[12:]
	return gcm.Open(nil, nonce, ct, nil)
}

func x25519PubFromPriv(priv []byte) ([]byte, error) {
	return curve25519.X25519(priv, curve25519.Basepoint)
}
