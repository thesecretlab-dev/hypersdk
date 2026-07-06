// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain_test

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/chain/chaintest"
)

// hardcodedBlockHexCanotoV015 is the golden vector TestParseHardcodedBlock
// used while hypersdk pinned canoto v0.15. It is retained here as the "before"
// side of the wire-format migration attestation below.
const hardcodedBlockHexCanotoV015 = "0a20e902a9a86640bfdb1cd0e36c0cc982b83e5765fad5f6bbe6abdcce7b5ae7d7c7117b000000000000001901000000000000002abd010a2508d00f12203d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a712360000000000000000010000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff1a5c00000000000000000101020300000000000000000000000000000000000000000000000000000000000001020300000000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff2ac6010a2e08d00f12203d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a719010000000000000012360000000000000000010000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff1a5c00000000000000000101020300000000000000000000000000000000000000000000000000000000000001020300000000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff2ac6010a2e08d00f12203d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a719020000000000000012360000000000000000010000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff1a5c00000000000000000101020300000000000000000000000000000000000000000000000000000000000001020300000000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff32204a177205df5c29929d06db9d941f83d5ea985de302015e99252d16469a6610db"

// hardcodedBlockHexCanotoV018 is the regenerated golden vector for the same
// canonical block under canoto v0.18 (required by avalanchego v1.14.2). canoto
// v0.18 changed the wire encoding of `repeated pointer` (repeated message)
// fields: each element now carries an additional length-delimited wrapper
// (inner field 1) inside the outer tag. For StatelessBlock the only such field
// is Txs (field 5).
//
// TestWireFormatMigrationCanotoV015ToV018 proves the two vectors encode the
// same semantic block and that the byte-level delta is exclusively the
// documented nesting change. Do not update either constant without an
// equivalent attestation.
const hardcodedBlockHexCanotoV018 = "0a20e902a9a86640bfdb1cd0e36c0cc982b83e5765fad5f6bbe6abdcce7b5ae7d7c7117b000000000000001901000000000000002ac0010abd010a2508d00f12203d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a712360000000000000000010000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff1a5c00000000000000000101020300000000000000000000000000000000000000000000000000000000000001020300000000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff2ac9010ac6010a2e08d00f12203d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a719010000000000000012360000000000000000010000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff1a5c00000000000000000101020300000000000000000000000000000000000000000000000000000000000001020300000000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff2ac9010ac6010a2e08d00f12203d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a719020000000000000012360000000000000000010000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff1a5c00000000000000000101020300000000000000000000000000000000000000000000000000000000000001020300000000000000000000000000000000000000000000000000000000000000ffffffffffffffffffffffffffffffff32204a177205df5c29929d06db9d941f83d5ea985de302015e99252d16469a6610db"

// wireField is one top-level field of a canoto/protobuf-compatible message.
type wireField struct {
	num      uint64
	wireType byte
	payload  []byte // for wireType 2: the length-delimited value, excluding tag+length prefix
	raw      []byte // full field bytes: tag, length prefix (if any), payload
}

func readUvarint(r *require.Assertions, b []byte, at int) (uint64, int) {
	v, n := binary.Uvarint(b[at:])
	r.Positive(n, "invalid varint at offset %d", at)
	return v, at + n
}

// walkWire splits a message into its top-level fields without any knowledge of
// the schema, so it applies identically to the v0.15 and v0.18 encodings.
func walkWire(r *require.Assertions, b []byte) []wireField {
	fields := []wireField{}
	for i := 0; i < len(b); {
		start := i
		tag, next := readUvarint(r, b, i)
		i = next
		f := wireField{num: tag >> 3, wireType: byte(tag & 0x7)}
		switch f.wireType {
		case 0: // varint
			_, i = readUvarint(r, b, i)
		case 1: // fixed 8 bytes
			i += 8
		case 2: // length-delimited
			length, next := readUvarint(r, b, i)
			i = next
			r.LessOrEqual(i+int(length), len(b), "field %d overruns buffer", f.num)
			f.payload = b[i : i+int(length)]
			i += int(length)
		case 5: // fixed 4 bytes
			i += 4
		default:
			r.FailNowf("unsupported wire type", "wire type %d at offset %d", f.wireType, start)
		}
		f.raw = b[start:i]
		fields = append(fields, f)
	}
	return fields
}

