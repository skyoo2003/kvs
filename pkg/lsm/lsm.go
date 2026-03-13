// Package lsm implements a small in-memory LSM tree.
package lsm

import (
	"errors"
	"sort"
)

var (
	// ErrKeyNotFound is returned when a key does not exist in the tree.
	ErrKeyNotFound = errors.New("key not found")
)

const (
	// DefaultMemtableLimit is the number of pending keys before an automatic flush.
	DefaultMemtableLimit = 4
)

// Tree is a small in-memory LSM tree.
type Tree struct {
	memtable      map[string]entry
	segments      []segment
	memtableLimit int
}

type entry struct {
	key     string
	value   interface{}
	deleted bool
}

type segment struct {
	entries []entry
}

// New creates a Tree with the default memtable limit.
func New() *Tree {
	return NewWithMemtableLimit(DefaultMemtableLimit)
}

// NewWithMemtableLimit creates a Tree with a custom memtable limit.
func NewWithMemtableLimit(limit int) *Tree {
	return &Tree{
		memtable:      make(map[string]entry),
		segments:      nil,
		memtableLimit: normalizeLimit(limit),
	}
}

// Put stores a value for key in the tree.
func (t *Tree) Put(key string, value interface{}) error {
	if t == nil {
		return ErrKeyNotFound
	}

	t.ensureMemtable()
	t.memtable[key] = entry{key: key, value: value}
	return t.flushIfNeeded()
}

// Get returns the current value for key.
func (t *Tree) Get(key string) (interface{}, error) {
	current, ok := t.lookup(key)
	if !ok || current.deleted {
		return nil, ErrKeyNotFound
	}

	return current.value, nil
}

// Delete removes a key from the tree.
func (t *Tree) Delete(key string) error {
	current, ok := t.lookup(key)
	if !ok || current.deleted {
		return ErrKeyNotFound
	}

	t.ensureMemtable()
	t.memtable[key] = entry{key: key, value: current.value, deleted: true}
	return t.flushIfNeeded()
}

// Flush moves the current memtable into an immutable segment.
func (t *Tree) Flush() error {
	if t == nil || len(t.memtable) == 0 {
		return nil
	}

	entries := make([]entry, 0, len(t.memtable))
	for _, current := range t.memtable {
		entries = append(entries, current)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	t.segments = append([]segment{{entries: entries}}, t.segments...)
	t.memtable = make(map[string]entry)
	return nil
}

func (t *Tree) ensureMemtable() {
	if t.memtable == nil {
		t.memtable = make(map[string]entry)
	}
	if t.memtableLimit < 1 {
		t.memtableLimit = DefaultMemtableLimit
	}
}

func (t *Tree) flushIfNeeded() error {
	if len(t.memtable) < t.memtableLimit {
		return nil
	}

	return t.Flush()
}

func (t *Tree) lookup(key string) (entry, bool) {
	if t == nil {
		return entry{}, false
	}

	if current, ok := t.memtable[key]; ok {
		return current, true
	}

	for _, current := range t.segments {
		if found, ok := current.get(key); ok {
			return found, true
		}
	}

	return entry{}, false
}

func (s segment) get(key string) (entry, bool) {
	idx := sort.Search(len(s.entries), func(i int) bool {
		return s.entries[i].key >= key
	})
	if idx >= len(s.entries) || s.entries[idx].key != key {
		return entry{}, false
	}

	return s.entries[idx], true
}

func normalizeLimit(limit int) int {
	if limit < 1 {
		return DefaultMemtableLimit
	}

	return limit
}
