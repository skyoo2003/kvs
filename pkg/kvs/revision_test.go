package kvs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/skyoo2003/kvs/internal/datadir"
)

// One transaction is one revision, however many keys it touched.
func TestRevisionAdvancesOncePerWrite(t *testing.T) {
	store := NewStore()
	if got := store.Revision(); got != 0 {
		t.Fatalf("Revision() of a new store = %d, want 0", got)
	}

	rev, err := store.WriteRevision(func(tx *Tx) error {
		tx.Set("a", Entry{Value: "1"})
		tx.Set("b", Entry{Value: "1"})

		return nil
	})
	if err != nil || rev != 1 {
		t.Fatalf("WriteRevision() = %d, %v, want 1, nil", rev, err)
	}

	if err := store.Put("a", "2"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := store.Revision(); got != 2 {
		t.Fatalf("Revision() = %d, want 2", got)
	}

	assertRevisions(t, store, "a", 1, 2)
	assertRevisions(t, store, "b", 1, 1)
}

// A write that changes nothing has nothing to number.
func TestWriteThatChangesNothingKeepsTheRevision(t *testing.T) {
	store := NewStore()
	if err := store.Put("a", "1"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	rev, err := store.WriteRevision(func(*Tx) error { return nil })
	if err != nil || rev != 1 {
		t.Fatalf("WriteRevision() of nothing = %d, %v, want 1, nil", rev, err)
	}
	if err := store.Delete("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, ErrKeyNotFound)
	}
	if got := store.Revision(); got != 1 {
		t.Fatalf("Revision() = %d, want 1", got)
	}
}

// A failed write reports no revision, but what it changed before failing is in memory, so the
// store's revision has moved past it.
func TestFailedWriteReportsNoRevision(t *testing.T) {
	store := NewStore()
	if err := store.Put("a", "1"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	failure := errors.New("halfway")
	rev, err := store.WriteRevision(func(tx *Tx) error {
		tx.Set("a", Entry{Value: "2"})

		return failure
	})
	if !errors.Is(err, failure) || rev != 0 {
		t.Fatalf("WriteRevision() = %d, %v, want 0, %v", rev, err, failure)
	}
	if got := store.Revision(); got != 2 {
		t.Fatalf("Revision() = %d, want 2", got)
	}
}

// A key deleted and written again is a new key, and says so.
func TestRecreatedKeyGetsANewCreateRevision(t *testing.T) {
	store := NewStore()
	for _, step := range []func() error{
		func() error { return store.Put("a", "1") },
		func() error { return store.Delete("a") },
		func() error { return store.Put("a", "2") },
	} {
		if err := step(); err != nil {
			t.Fatalf("step error = %v", err)
		}
	}

	assertRevisions(t, store, "a", 3, 3)
}

// An expired key is gone, so overwriting it creates the key rather than changing it.
func TestOverwrittenExpiredKeyCountsAsCreated(t *testing.T) {
	store := NewStore()
	if err := store.Write(func(tx *Tx) error {
		tx.Set("a", Entry{Value: "1", ExpiresAt: time.Now().Add(-time.Second)})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := store.Put("a", "2"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	assertRevisions(t, store, "a", 2, 2)
}

// The revision has to come back after a restart even when the last write was a delete, whose key
// the startup rewrite drops. Opening twice covers both the replayed log and the rewritten one.
func TestRevisionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	store := openTestStore(t, dir)
	for _, step := range []func() error{
		func() error { return store.Put("a", "1") },
		func() error { return store.Put("b", "1") },
		func() error { return store.Delete("b") },
	} {
		if err := step(); err != nil {
			t.Fatalf("step error = %v", err)
		}
	}

	for range 2 {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		store = openTestStore(t, dir)
		if got := store.Revision(); got != 3 {
			t.Fatalf("Revision() after restart = %d, want 3", got)
		}
		assertRevisions(t, store, "a", 1, 1)
	}

	rev, err := store.WriteRevision(func(tx *Tx) error {
		tx.Set("c", Entry{Value: "1"})

		return nil
	})
	if err != nil || rev != 4 {
		t.Fatalf("WriteRevision() after restart = %d, %v, want 4, nil", rev, err)
	}
}

// A directory written before revisions opens with everything it held at revision 1.
func TestLegacyLogOpensAtRevisionOne(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, datadir.FormatName), "1\n")
	// "aGVsbG8=" is "hello": the log stores values as base64.
	writeTestFile(t, filepath.Join(dir, logName), `{"op":"set","key":"a","value":"aGVsbG8="}`+"\n")

	store := openTestStore(t, dir)
	if got, err := store.Get("a"); err != nil || got != "hello" {
		t.Fatalf(`Get("a") = %v, %v, want %v, nil`, got, err, "hello")
	}
	if got := store.Revision(); got != 1 {
		t.Fatalf("Revision() = %d, want 1", got)
	}
	assertRevisions(t, store, "a", 1, 1)
}

// The leader's guess at a revision is only a guess: what the cluster agreed to number the write
// is what every node records.
func TestReplicatedWriteTakesTheAgreedRevision(t *testing.T) {
	leader := newReplicatedStore(t)
	follower := newReplicatedStore(t)

	set, err := leader.Speculate(func(tx *Tx) error {
		tx.Set("a", Entry{Value: "1"})

		return nil
	})
	if err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}
	if rev, applyErr := follower.ApplyReplicated(7, set); applyErr != nil || rev != 7 {
		t.Fatalf("ApplyReplicated(7) = %d, %v, want 7, nil", rev, applyErr)
	}
	assertRevisions(t, follower, "a", 7, 7)

	del, err := follower.Speculate(func(tx *Tx) error {
		tx.Delete("a")

		return nil
	})
	if err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}
	if rev, applyErr := follower.ApplyReplicated(9, del); applyErr != nil || rev != 9 {
		t.Fatalf("ApplyReplicated(9) = %d, %v, want 9, nil", rev, applyErr)
	}
}

// A snapshot is the agreed state, revision included, even when the node held a later one.
func TestReplaceWithTakesTheSnapshotRevision(t *testing.T) {
	leader := newReplicatedStore(t)
	replica := newReplicatedStore(t)

	for range 3 {
		if err := leader.Put("a", "leader"); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
	}
	for range 5 {
		if err := replica.Put("b", "replica"); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
	}

	snapshot, err := leader.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if err := replica.ReplaceWith(snapshot); err != nil {
		t.Fatalf("ReplaceWith() error = %v", err)
	}

	if got := replica.Revision(); got != 3 {
		t.Fatalf("Revision() after ReplaceWith = %d, want 3", got)
	}
	assertRevisions(t, replica, "a", 1, 3)
}

// A snapshot of a store nothing was ever written to carries revision zero, and so does the node
// restored from it, whatever it held before.
func TestReplaceWithAnEmptySnapshotKeepsRevisionZero(t *testing.T) {
	snapshot, err := newReplicatedStore(t).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	replica := newReplicatedStore(t)
	if err := replica.Put("b", "replica"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := replica.ReplaceWith(snapshot); err != nil {
		t.Fatalf("ReplaceWith() error = %v", err)
	}

	if got := replica.Revision(); got != 0 {
		t.Fatalf("Revision() after ReplaceWith = %d, want 0", got)
	}
}

// A store emptied by a flush still has a revision to snapshot, and nothing that needs a codec.
func TestSnapshotOfAnEmptiedStoreNeedsNoCodec(t *testing.T) {
	store := NewStore()
	if err := store.Put("a", "1"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Write(func(tx *Tx) error {
		tx.Flush()

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := store.Snapshot(); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got := store.Revision(); got != 2 {
		t.Fatalf("Revision() = %d, want 2", got)
	}
}

func assertRevisions(t *testing.T, store *Store, key string, create, mod int64) {
	t.Helper()

	var entry Entry
	if err := store.Read(func(tx *ReadTx) error {
		var ok bool
		if entry, ok = tx.Get(key); !ok {
			return ErrKeyNotFound
		}

		return nil
	}); err != nil {
		t.Fatalf("Get(%q) error = %v", key, err)
	}

	if entry.CreateRevision != create || entry.ModRevision != mod {
		t.Fatalf("%q revisions = create %d, mod %d, want create %d, mod %d",
			key, entry.CreateRevision, entry.ModRevision, create, mod)
	}
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