// TestWireFormatMigrationCanotoV015ToV018 attests the regeneration of the
// TestParseHardcodedBlock golden vector across the canoto v0.15 -> v0.18 bump
// forced by avalanchego v1.14.2. It proves the new golden bytes encode the
// same block as the old golden bytes, and that the byte-level delta is
// exclusively canoto v0.18's added length-delimited wrapper around each
// element of a repeated message field:
//
//  1. Structural totality: rewriting the old vector by wrapping each Txs
//     (field 5) element as 0x0a || varint(len) || element — and changing
//     nothing else — reproduces the new vector byte-for-byte.
//  2. Semantic identity: the non-repeated fields decoded from the old vector
//     (parent, timestamp, height, state root, absent block context) equal the
//     fields of the block parsed from the new vector, and each old Txs
//     element parses as a Transaction equal to the corresponding transaction
//     of the parsed new block. The inner transaction bytes are unchanged
//     between the two encodings (Transaction contains no repeated message
//     fields), so transaction IDs are preserved; only block bytes — and hence
//     block IDs — differ.
func TestWireFormatMigrationCanotoV015ToV018(t *testing.T) {
	r := require.New(t)

	oldBytes, err := hex.DecodeString(hardcodedBlockHexCanotoV015)
	r.NoError(err)
	newBytes, err := hex.DecodeString(hardcodedBlockHexCanotoV018)
	r.NoError(err)

	// 1. Structural totality: old -> new differs only by the documented
	// wrapper on field 5 elements.
	const txsFieldNum = 5
	rewritten := []byte{}
	oldFields := walkWire(r, oldBytes)
	oldTxElements := [][]byte{}
	for _, f := range oldFields {
		if f.num != txsFieldNum {
			rewritten = append(rewritten, f.raw...)
			continue
		}
		r.EqualValues(2, f.wireType)
		oldTxElements = append(oldTxElements, f.payload)

		wrapped := binary.AppendUvarint([]byte{0x0a}, uint64(len(f.payload)))
		wrapped = append(wrapped, f.payload...)
		element := binary.AppendUvarint([]byte{txsFieldNum<<3 | 2}, uint64(len(wrapped)))
		element = append(element, wrapped...)
		rewritten = append(rewritten, element...)
	}
	r.Equal(newBytes, rewritten,
		"new golden must equal old golden with exactly the repeated-message wrapper added")

	// 2. Semantic identity.
	testParser := chaintest.NewTestParser()
	newBlock, err := chain.UnmarshalBlock(newBytes, testParser)
	r.NoError(err)

	sawBlockContext := false
	txIndex := 0
	for _, f := range oldFields {
		switch f.num {
		case 1: // Prnt `fixed bytes,1`
			r.Equal(newBlock.Prnt[:], f.payload)
		case 2: // Tmstmp `fint64,2`
			r.Len(f.raw, 9)
			r.Equal(uint64(newBlock.Tmstmp), binary.LittleEndian.Uint64(f.raw[1:]))
		case 3: // Hght `fint64,3`
			r.Len(f.raw, 9)
			r.Equal(newBlock.Hght, binary.LittleEndian.Uint64(f.raw[1:]))
		case 4: // BlockContext `pointer,4`
			sawBlockContext = true
		case txsFieldNum: // Txs `repeated pointer,5`
			oldTx, err := chain.UnmarshalTx(f.payload, testParser)
			r.NoError(err)
			r.Less(txIndex, len(newBlock.Txs))
			r.EqualValues(newBlock.Txs[txIndex], oldTx)
			r.Equal(newBlock.Txs[txIndex].GetID(), oldTx.GetID(),
				"transaction IDs must be preserved across the format bump")
			txIndex++
		case 6: // StateRoot `fixed bytes,6`
			r.Equal(newBlock.StateRoot[:], f.payload)
		default:
			r.FailNowf("unexpected field", "field %d in v0.15 golden", f.num)
		}
	}
	r.Equal(len(newBlock.Txs), txIndex)
	r.Equal(newBlock.BlockContext != nil, sawBlockContext)
}
