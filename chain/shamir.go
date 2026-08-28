package chain

import (
	"crypto/rand"
	"errors"
)

// GF(256) Shamir split/combine for 32-byte secrets. x coordinates are 1..n.
var (
	ErrShamirParams = errors.New("invalid shamir parameters")
	ErrShamirShare  = errors.New("invalid shamir share")
)

type shamirShare struct {
	X byte
	Y []byte // 32 bytes
}

func shamirSplit(secret []byte, t, n int) ([]shamirShare, error) {
	if t < 1 || n < t || n > 255 || len(secret) == 0 {
		return nil, ErrShamirParams
	}
	shares := make([]shamirShare, n)
	for i := 0; i < n; i++ {
		shares[i] = shamirShare{X: byte(i + 1), Y: make([]byte, len(secret))}
	}
	for i, sb := range secret {
		coeffs := make([]byte, t)
		coeffs[0] = sb
		if t > 1 {
			if _, err := rand.Read(coeffs[1:]); err != nil {
				return nil, err
			}
		}
		for j := 0; j < n; j++ {
			shares[j].Y[i] = evalPoly(coeffs, byte(j+1))
		}
	}
	return shares, nil
}

func shamirCombine(shares []shamirShare, t int) ([]byte, error) {
	if t < 1 || len(shares) < t {
		return nil, ErrShamirShare
	}
	shares = shares[:t]
	secLen := len(shares[0].Y)
	if secLen == 0 {
		return nil, ErrShamirShare
	}
	for _, s := range shares {
		if s.X == 0 || len(s.Y) != secLen {
			return nil, ErrShamirShare
		}
	}
	out := make([]byte, secLen)
	for i := 0; i < secLen; i++ {
		var acc byte
		for j, sj := range shares {
			num, den := byte(1), byte(1)
			for m, sm := range shares {
				if m == j {
					continue
				}
				num = gfMul(num, sm.X)
				den = gfMul(den, gfAdd(sm.X, sj.X))
			}
			acc = gfAdd(acc, gfMul(sj.Y[i], gfMul(num, gfInv(den))))
		}
		out[i] = acc
	}
	return out, nil
}

func evalPoly(coeffs []byte, x byte) byte {
	var y byte
	for i := len(coeffs) - 1; i >= 0; i-- {
		y = gfAdd(gfMul(y, x), coeffs[i])
	}
	return y
}

func gfAdd(a, b byte) byte { return a ^ b }

func gfMul(a, b byte) byte {
	var p byte
	for i := 0; i < 8; i++ {
		if b&1 != 0 {
			p ^= a
		}
		hi := a & 0x80
		a <<= 1
		if hi != 0 {
			a ^= 0x1b
		}
		b >>= 1
	}
	return p
}

func gfInv(a byte) byte {
	if a == 0 {
		return 0
	}
	return gfPow(a, 254)
}

func gfPow(a byte, e int) byte {
	r := byte(1)
	x := a
	for e > 0 {
		if e&1 != 0 {
			r = gfMul(r, x)
		}
		x = gfMul(x, x)
		e >>= 1
	}
	return r
}
