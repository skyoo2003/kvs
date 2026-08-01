package kvs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStringCodecRoundTrip(t *testing.T) {
	data, err := StringCodec{}.Encode("hello")
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	got, err := StringCodec{}.Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("Decode() = %v, want %v", got, "hello")
	}
}

func TestStringCodecRejectsOtherTypes(t *testing.T) {
	if _, err := (StringCodec{}).Encode(42); !errors.Is(err, ErrUnsupportedValue) {
		t.Fatalf("Encode(42) error = %v, want %v", err, ErrUnsupportedValue)
	}
}

// A value the codec cannot handle has to fail the write. Accepting it would mean reporting
// success for something that is gone at the next restart.
func TestStoreWriteFailsOnUnencodableValue(t *testing.T) {
	store := openTestStore(t, t.TempDir())

	if err := store.Put("answer", 42); !errors.Is(err, ErrUnsupportedValue) {
		t.Fatalf("Put() error = %v, want %v", err, ErrUnsupportedValue)
	}
}

func TestStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	want := map[string]string{"language": "go", "store": "kvs"}

	store := openTestStore(t, dir)
	for key, value := range want {
		if err := store.Put(key, value); err != nil {
			t.Fatalf("Put(%q) error = %v", key, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened := openTestStore(t, dir)
	for key, value := range want {
		got, err := reopened.Get(key)
		if err != nil {
			t.Fatalf("Get(%q) after restart error = %v", key, err)
		}
		if got != value {
			t.Fatalf("Get(%q) after restart = %v, want %v", key, got, value)
		}
	}
}

func TestStoreReplaysDeleteAndFlush(t *testing.T) {
	dir := t.TempDir()

	store := openTestStore(t, dir)
	if err := store.Put("gone", "x"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Delete("gone"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Put("flushed", "x"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Write(func(tx *Tx) error {
		tx.Flush()

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := store.Put("kept", "y"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	_ = store.Close()

	reopened := openTestStore(t, dir)
	for _, key := range []string{"gone", "flushed"} {
		if _, err := reopened.Get(key); !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("Get(%q) after restart error = %v, want %v", key, err, ErrKeyNotFound)
		}
	}
	if got, err := reopened.Get("kept"); err != nil || got != "y" {
		t.Fatalf(`Get("kept") after restart = %v, %v, want %v, nil`, got, err, "y")
	}
}

func TestStoreDropsExpiredEntriesOnRestart(t *testing.T) {
	dir := t.TempDir()

	store := openTestStore(t, dir)
	if err := store.Write(func(tx *Tx) error {
		tx.Set("stale", Entry{Value: "x", ExpiresAt: time.Now().Add(-time.Hour)})
		tx.Set("fresh", Entry{Value: "y", ExpiresAt: time.Now().Add(time.Hour)})

		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	_ = store.Close()

	reopened := openTestStore(t, dir)
	if _, err := reopened.Get("stale"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf(`Get("stale") after restart error = %v, want %v`, err, ErrKeyNotFound)
	}
	if got, err := reopened.Get("fresh"); err != nil || got != "y" {
		t.Fatalf(`Get("fresh") after restart = %v, %v, want %v, nil`, got, err, "y")
	}
	if got := reopened.tracked(); got != 1 {
		t.Fatalf("tracked() after restart = %d, want 1", got)
	}
}

// A crash part-way through an append leaves a record that stops mid-object. Everything written
// before it is still sound and has to come back.
func TestStoreToleratesTruncatedTail(t *testing.T) {
	dir := t.TempDir()

	store := openTestStore(t, dir)
	if err := store.Put("intact", "value"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	_ = store.Close()

	//nolint:gosec // The path is the test's own temporary directory.
	file, err := os.OpenFile(filepath.Join(dir, logName), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.WriteString(`{"op":"set","key":"cut`); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	_ = file.Close()

	reopened := openTestStore(t, dir)
	if got, err := reopened.Get("intact"); err != nil || got != "value" {
		t.Fatalf(`Get("intact") after truncated tail = %v, %v, want %v, nil`, got, err, "value")
	}
	if _, err := reopened.Get("cut"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf(`Get("cut") error = %v, want %v`, err, ErrKeyNotFound)
	}
}

func TestStoreCompactsLogOnOpen(t *testing.T) {
	dir := t.TempDir()

	store := openTestStore(t, dir)
	for i := range 50 {
		if err := store.Put("counter", string(rune('a'+i%26))); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
	}
	_ = store.Close()

	if got := logRecords(t, dir); got != 50 {
		t.Fatalf("records before compaction = %d, want 50", got)
	}

	reopened := openTestStore(t, dir)
	if got := logRecords(t, dir); got != 1 {
		t.Fatalf("records after compaction = %d, want 1", got)
	}
	if _, err := reopened.Get("counter"); err != nil {
		t.Fatalf(`Get("counter") after compaction error = %v`, err)
	}
}

func TestOpenDefaultsToStringCodec(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Put("language", "go"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// Closing twice is what a caller with a deferred Close and an explicit one does.
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

// A store with no log is still the default, and must not have gained a file or a failure.
func TestNewStoreWritesNothing(t *testing.T) {
	dir := t.TempDir()

	store := NewStore()
	if err := store.Put("language", "go"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() on in-memory store error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("in-memory store wrote %d files, want 0", len(entries))
	}
}

func openTestStore(t *testing.T, dir string) *Store {
	t.Helper()

	store, err := Open(dir, StringCodec{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return store
}

func logRecords(t *testing.T, dir string) int {
	t.Helper()

	//nolint:gosec // The path is the test's own temporary directory.
	data, err := os.ReadFile(filepath.Join(dir, logName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	return bytes.Count(data, []byte("\n"))
}
