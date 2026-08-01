package kvs

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// mutableCodec stores a value a caller can change in place, which is the shape that makes
// clone-on-Get load bearing. The RESP containers behave exactly this way: a push changes the
// stored list rather than replacing it.
type mutableCodec struct{}

func (mutableCodec) Encode(value interface{}) ([]byte, error) {
	list, ok := value.(*[]string)
	if !ok {
		return nil, ErrUnsupportedValue
	}

	data, err := json.Marshal(*list)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (mutableCodec) Decode(data []byte) (interface{}, error) {
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}

	return &list, nil
}

func (mutableCodec) Clone(value interface{}) interface{} {
	list, ok := value.(*[]string)
	if !ok {
		return value
	}
	copied := slices.Clone(*list)

	return &copied
}

func newSpeculativeStore(t *testing.T) *Store {
	t.Helper()

	store := NewStore()
	store.SetCodec(mutableCodec{})

	return store
}

// The whole point: Speculate reports what a transaction would do and leaves nothing behind.
func TestSpeculateReturnsTheFrameAndChangesNothing(t *testing.T) {
	store := newSpeculativeStore(t)
	existing := []string{"go"}
	if err := store.Put("langs", &existing); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	lines, err := store.Speculate(func(tx *Tx) error {
		entry, ok := tx.Get("langs")
		if !ok {
			t.Error("Get() missing the key the test just wrote")

			return nil
		}

		list, _ := entry.Value.(*[]string)
		*list = append(*list, "rust")
		tx.Set("langs", Entry{Value: list})
		tx.Set("added", Entry{Value: &[]string{"new"}})

		return nil
	})
	if err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("frame holds %d records, want 2", len(lines))
	}
	// The frame describes the new world.
	if got := string(lines[0]); !strings.Contains(got, `WyJnbyIsInJ1c3QiXQ==`) {
		t.Fatalf("frame record = %s, want it to carry [go rust]", got)
	}

	// The store is still in the old one, in-place change and all.
	if got := stored(t, store, "langs"); !slices.Equal(got, []string{"go"}) {
		t.Fatalf("stored list = %v, want %v — the in-place append survived the rollback", got, []string{"go"})
	}
	if _, err := store.Get("added"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf(`Get("added") error = %v, want %v`, err, ErrKeyNotFound)
	}
	if got := store.tracked(); got != 1 {
		t.Fatalf("tracked() = %d, want 1", got)
	}
}

func TestSpeculateRollsBackDeletesAndFlush(t *testing.T) {
	store := newSpeculativeStore(t)
	for _, key := range []string{"first", "second"} {
		if err := store.Put(key, &[]string{key}); err != nil {
			t.Fatalf("Put(%q) error = %v", key, err)
		}
	}

	if _, err := store.Speculate(func(tx *Tx) error {
		tx.Delete("first")
		tx.Flush()
		tx.Set("only", Entry{Value: &[]string{"survivor"}})

		return nil
	}); err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	if got := store.tracked(); got != 2 {
		t.Fatalf("tracked() after rollback = %d, want 2", got)
	}
	for _, key := range []string{"first", "second"} {
		if got := stored(t, store, key); !slices.Equal(got, []string{key}) {
			t.Fatalf("stored %q = %v, want %v", key, got, []string{key})
		}
	}
}

