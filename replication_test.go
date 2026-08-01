package kvs

import (
	"errors"
	"testing"
)

// These cover the three methods consensus drives the store through: work out what a write would
// change, apply one agreed change, and rebuild from a snapshot.

// What one node worked out has to be what every other node applies.
func TestAgreedChangeAppliesOnAnotherNode(t *testing.T) {
	leader := newReplicatedStore(t)
	lines, err := leader.Speculate(func(tx *Tx) error {
		tx.Set("greeting", Entry{Value: "hello"})

		return nil
	})
	if err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	follower := newReplicatedStore(t)
	if err := follower.ApplyReplicated(lines); err != nil {
		t.Fatalf("ApplyReplicated() error = %v", err)
	}
	if got, err := follower.Get("greeting"); err != nil || got != "hello" {
		t.Fatalf("follower Get() = %v, %v, want %v, nil", got, err, "hello")
	}
}

// A rebuild starts from the leader's world, not from whatever the node happened to hold.
func TestReplaceWithDropsWhatTheLeaderNoLongerHas(t *testing.T) {
	replica := newReplicatedStore(t)
	if err := replica.Put("stale", "old"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	leader := newReplicatedStore(t)
	if err := leader.Put("fresh", "new"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	snapshot, err := leader.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if err := replica.ReplaceWith(snapshot); err != nil {
		t.Fatalf("ReplaceWith() error = %v", err)
	}

	if _, err := replica.Get("stale"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf(`Get("stale") after rebuild error = %v, want %v`, err, ErrKeyNotFound)
	}
	if got, err := replica.Get("fresh"); err != nil || got != "new" {
		t.Fatalf(`Get("fresh") after rebuild = %v, %v, want %v, nil`, got, err, "new")
	}
}

// A delete has to travel too, in order with the set that preceded it.
func TestAppliedFrameKeepsItsOrder(t *testing.T) {
	leader := newReplicatedStore(t)
	lines, err := leader.Speculate(func(tx *Tx) error {
		tx.Set("gone", Entry{Value: "x"})
		tx.Delete("gone")
		tx.Set("kept", Entry{Value: "y"})

		return nil
	})
	if err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	replica := newReplicatedStore(t)
	if err := replica.ApplyReplicated(lines); err != nil {
		t.Fatalf("ApplyReplicated() error = %v", err)
	}

	if _, err := replica.Get("gone"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf(`replica Get("gone") error = %v, want %v`, err, ErrKeyNotFound)
	}
	if got, err := replica.Get("kept"); err != nil || got != "y" {
		t.Fatalf(`replica Get("kept") = %v, %v, want %v, nil`, got, err, "y")
	}
}

// A plain in-memory store must not pay for bookkeeping only a log or a cluster needs.
func TestStoreWithoutALogRecordsNothing(t *testing.T) {
	store := newReplicatedStore(t)

	if err := store.Write(func(tx *Tx) error {
		tx.Set("greeting", Entry{Value: "hello"})
		if len(tx.pending) != 0 {
			t.Errorf("pending = %d, want 0 with no log", len(tx.pending))
		}

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

// A replicator takes over the write path entirely, which is how a clustered node makes every
// write pass consensus without the servers knowing.
func TestReplicatorTakesOverWrites(t *testing.T) {
	store := newReplicatedStore(t)

	seen := 0
	store.SetReplicator(func(fn func(tx *Tx) error) error {
		seen++

		return ErrNotLeader
	})

	if err := store.Put("greeting", "hello"); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("Put() error = %v, want %v", err, ErrNotLeader)
	}
	if seen != 1 {
		t.Fatalf("replicator called %d times, want 1", seen)
	}
	if _, err := store.Get("greeting"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() error = %v, want %v — the write must not have landed", err, ErrKeyNotFound)
	}

	store.SetReplicator(nil)
	if err := store.Put("greeting", "hello"); err != nil {
		t.Fatalf("Put() after clearing the replicator error = %v", err)
	}
}

func TestNotLeaderErrorCarriesTheLeader(t *testing.T) {
	err := error(&NotLeaderError{Leader: "127.0.0.1:6380"})
	if !errors.Is(err, ErrNotLeader) {
		t.Fatalf("errors.Is(%v, ErrNotLeader) = false, want true", err)
	}

	var notLeader *NotLeaderError
	if !errors.As(err, &notLeader) || notLeader.Leader != "127.0.0.1:6380" {
		t.Fatalf("errors.As() gave %v, want the leader address", notLeader)
	}

	if got := (&NotLeaderError{}).Error(); got == "" {
		t.Fatal("Error() with no leader is empty, want it to say an election is in progress")
	}
}

func newReplicatedStore(t *testing.T) *Store {
	t.Helper()

	store := NewStore()
	store.SetCodec(StringCodec{})

	return store
}
