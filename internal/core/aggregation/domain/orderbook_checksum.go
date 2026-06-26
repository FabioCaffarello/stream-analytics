package domain

import (
	"encoding/binary"
	"hash/crc32"
	"math"
)

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

// Checksum computes a deterministic CRC32C of the order book state.
//
// Encoding:
//  1. bids (desc): for each level => price(float64 LE) + qty(float64 LE)
//  2. separator byte 0xFF
//  3. asks (asc):  for each level => price(float64 LE) + qty(float64 LE)
func (b *OrderBook) Checksum() uint32 {
	if b == nil {
		return computeOrderBookChecksum(nil, nil)
	}
	return computeOrderBookChecksum(b.Bids(), b.Asks())
}

// ComputeOrderBookChecksum computes the deterministic checksum from canonical
// level slices. Bids must be pre-sorted desc, asks pre-sorted asc.
func ComputeOrderBookChecksum(bids, asks []Level) uint32 {
	return computeOrderBookChecksum(bids, asks)
}

func computeOrderBookChecksum(bids, asks []Level) uint32 {
	checksum := uint32(0)
	var enc [8]byte

	for i := range bids {
		binary.LittleEndian.PutUint64(enc[:], math.Float64bits(float64(bids[i].Price)))
		checksum = crc32.Update(checksum, castagnoliTable, enc[:])
		binary.LittleEndian.PutUint64(enc[:], math.Float64bits(float64(bids[i].Quantity)))
		checksum = crc32.Update(checksum, castagnoliTable, enc[:])
	}

	checksum = crc32.Update(checksum, castagnoliTable, []byte{0xFF})

	for i := range asks {
		binary.LittleEndian.PutUint64(enc[:], math.Float64bits(float64(asks[i].Price)))
		checksum = crc32.Update(checksum, castagnoliTable, enc[:])
		binary.LittleEndian.PutUint64(enc[:], math.Float64bits(float64(asks[i].Quantity)))
		checksum = crc32.Update(checksum, castagnoliTable, enc[:])
	}

	return checksum
}
