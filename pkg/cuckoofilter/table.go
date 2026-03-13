package cuckoofilter

import (
	"errors"
	"math/rand"
)

var (
	// ErrTableIsFull an error when it is full
	ErrTableIsFull = errors.New("table is full")
	// ErrTableNoEntry an error when it is no entry
	ErrTableNoEntry = errors.New("table no entry")
)

const (
	// DefaultBuckets default buckets size
	DefaultBuckets = 4
)

// Table implement Cuckoo-filter's hash table
type Table struct {
	buckets    []fingerprint
	bucketSize int
	size       int
}

// NewTable creates an instance with default buckets size
func NewTable() *Table {
	return NewTableWithBucketSize(DefaultBuckets)
}

// NewTableWithBucketSize creates an instance with arbitrary buckets size
func NewTableWithBucketSize(bucketSize int) *Table {
	return &Table{
		buckets:    make([]fingerprint, bucketSize),
		bucketSize: bucketSize,
		size:       0,
	}
}

// Insert insert a fingerprint into the buckets
func (t *Table) Insert(fp fingerprint) error {
	if t.size < t.bucketSize {
		for i, bucket := range t.buckets {
			if bucket == 0 {
				t.buckets[i] = fp
				t.size++
				return nil
			}
		}
	}
	return ErrTableIsFull
}

// Delete delete an entry in the buckets by a fingerprint
func (t *Table) Delete(fp fingerprint) error {
	for i, v := range t.buckets {
		if v == fp {
			t.buckets[i] = 0
			t.size--
			return nil
		}
	}
	return ErrTableNoEntry
}

// Index get an index of a fingerprint in the buckets
func (t *Table) Index(fp fingerprint) int {
	for i, v := range t.buckets {
		if v == fp {
			return i
		}
	}
	return -1
}

// Reset clear all entries in the buckets
func (t *Table) Reset() error {
	t.buckets = make([]fingerprint, t.bucketSize)
	t.size = 0
	return nil
}

// Swap swap two entries at random and returns an original one.
// nolint:golint
func (t *Table) Swap(fp fingerprint) fingerprint {
	// nolint:gosec
	idx := rand.Intn(t.bucketSize)
	ofp := t.buckets[idx]
	t.buckets[idx] = fp
	return ofp
}
