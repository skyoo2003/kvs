// Package cuckoofilter implements the Cuckoo-filter
package cuckoofilter

import (
	"errors"
	"math/rand"
	"time"

	"github.com/cespare/xxhash/v2"
)

var (
	// ErrReallocationFails reallocation fails error
	ErrReallocationFails = errors.New("reallocation fails")
	// ErrNoData no data error
	ErrNoData = errors.New("no data")
)

const (
	// DefaultTables default tables size
	DefaultTables = 1 << 20
	// MaxReallocCount maximum count of reallocation
	MaxReallocCount = 1024
)

// nolint:gochecknoinits
func init() {
	rand.Seed(time.Now().UnixNano())
}

// Filter implements Cuckoo-filter
type Filter struct {
	tables    []*Table
	tableSize int
	size      int
}

// NewFilter creates an instance with default tables size
func NewFilter() *Filter {
	return NewFilterWithTableSize(DefaultTables)
}

// NewFilterWithTableSize creates an instance with arbitrary tables size
func NewFilterWithTableSize(tableSize int) *Filter {
	tables := make([]*Table, tableSize)
	for i := 0; i < tableSize; i++ {
		tables[i] = NewTable()
	}
	return &Filter{
		tables:    tables,
		tableSize: tableSize,
		size:      0,
	}
}

// Size get number of entries in all tables
func (f *Filter) Size() int {
	return f.size
}

// Insert insert a data into the tables
func (f *Filter) Insert(data []byte) error {
	idx1, fp := f.index(data), obtainsFingerprint(data)
	if err := f.tables[idx1].Insert(fp); err == nil {
		f.size++
		return nil
	}

	idx2 := f.altIndex(idx1, fp)
	if err := f.tables[idx2].Insert(fp); err == nil {
		f.size++
		return nil
	}

	// When tables in idx1 and idx2 are full, we need to relocate the data until it can be allocated to another table.
	// If the reallocation fails, an error is returned.
	idx := randIndices(idx1, idx2)
	for i := 0; i < MaxReallocCount; i++ {
		fp = f.tables[idx].Swap(fp)
		idx = f.altIndex(idx, fp)
		if err := f.tables[idx].Insert(fp); err == nil {
			f.size++
			return nil
		}
	}
	return ErrReallocationFails
}

// Lookup whether the data exists
func (f *Filter) Lookup(data []byte) bool {
	idx1, fp := f.index(data), obtainsFingerprint(data)
	if where := f.tables[idx1].Index(fp); where >= 0 {
		return true
	}

	idx2 := f.altIndex(idx1, fp)
	if where := f.tables[idx2].Index(fp); where >= 0 {
		return true
	}

	return false
}

// Delete delete the data in any tables
func (f *Filter) Delete(data []byte) error {
	idx1, fp := f.index(data), obtainsFingerprint(data)
	if err := f.tables[idx1].Delete(fp); err == nil {
		f.size--
		return nil
	}

	idx2 := f.altIndex(idx1, fp)
	if err := f.tables[idx2].Delete(fp); err == nil {
		f.size--
		return nil
	}

	return ErrNoData
}

func (f *Filter) index(data []byte) int {
	return int(xxhash.Sum64(data) % uint64(f.tableSize))
}

func (f *Filter) altIndex(idx int, fp fingerprint) int {
	return (idx ^ f.index(getBytesByFingerprint(fp))) % f.tableSize
}