func TestSpeculateRestoresTheExpiryIndex(t *testing.T) {
	store := newSpeculativeStore(t)
	if err := store.Write(func(tx *Tx) error {
		tx.Set("keeps", Entry{Value: &[]string{"x"}, ExpiresAt: time.Now().Add(time.Hour)})
		tx.Set("plain", Entry{Value: &[]string{"y"}})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := store.Speculate(func(tx *Tx) error {
		// Clear one expiry, add another, delete the key that had one.
		tx.Set("keeps", Entry{Value: &[]string{"x"}})
		tx.Set("plain", Entry{Value: &[]string{"y"}, ExpiresAt: time.Now().Add(time.Hour)})
		tx.Delete("keeps")

		return nil
	}); err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	if got := store.expiringCount(); got != 1 {
		t.Fatalf("expiringCount() after rollback = %d, want 1", got)
	}
	if got := store.tracked(); got != 2 {
		t.Fatalf("tracked() after rollback = %d, want 2", got)
	}
}

// A speculative pass must not abort someone else's optimistic transaction over a change that
// never happened.
func TestSpeculateLeavesWatchersAlone(t *testing.T) {
	store := newSpeculativeStore(t)
	if err := store.Put("watched", &[]string{"before"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	watch := store.Watch("watched")
	defer watch.Close()

	if _, err := store.Speculate(func(tx *Tx) error {
		tx.Set("watched", Entry{Value: &[]string{"after"}})

		return nil
	}); err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	if watch.Conflicted() {
		t.Fatal("Conflicted() = true after a speculative write, want false")
	}

	// A real write still marks it.
	if err := store.Put("watched", &[]string{"after"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !watch.Conflicted() {
		t.Fatal("Conflicted() = false after a real write, want true")
	}
}

// A closure that fails leaves nothing behind either.
func TestSpeculateRollsBackOnError(t *testing.T) {
	store := newSpeculativeStore(t)
	failure := errors.New("no")

	if _, err := store.Speculate(func(tx *Tx) error {
		tx.Set("ghost", Entry{Value: &[]string{"x"}})

		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("Speculate() error = %v, want %v", err, failure)
	}

	if got := store.tracked(); got != 0 {
		t.Fatalf("tracked() = %d, want 0", got)
	}
}

// What Speculate produces has to be what ApplyReplicated consumes; otherwise consensus agrees
// on something no node can apply.
func TestSpeculateFrameAppliesElsewhere(t *testing.T) {
	leader := newSpeculativeStore(t)
	if err := leader.Put("langs", &[]string{"go"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	lines, err := leader.Speculate(func(tx *Tx) error {
		entry, _ := tx.Get("langs")
		list, _ := entry.Value.(*[]string)
		*list = append(*list, "rust")
		tx.Set("langs", Entry{Value: list})

		return nil
	})
	if err != nil {
		t.Fatalf("Speculate() error = %v", err)
	}

	follower := newSpeculativeStore(t)
	if err := follower.ApplyReplicated(lines); err != nil {
		t.Fatalf("ApplyReplicated() error = %v", err)
	}
	if got := stored(t, follower, "langs"); !slices.Equal(got, []string{"go", "rust"}) {
		t.Fatalf("follower list = %v, want %v", got, []string{"go", "rust"})
	}

	// Applying it on the leader too gets both to the same place.
	if err := leader.ApplyReplicated(lines); err != nil {
		t.Fatalf("ApplyReplicated() on leader error = %v", err)
	}
	if got := stored(t, leader, "langs"); !slices.Equal(got, []string{"go", "rust"}) {
		t.Fatalf("leader list = %v, want %v", got, []string{"go", "rust"})
	}
}

func TestSnapshotIsAFrameOfTheLiveKeyspace(t *testing.T) {
	store := newSpeculativeStore(t)
	if err := store.Put("langs", &[]string{"go"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	lines, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	rebuilt := newSpeculativeStore(t)
	if err := rebuilt.ReplaceWith(lines); err != nil {
		t.Fatalf("ReplaceWith() error = %v", err)
	}
	if got := stored(t, rebuilt, "langs"); !slices.Equal(got, []string{"go"}) {
		t.Fatalf("rebuilt list = %v, want %v", got, []string{"go"})
	}
}

func stored(t *testing.T, store *Store, key string) []string {
	t.Helper()

	value, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", key, err)
	}

	list, ok := value.(*[]string)
	if !ok {
		t.Fatalf("Get(%q) = %T, want *[]string", key, value)
	}

	return *list
}
