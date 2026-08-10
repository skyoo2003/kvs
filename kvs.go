// Package kvs implements a key-value store. NewStore keeps the keyspace in memory, which is all a
// caller who wanted a cache has to think about; Open keeps an append log beside it, so the
// keyspace survives a restart.
//
// Running one as a member of a cluster lives in internal/cluster, so that a consensus library is
// not something this package hands to everyone importing it.
package kvs

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skyoo2003/kvs/internal/datadir"
)

var (
	// ErrKeyNotFound is returned when the requested key does not exist.
	ErrKeyNotFound = errors.New("key not found")

	// ErrNoCodec is what the paths that have to render a value get back when no Codec has been
	// set. A Store from NewStore has none until SetCodec, which is enough for a caller who only
	// reads and writes through it, and not enough for one asking it to snapshot or speculate:
	// those have to turn values into bytes.
	ErrNoCodec = errors.New("no codec set")
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

// Store is a key-value store, holding the keyspace in memory unless Open gave it a log to
// write to. The zero value is ready to use and every method is safe for concurrent use.
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
	// codec renders stored values for the log and for replicas. Only a Store that persists or
	// replicates needs one.
	codec Codec
	// replicate, when set, is where a write goes instead of straight into the keyspace. A
	// clustered node points it at consensus, so the HTTP, gRPC, and RESP servers keep calling
	// Write and never have to know the difference.
	replicate func(fn func(tx *Tx) error) error
	// lg is the append log the keyspace survives a restart through. A Store from NewStore
	// has none and keeps everything in memory, which is what a caller who only wants a cache
	// gets by default.
	lg *appendLog
}

// NewStore creates a new Store instance. It keeps everything in memory; Open builds one that
// outlives the process.
func NewStore() *Store {
	return &Store{
		data:    make(map[string]Entry),
		expires: make(map[string]struct{}),
	}
}

// Open creates a Store backed by an append log inside dir, replaying whatever a previous run
// left there. Values are persisted through codec, and a nil codec means StringCodec.
//
// Close releases the log. A Store that is never closed leaves its last writes flushed but the
// file handle open.
func Open(dir string, codec Codec) (*Store, error) {
	if codec == nil {
		codec = StringCodec{}
	}

	// Before anything is read: a directory written by a version whose format this build does
	// not know has to stop here, where the reason can still be explained, rather than during
	// the replay where it would look like corruption.
	if err := datadir.Ensure(dir); err != nil {
		return nil, err
	}

	lg, err := openLog(dir)
	if err != nil {
		return nil, err
	}

	// The log is attached to the store only after the replay and the rewrite below, so
	// neither of them records what it is in the middle of reading back.
	store := NewStore()
	store.codec = codec
	if err := store.load(lg); err != nil {
		_ = lg.close()

		return nil, err
	}

	store.lg = lg

	return store, nil
}

// load replays the log into the store and then rewrites it from what survived, which drops
// overwritten keys, deleted keys, expired keys, and any half-written tail in one pass.
func (s *Store) load(lg *appendLog) error {
	s.mu.Lock()
	err := s.writeLocked(func(tx *Tx) error {
		return lg.replay(tx.restore)
	})
	s.mu.Unlock()

	if err != nil {
		return err
	}

	live, err := s.snapshot()
	if err != nil {
		return err
	}

	return lg.rewrite(live)
}

// Close releases the append log. A Store from NewStore has none, so closing it does nothing,
// and closing twice is safe.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lg == nil {
		return nil
	}

	lg := s.lg
	s.lg = nil

	return lg.close()
}

// snapshot is the live keyspace as a frame, which is what the log is rewritten from and what a
// replica is handed before the stream starts.
func (s *Store) snapshot() (frame, error) {
	var lines frame

	err := s.Read(func(_ *ReadTx) error {
		var snapErr error
		lines, snapErr = s.snapshotLocked()

		return snapErr
	})

	return lines, err
}

// snapshotLocked builds the snapshot without taking the lock, for the callers that already
// hold it. Follow is one, and it has to be: a snapshot taken outside the lock it registers
// under would either miss a write or repeat one.
func (s *Store) snapshotLocked() (frame, error) {
	now := time.Now()
	live := make([]record, 0, len(s.data))

	for key, entry := range s.data {
		if !entry.ExpiresAt.IsZero() && !entry.ExpiresAt.After(now) {
			continue
		}

		live = append(live, record{Op: opSet, Key: key, value: entry.Value, ExpiresAt: entry.ExpiresAt})
	}

	return s.encodeFrame(live)
}

