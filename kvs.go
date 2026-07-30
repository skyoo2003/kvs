// Package kvs implements an in-memory key-value store.
package kvs

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrKeyNotFound is returned when the requested key does not exist.
	ErrKeyNotFound = errors.New("key not found")
)

// reapSample bounds how many keys carrying an expiry one write transaction inspects, so the
// cost of reclaiming expired keys stays flat however large the keyspace gets.
const reapSample = 20

// Entry is one stored key. The dynamic type of Value decides what the key holds, so a
// caller that stores several shapes can tell them apart with a type switch. A zero
// ExpiresAt means the key never expires.
type Entry struct {
	Value     interface{}
	ExpiresAt time.Time
}

// Store is an in-memory key-value store. The zero value is ready to use and every method
// is safe for concurrent use.
type Store struct {
	mu   sync.RWMutex
	data map[string]Entry
	// expires holds only the keys that carry an expiry, so reclaiming them samples
	// candidates rather than walking the whole keyspace.
	expires  map[string]struct{}
	watchers map[string]map[*Watch]struct{}
	// sorted caches the keys in order for SortedKeys. Only a change to the key set clears
	// it, so overwriting a key that already exists keeps it valid. Readers build it while
	// holding the read lock, hence a lock of its own; a writer clearing it holds mu
	// exclusively and so cannot race a build.
	sortedMu sync.Mutex
	sorted   []string
}

// NewStore creates a new Store instance.
func NewStore() *Store {
	return &Store{
		data:    make(map[string]Entry),
		expires: make(map[string]struct{}),
	}
}

// Read runs fn while the store is locked for reading. Several readers run at once, so fn
// must not mutate anything it reaches through tx.
func (s *Store) Read(fn func(tx *ReadTx) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fn(&ReadTx{store: s, now: time.Now()})
}

