// Package bitset implements the Bit-Set Data strcutrue
package bitset

import (
	"math/big"
	"math/bits"
)

// BitSet A data structure of a set of bits
type BitSet struct {
	v big.Int
}

// New creates a new BitSet instance
func New() *BitSet {
	return new(BitSet)
}

// Test whether the bit at i is set
func (bs *BitSet) Test(i uint) bool {
	return bs.v.Bit(int(i)) == 1
}

// Set set the bit at i to be 1
func (bs *BitSet) Set(i uint) *BitSet {
	bs.v.SetBit(&bs.v, int(i), 1)
	return bs
}

// Reset set the bit at i to be 0
func (bs *BitSet) Reset(i uint) *BitSet {
	bs.v.SetBit(&bs.v, int(i), 0)
	return bs
}

// Flip flip the bit at i
func (bs *BitSet) Flip(i uint) *BitSet {
	if !bs.Test(i) {
		return bs.Set(i)
	}
	return bs.Reset(i)
}

// All whether all bits are set
func (bs *BitSet) All() bool {
	return bs.Count() == bs.Size()
}

// Any whether any bits are set
func (bs *BitSet) Any() bool {
	return bs.Count() > 0
}

// Count number of bits of 1
func (bs *BitSet) Count() int {
	return bits.OnesCount64(bs.v.Uint64())
}

// Size length of bits
func (bs *BitSet) Size() int {
	return bs.v.BitLen()
}

// And bitwise AND operation
func (bs *BitSet) And(o *BitSet) *BitSet {
	bs.v.And(&bs.v, &o.v)
	return bs
}

// Or bitwise OR operation
func (bs *BitSet) Or(o *BitSet) *BitSet {
	bs.v.Or(&bs.v, &o.v)
	return bs
}

// Xor bitwise XOR operation
func (bs *BitSet) Xor(o *BitSet) *BitSet {
	bs.v.Xor(&bs.v, &o.v)
	return bs
}

// Not bitwise NOT operation
func (bs *BitSet) Not() *BitSet {
	bs.v.Not(&bs.v)
	return bs
}

// Lshift bitwise LEFT SHIFT operation
func (bs *BitSet) Lshift(n uint) *BitSet {
	bs.v.Lsh(&bs.v, n)
	return bs
}

// Rshift bitwise RIGHT SHIFT operation
func (bs *BitSet) Rshift(n uint) *BitSet {
	bs.v.Rsh(&bs.v, n)
	return bs
}

// String get a string in binary format
func (bs *BitSet) String() string {
	return bs.v.Text(2) // nolint
}

// Uint get a uint data type
func (bs *BitSet) Uint() uint {
	return uint(bs.v.Uint64())
}