// encodeFrame renders what a transaction changed as the lines the log stores and replicas
// receive. Encoding here rather than at each Set is what makes a value the codec cannot handle
// fail the write that stored it.
func (s *Store) encodeFrame(pending []record) (frame, error) {
	// The one place a stored value becomes bytes, and so the one place worth checking. Nothing to
	// encode needs no codec, which is why an empty keyspace snapshots without one and a keyspace
	// holding a single key does not.
	if len(pending) > 0 && s.codec == nil {
		return nil, ErrNoCodec
	}

	lines := make(frame, 0, len(pending))

	for i := range pending {
		if pending[i].Op == opSet {
			value, err := s.codec.Encode(pending[i].value)
			if err != nil {
				return nil, fmt.Errorf("encode %q: %w", pending[i].Key, err)
			}
			pending[i].Value = value
		}

		line, err := encodeRecord(&pending[i])
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}

	return lines, nil
}

// recording reports whether anything consumes the records a transaction produces. Without a log
// there is nobody to tell, so a transaction skips the bookkeeping; a speculative one records
// regardless, because the frame is the only thing it is run for.
func (s *Store) recording() bool {
	return s.lg != nil
}

// commit hands what the transaction changed to the log, which is the promise a returning write
// makes. In a cluster the log is off and consensus has already agreed on the frame before this
// runs.
//
// It runs even when the transaction's function failed, because a change made before that
// failure is already in memory and the log has to say the same thing memory does.
//
// ponytail: a failed append leaves memory ahead of the log rather than rolling back. The error
// does reach the caller, so the divergence is loud rather than silent; refusing further writes
// until the log recovers, the way Redis does, is the upgrade path.
func (s *Store) commit(pending []record) error {
	if len(pending) == 0 {
		return nil
	}

	lines, err := s.encodeFrame(pending)
	if err != nil {
		return err
	}

	if s.lg != nil {
		if err := s.lg.append(lines); err != nil {
			return err
		}
	}

	return nil
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
// A clustered node sends the write through consensus from here, so this one door covers the
// HTTP, gRPC, and RESP servers at once.
func (s *Store) Write(fn func(tx *Tx) error) error {
	if replicate := s.replicator(); replicate != nil {
		// No lock: the replicator takes it itself, once to work out what fn would change and
		// again to apply what the cluster agreed to.
		return replicate(fn)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeLocked(fn)
}

// Speculate runs fn the way Write would, then puts the keyspace back and returns only what fn
// would have changed, encoded.
//
// It exists because consensus needs the two halves in the opposite order from the way a
// transaction produces them: every node has to agree on the effect before any node applies it,
// but the effect is only known by running the closure. Speculate is that first half, and
// ApplyReplicated is the second.
//
// ponytail: the store's write lock is held for the whole speculative pass, and the caller will
// hold it again to apply. Serializing every write around a consensus round is the simplest
// thing that is correct; pipelining, or applying against a view rather than the live keyspace,
// is the upgrade path if the throughput ever matters more.
func (s *Store) Speculate(fn func(tx *Tx) error) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Checked before fn runs rather than inside the Get it would fail in: a speculating Get hands
	// back a clone, the codec is what clones, and Get has no error to return. One check at the
	// door means the ones behind it cannot be reached without a codec.
	if s.codec == nil {
		return nil, ErrNoCodec
	}

	tx := &Tx{ReadTx: ReadTx{store: s, now: time.Now()}, speculative: true}

	err := fn(tx)
	if err != nil {
		tx.rollback()

		return nil, err
	}

	lines, encodeErr := s.encodeFrame(tx.pending)
	tx.rollback()

	if encodeErr != nil {
		return nil, encodeErr
	}

	return lines, nil
}

// Snapshot is the whole live keyspace, encoded the same way the log and the replication stream
// encode it.
func (s *Store) Snapshot() ([][]byte, error) {
	return s.snapshot()
}

// writeLocked is Write without the lock or the read-only check, for the paths that already
// hold the lock and are the reason the keyspace may change at all: startup replay and the
// stream from a leader.
func (s *Store) writeLocked(fn func(tx *Tx) error) error {
	tx := &Tx{ReadTx: ReadTx{store: s, now: time.Now()}}
	err := fn(tx)
	tx.reapExpired()

	if commitErr := s.commit(tx.pending); commitErr != nil && err == nil {
		err = commitErr
	}

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

// Len counts the live keys. Only a key carrying an expiry can be dead, so the count walks the
// expiry index and subtracts, rather than walking a keyspace that is usually far larger.
func (tx *ReadTx) Len() int {
	return len(tx.store.data) - tx.countExpiring(true)
}

// Expiring counts the live keys that carry an expiry.
func (tx *ReadTx) Expiring() int {
	return tx.countExpiring(false)
}

// countExpiring counts the keys in the expiry index that are past their expiry when dead is set,
// and those that are not when it is clear. It does not assume the index is a subset of the
// keyspace, so a key reclaimed without its index entry cannot make the count drift.
func (tx *ReadTx) countExpiring(dead bool) int {
	count := 0
	for key := range tx.store.expires {
		if entry, ok := tx.store.data[key]; ok && tx.expired(entry) == dead {
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
	// pending collects what the transaction changed, for the log to take at commit. It stays
	// empty on a store with no log, so an in-memory store pays nothing for it.
	pending []record
	// speculative marks a transaction that is being run only to find out what it would change.
	// It is put back afterwards, so it hands out copies rather than the stored values and it
	// tells no watcher anything.
	speculative bool
	// undoLog is what rollback puts back, and is filled only while speculating.
	undoLog []undo
}

// undo is what one key held before the transaction touched it.
type undo struct {
	key     string
	entry   Entry
	existed bool
}

// captureUndo remembers the current state of key so rollback can restore it.
func (tx *Tx) captureUndo(key string) {
	if !tx.speculative {
		return
	}

	entry, existed := tx.store.data[key]
	tx.undoLog = append(tx.undoLog, undo{key: key, entry: entry, existed: existed})
}

// rollback puts the keyspace back the way it was, newest change first.
//
// It only has to restore map entries. What a container held is safe already: a speculative
// transaction never hands out the stored value, only a copy, so nothing it did reached the
// originals.
func (tx *Tx) rollback() {
	for i := len(tx.undoLog) - 1; i >= 0; i-- {
		step := tx.undoLog[i]

		if !step.existed {
			delete(tx.store.data, step.key)
			delete(tx.store.expires, step.key)

			continue
		}

		tx.store.data[step.key] = step.entry
		if step.entry.ExpiresAt.IsZero() {
			delete(tx.store.expires, step.key)
		} else {
			tx.store.expires[step.key] = struct{}{}
		}
	}

	// Dropping the cached order is always safe; rebuilding it costs one sort.
	tx.store.sorted = nil
	tx.undoLog = nil
}

// signalChange marks a key's watchers unless the transaction is speculative. Telling a watcher
// about a change that is about to be undone would abort an unrelated client's transaction over
// something that never happened; the apply that follows signals for real.
func (tx *Tx) signalChange(key string) {
	if tx.speculative {
		return
	}

	tx.store.signalChange(key)
}

// Get returns the entry stored under key, removing it first if it is past its expiry.
func (tx *Tx) Get(key string) (Entry, bool) {
	entry, ok := tx.store.data[key]
	if !ok {
		return Entry{}, false
	}
	if tx.expired(entry) {
		tx.discard(key)

		return Entry{}, false
	}

	if tx.speculative {
		// The caller is free to change a container in place, so while speculating it may only
		// ever be given one it is free to ruin.
		entry.Value = tx.store.codec.Clone(entry.Value)
	}

	return entry, true
}

// Set stores entry under key, replacing whatever was there.
func (tx *Tx) Set(key string, entry Entry) {
	if tx.store.data == nil {
		tx.store.data = make(map[string]Entry)
	}

	tx.captureUndo(key)

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

	tx.signalChange(key)
	tx.record(&record{Op: opSet, Key: key, value: entry.Value, ExpiresAt: entry.ExpiresAt})
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

	if tx.speculative {
		for key := range tx.store.data {
			tx.captureUndo(key)
		}
	}

	clear(tx.store.data)
	clear(tx.store.expires)
	tx.store.sorted = nil
	if !tx.speculative {
		tx.store.signalFlush()
	}
	tx.record(&record{Op: opFlush})
}

// remove deletes key as a change callers can see, so it marks the key's watchers.
func (tx *Tx) remove(key string) {
	tx.discard(key)
	tx.signalChange(key)
	tx.record(&record{Op: opDel, Key: key})
}

// record queues a change for the log. It sits beside every signalChange call and nowhere else,
// because the two answer the same question: is this something a caller can see, or is it the
// store tidying up after itself? Reclaiming an expired key is the latter, and needs no record
// of its own — the expiry that condemned it is already in the entry the log holds.
func (tx *Tx) record(rec *record) {
	if !tx.speculative && !tx.store.recording() {
		return
	}

	tx.pending = append(tx.pending, *rec)
}

// restore applies one replayed record. A key already past its expiry is dropped rather than
// restored: it would not be visible anyway, and dropping it here keeps it out of the rewrite
// that follows.
func (tx *Tx) restore(rec *record) error {
	switch rec.Op {
	case opFlush:
		tx.Flush()
	case opDel:
		tx.Delete(rec.Key)
	case opSet:
		if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(tx.now) {
			return nil
		}

		value, err := tx.store.codec.Decode(rec.Value)
		if err != nil {
			return fmt.Errorf("decode %q: %w", rec.Key, err)
		}
		tx.Set(rec.Key, Entry{Value: value, ExpiresAt: rec.ExpiresAt})
	default:
		// Refusing to start beats starting with part of the keyspace missing.
		return fmt.Errorf("unknown log operation %q", rec.Op)
	}

	return nil
}

// discard reclaims key and keeps the expiry index in step, without marking its watchers.
// Reclaiming a key that has already expired is bookkeeping rather than a change: a watch
// survives its key reaching an expiry it was armed against, and otherwise the sweep's random
// sample would decide whether an unrelated caller's optimistic commit went through.
func (tx *Tx) discard(key string) {
	tx.captureUndo(key)
	delete(tx.store.data, key)
	delete(tx.store.expires, key)
	tx.store.sorted = nil
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
			tx.discard(key)
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