// Write runs fn while the store is locked for writing. Everything fn does through tx
// lands as one atomic step, which is what lets a caller build compound operations such as
// a read-modify-write or a batch of commands without extra locking.
func (s *Store) Write(fn func(tx *Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := &Tx{ReadTx{store: s, now: time.Now()}}
	err := fn(tx)
	tx.reapExpired()

	return err
}

// Watch starts tracking keys for change. Registration happens under the store lock, so no
// change can slip in between a caller's reads and the tracking taking effect, which is what
// makes an optimistic read-check-commit sequence sound.
//
// Close releases it. A Watch that is never closed keeps the store tracking its keys.
func (s *Store) Watch(keys ...string) *Watch {
	s.mu.Lock()
	defer s.mu.Unlock()

	watch := &Watch{store: s, keys: slices.Clone(keys)}
	if s.watchers == nil {
		s.watchers = make(map[string]map[*Watch]struct{})
	}

	for _, key := range watch.keys {
		if s.watchers[key] == nil {
			s.watchers[key] = make(map[*Watch]struct{})
		}
		s.watchers[key][watch] = struct{}{}
	}

	return watch
}

// Get gets a value by key.
func (s *Store) Get(key string) (interface{}, error) {
	var value interface{}

	err := s.Read(func(tx *ReadTx) error {
		entry, ok := tx.Get(key)
		if !ok {
			return ErrKeyNotFound
		}
		value = entry.Value

		return nil
	})
	if err != nil {
		return nil, err
	}

	return value, nil
}

// Put stores a value under key, clearing any expiry the key had.
func (s *Store) Put(key string, value interface{}) error {
	return s.Write(func(tx *Tx) error {
		tx.Set(key, Entry{Value: value})

		return nil
	})
}

// Delete removes a key from the store.
func (s *Store) Delete(key string) error {
	return s.Write(func(tx *Tx) error {
		if !tx.Delete(key) {
			return ErrKeyNotFound
		}

		return nil
	})
}

// ReadTx reads the keyspace while the store is locked. It is only valid for the duration
// of the Read or Write call that produced it.
type ReadTx struct {
	store *Store
	now   time.Time
}

// Now is the instant the transaction started. Every expiry check inside one transaction
// uses it, so a batch of operations sees one consistent view of what has expired.
func (tx *ReadTx) Now() time.Time {
	return tx.now
}

// Get returns the entry stored under key. A key past its expiry reports as missing. A read
// cannot remove it, so its memory is reclaimed by the next write transaction, either because
// that write touches the key or because the write's sampling sweep reaches it.
func (tx *ReadTx) Get(key string) (Entry, bool) {
	entry, ok := tx.store.data[key]
	if !ok || tx.expired(entry) {
		return Entry{}, false
	}

	return entry, true
}

// Keys returns the live keys in an unspecified order.
func (tx *ReadTx) Keys() []string {
	keys := make([]string, 0, len(tx.store.data))
	for key, entry := range tx.store.data {
		if !tx.expired(entry) {
			keys = append(keys, key)
		}
	}

	return keys
}

// SortedKeys returns the keys in lexicographic order. The order is cached until the key set
// changes, so a caller paging through a large keyspace pays for the sort once rather than once
// per page. Two consequences follow from the cache, and both suit a paged walk: the slice is
// shared, so a caller must not modify it, and a key that has expired but not yet been
// reclaimed is still listed, so a caller that cares must check it with Get.
func (tx *ReadTx) SortedKeys() []string {
	tx.store.sortedMu.Lock()
	defer tx.store.sortedMu.Unlock()

	if tx.store.sorted == nil {
		tx.store.sorted = slices.Sorted(maps.Keys(tx.store.data))
	}

	return tx.store.sorted
}

// Len counts the live keys.
func (tx *ReadTx) Len() int {
	count := 0
	for _, entry := range tx.store.data {
		if !tx.expired(entry) {
			count++
		}
	}

	return count
}

func (tx *ReadTx) expired(entry Entry) bool {
	return !entry.ExpiresAt.IsZero() && !entry.ExpiresAt.After(tx.now)
}

// Tx reads and changes the keyspace while the store is locked for writing. It is only
// valid for the duration of the Write call that produced it.
type Tx struct {
	ReadTx
}

// Get returns the entry stored under key, removing it first if it is past its expiry.
func (tx *Tx) Get(key string) (Entry, bool) {
	entry, ok := tx.store.data[key]
	if !ok {
		return Entry{}, false
	}
	if tx.expired(entry) {
		tx.remove(key)

		return Entry{}, false
	}

	return entry, true
}

// Set stores entry under key, replacing whatever was there.
func (tx *Tx) Set(key string, entry Entry) {
	if tx.store.data == nil {
		tx.store.data = make(map[string]Entry)
	}

	if _, existed := tx.store.data[key]; !existed {
		tx.store.sorted = nil
	}

	tx.store.data[key] = entry
	if entry.ExpiresAt.IsZero() {
		delete(tx.store.expires, key)
	} else {
		if tx.store.expires == nil {
			tx.store.expires = make(map[string]struct{})
		}
		tx.store.expires[key] = struct{}{}
	}

	tx.store.signalChange(key)
}

// Delete removes key and reports whether it was there to begin with.
func (tx *Tx) Delete(key string) bool {
	if _, ok := tx.Get(key); !ok {
		return false
	}

	tx.remove(key)

	return true
}

// Flush removes every key.
func (tx *Tx) Flush() {
	if len(tx.store.data) == 0 {
		return
	}

	clear(tx.store.data)
	clear(tx.store.expires)
	tx.store.sorted = nil
	tx.store.signalFlush()
}

// remove deletes key and keeps the expiry index in step.
func (tx *Tx) remove(key string) {
	delete(tx.store.data, key)
	delete(tx.store.expires, key)
	tx.store.sorted = nil
	tx.store.signalChange(key)
}

// reapExpired reclaims expired keys from a bounded sample of those that carry an expiry. Go
// randomizes where a map range starts, so successive writes look at different candidates and
// the set is covered over time.
//
// Sweeping here rather than on a timer keeps the Store free of a goroutine it would have to
// own and shut down, and a keyspace with no expiries pays nothing.
func (tx *Tx) reapExpired() {
	inspected := 0
	for key := range tx.store.expires {
		if inspected == reapSample {
			return
		}
		inspected++

		if entry, ok := tx.store.data[key]; !ok || tx.expired(entry) {
			tx.remove(key)
		}
	}
}

// Watch reports whether any of the keys it tracks has changed since it was created.
type Watch struct {
	store   *Store
	keys    []string
	changed atomic.Bool
}

// Conflicted reports whether a watched key changed. A caller holding the store's write lock
// sees a stable answer, because every change is made under that lock.
func (w *Watch) Conflicted() bool {
	return w.changed.Load()
}

// Close stops tracking. It is safe to call more than once, and it must not be called from
// inside a Read or Write callback, because it takes the store lock itself.
func (w *Watch) Close() {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()

	for _, key := range w.keys {
		watchers := w.store.watchers[key]
		delete(watchers, w)
		if len(watchers) == 0 {
			delete(w.store.watchers, key)
		}
	}

	w.keys = nil
}

func (s *Store) signalChange(key string) {
	if len(s.watchers) == 0 {
		return
	}

	for watch := range s.watchers[key] {
		watch.changed.Store(true)
	}
}

// signalFlush marks every watch, because a flush changes every key.
func (s *Store) signalFlush() {
	for _, watchers := range s.watchers {
		for watch := range watchers {
			watch.changed.Store(true)
		}
	}
}
