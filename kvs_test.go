package kvs

import (
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestStorePutGetDelete(t *testing.T) {
	store := NewStore()

	if err := store.Put("name", "kvs"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := store.Get("name")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "kvs" {
		t.Fatalf("Get() = %v, want %v", got, "kvs")
	}

	err = store.Delete("name")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = store.Get("name")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestStoreZeroValueIsUsable(t *testing.T) {
	var store Store

	if err := store.Put("answer", 42); err != nil {
		t.Fatalf("Put() on zero-value store error = %v", err)
	}

	got, err := store.Get("answer")
	if err != nil {
		t.Fatalf("Get() on zero-value store error = %v", err)
	}
	if got != 42 {
		t.Fatalf("Get() on zero-value store = %v, want %v", got, 42)
	}
}

func TestStoreMissingKey(t *testing.T) {
	store := NewStore()

	if _, err := store.Get("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() missing key error = %v, want %v", err, ErrKeyNotFound)
	}

	if err := store.Delete("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Delete() missing key error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestStoreHidesExpiredEntries(t *testing.T) {
	store := NewStore()

	if err := store.Write(func(tx *Tx) error {
		tx.Set("gone", Entry{Value: "a", ExpiresAt: time.Now().Add(-time.Second)})
		tx.Set("alive", Entry{Value: "b", ExpiresAt: time.Now().Add(time.Hour)})
		tx.Set("forever", Entry{Value: "c"})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := store.Get("gone"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() expired key error = %v, want %v", err, ErrKeyNotFound)
	}
	for _, key := range []string{"alive", "forever"} {
		if _, err := store.Get(key); err != nil {
			t.Fatalf("Get(%q) error = %v", key, err)
		}
	}

	if err := store.Read(func(tx *ReadTx) error {
		if got := tx.Len(); got != 2 {
			t.Fatalf("Len() = %d, want 2", got)
		}
		if got := len(tx.Keys()); got != 2 {
			t.Fatalf("len(Keys()) = %d, want 2", got)
		}

		return nil
	}); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Deleting an already expired key reports that nothing was there.
	if err := store.Delete("gone"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Delete() expired key error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestStoreWatchTracksOnlyItsOwnKeys(t *testing.T) {
	store := NewStore()

	watch := store.Watch("watched")
	defer watch.Close()

	if err := store.Put("unrelated", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if watch.Conflicted() {
		t.Fatal("Conflicted() = true after writing an unrelated key, want only the watched keys to count")
	}

	if _, err := store.Get("unrelated"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if watch.Conflicted() {
		t.Fatal("Conflicted() = true after a read, want changes only")
	}

	if err := store.Put("watched", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !watch.Conflicted() {
		t.Fatal("Conflicted() = false after writing a watched key")
	}
}

func TestStoreWatchSeesDeleteAndFlush(t *testing.T) {
	store := NewStore()
	if err := store.Put("k", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	deleted := store.Watch("k")
	defer deleted.Close()

	if err := store.Delete("k"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted.Conflicted() {
		t.Fatal("Conflicted() = false after the watched key was deleted")
	}

	// A key created and then deleted again is still a change, which a version comparison
	// against the final state would miss.
	recreated := store.Watch("gone")
	defer recreated.Close()

	if err := store.Put("gone", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Delete("gone"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !recreated.Conflicted() {
		t.Fatal("Conflicted() = false after the watched key was created and removed")
	}

	flushed := store.Watch("anything")
	defer flushed.Close()

	if err := store.Write(func(tx *Tx) error {
		tx.Set("filler", Entry{Value: "v"})
		tx.Flush()

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !flushed.Conflicted() {
		t.Fatal("Conflicted() = false after a flush, want every watch marked")
	}
}

// TestStoreWatchIgnoresExpiryReclaim pins the difference between a change and bookkeeping: a
// watched key reaching an expiry it was armed against is not a conflict, so neither the lazy
// removal on read nor the sampling sweep may mark the watch. Marking it would hand the sweep's
// random sample a say in whether an unrelated caller's optimistic commit went through.
func TestStoreWatchIgnoresExpiryReclaim(t *testing.T) {
	store := NewStore()
	if err := store.Write(func(tx *Tx) error {
		tx.Set("k", Entry{Value: "v", ExpiresAt: time.Now().Add(-time.Second)})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	watch := store.Watch("k")
	defer watch.Close()

	// A read reclaims it lazily, and an unrelated write sweeps whatever is left.
	if _, err := store.Get("k"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() expired key error = %v, want %v", err, ErrKeyNotFound)
	}
	if err := store.Put("unrelated", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := store.tracked(); got != 1 {
		t.Fatalf("stored entries = %d, want the expired key reclaimed", got)
	}
	if watch.Conflicted() {
		t.Fatal("Conflicted() = true after the watched key merely expired")
	}

	// A real write to the same key still counts.
	if err := store.Put("k", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !watch.Conflicted() {
		t.Fatal("Conflicted() = false after the watched key was written")
	}
}

func TestStoreWatchCloseStopsTracking(t *testing.T) {
	store := NewStore()

	watch := store.Watch("k")
	watch.Close()
	watch.Close() // idempotent

	if err := store.Put("k", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if watch.Conflicted() {
		t.Fatal("Conflicted() = true after Close, want tracking to have stopped")
	}
}

// TestStoreReclaimsExpiredKeysOnWrite covers the sampling sweep: a key that is never read
// again must not hold its memory forever just because nothing touched it.
func TestStoreReclaimsExpiredKeysOnWrite(t *testing.T) {
	const abandoned = 500

	store := NewStore()
	if err := store.Write(func(tx *Tx) error {
		for i := range abandoned {
			tx.Set("expired"+strconv.Itoa(i), Entry{
				Value:     "v",
				ExpiresAt: time.Now().Add(-time.Second),
			})
		}

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Writes to an unrelated key sweep a sample of the expiring set each time.
	for i := range abandoned {
		if err := store.Put("live", strconv.Itoa(i)); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
	}

	if got := store.tracked(); got != 1 {
		t.Fatalf("stored entries = %d, want only the live key left", got)
	}
}

// TestStoreExpiryIndexStaysInStep guards the derived index: it must not keep a key that no
// longer has an expiry, or the sweep would walk entries it can never reclaim.
func TestStoreExpiryIndexStaysInStep(t *testing.T) {
	store := NewStore()

	if err := store.Write(func(tx *Tx) error {
		tx.Set("k", Entry{Value: "v", ExpiresAt: time.Now().Add(time.Hour)})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := store.expiringCount(); got != 1 {
		t.Fatalf("expiring = %d, want 1", got)
	}

	// A plain Put clears the expiry, so the key leaves the index.
	if err := store.Put("k", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := store.expiringCount(); got != 0 {
		t.Fatalf("expiring = %d after the expiry was cleared, want 0", got)
	}

	if err := store.Write(func(tx *Tx) error {
		tx.Set("k", Entry{Value: "v", ExpiresAt: time.Now().Add(time.Hour)})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := store.Delete("k"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := store.expiringCount(); got != 0 {
		t.Fatalf("expiring = %d after the key was deleted, want 0", got)
	}
}

// TestStoreSortedKeysCacheFollowsTheKeySet is the check that matters for paged scans: the order
// has to survive an overwrite, or every page pays for a fresh sort, and it has to be dropped
// whenever the key set moves, or a page reports a key that is gone.
func TestStoreSortedKeysCacheFollowsTheKeySet(t *testing.T) {
	store := NewStore()
	for _, key := range []string{"c", "a", "b"} {
		if err := store.Put(key, "v"); err != nil {
			t.Fatalf("Put(%q) error = %v", key, err)
		}
	}

	first := store.sortedKeys()
	if want := []string{"a", "b", "c"}; !slices.Equal(first, want) {
		t.Fatalf("SortedKeys() = %v, want %v", first, want)
	}

	// Overwriting a key leaves the key set alone, so the cached order is reused as it stands.
	if err := store.Put("b", "other"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if second := store.sortedKeys(); &second[0] != &first[0] {
		t.Fatal("SortedKeys() sorted again after an overwrite, want the cached order")
	}

	if err := store.Put("d", "v"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if want := []string{"a", "b", "c", "d"}; !slices.Equal(store.sortedKeys(), want) {
		t.Fatalf("SortedKeys() after an insert = %v, want %v", store.sortedKeys(), want)
	}

	if err := store.Delete("a"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if want := []string{"b", "c", "d"}; !slices.Equal(store.sortedKeys(), want) {
		t.Fatalf("SortedKeys() after a delete = %v, want %v", store.sortedKeys(), want)
	}

	if err := store.Write(func(tx *Tx) error {
		tx.Flush()

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := store.sortedKeys(); len(got) != 0 {
		t.Fatalf("SortedKeys() after a flush = %v, want none", got)
	}
}

// TestStoreWriteIsAtomic is the check that matters for the transaction layer: a
// read-modify-write done inside one Write must not lose a concurrent update.
func TestStoreWriteIsAtomic(t *testing.T) {
	const workers = 100

	store := NewStore()
	if err := store.Put("n", 0); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_ = store.Write(func(tx *Tx) error {
				entry, _ := tx.Get("n")
				count, _ := entry.Value.(int)
				tx.Set("n", Entry{Value: count + 1})

				return nil
			})
		}()
	}
	wg.Wait()

	got, err := store.Get("n")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != workers {
		t.Fatalf("Get() = %v, want %d", got, workers)
	}
}

func TestStorePutClearsExpiry(t *testing.T) {
	store := NewStore()

	if err := store.Write(func(tx *Tx) error {
		tx.Set("k", Entry{Value: "a", ExpiresAt: time.Now().Add(time.Minute)})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := store.Put("k", "b"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := store.Read(func(tx *ReadTx) error {
		entry, ok := tx.Get("k")
		if !ok {
			t.Fatal("Get() = missing, want the key")
		}
		if !entry.ExpiresAt.IsZero() {
			t.Fatalf("ExpiresAt = %v, want it cleared", entry.ExpiresAt)
		}

		return nil
	}); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
}
