package lsm

import (
	"errors"
	"testing"
)

func TestNewStartsEmpty(t *testing.T) {
	tree := New()

	if len(tree.memtable) != 0 {
		t.Fatalf("len(memtable) = %v, want 0", len(tree.memtable))
	}
	if len(tree.segments) != 0 {
		t.Fatalf("len(segments) = %v, want 0", len(tree.segments))
	}
	if tree.memtableLimit != DefaultMemtableLimit {
		t.Fatalf("memtableLimit = %v, want %v", tree.memtableLimit, DefaultMemtableLimit)
	}
}

func TestTreeZeroValueIsUsable(t *testing.T) {
	var tree Tree

	if err := tree.Put("answer", 42); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := tree.Get("answer")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != 42 {
		t.Fatalf("Get() = %v, want %v", got, 42)
	}
}

func TestNilTreeIsSafe(t *testing.T) {
	var tree *Tree

	if err := tree.Put("answer", 42); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Put() error = %v, want %v", err, ErrKeyNotFound)
	}
	if _, err := tree.Get("answer"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrKeyNotFound)
	}
	if err := tree.Delete("answer"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, ErrKeyNotFound)
	}
	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
}

func TestTreePutGet(t *testing.T) {
	tree := NewWithMemtableLimit(8)

	if err := tree.Put("language", "go"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := tree.Get("language")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "go" {
		t.Fatalf("Get() = %v, want %v", got, "go")
	}
}

func TestTreePutOverwritesValue(t *testing.T) {
	tree := NewWithMemtableLimit(8)

	if err := tree.Put("language", "go"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := tree.Put("language", "golang"); err != nil {
		t.Fatalf("Put() overwrite error = %v", err)
	}

	assertValue(t, tree, "language", "golang")
}

func TestTreeMissingKey(t *testing.T) {
	tree := New()

	if _, err := tree.Get("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrKeyNotFound)
	}
	if err := tree.Delete("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestTreeFlushEmptyMemtableIsNoOp(t *testing.T) {
	tree := New()

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(tree.segments) != 0 {
		t.Fatalf("len(segments) = %v, want 0", len(tree.segments))
	}
}

func TestTreeFlushPreservesReadableValues(t *testing.T) {
	tree := NewWithMemtableLimit(8)
	putPair(t, tree, "a", 1)
	putPair(t, tree, "b", 2)

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	assertValue(t, tree, "a", 1)
	assertValue(t, tree, "b", 2)
	if len(tree.memtable) != 0 {
		t.Fatalf("len(memtable) = %v, want 0", len(tree.memtable))
	}
	if len(tree.segments) != 1 {
		t.Fatalf("len(segments) = %v, want 1", len(tree.segments))
	}
}

func TestTreeGetPrefersMemtableOverFlushedSegment(t *testing.T) {
	tree := NewWithMemtableLimit(8)
	putPair(t, tree, "language", "go")

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	putPair(t, tree, "language", "golang")

	assertValue(t, tree, "language", "golang")
}

func TestTreeDeleteFromMemtable(t *testing.T) {
	tree := NewWithMemtableLimit(8)
	putPair(t, tree, "language", "go")

	if err := tree.Delete("language"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	assertMissing(t, tree, "language")
}

func TestTreeDeleteFromFlushedSegmentCreatesTombstone(t *testing.T) {
	tree := NewWithMemtableLimit(8)
	putPair(t, tree, "language", "go")

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := tree.Delete("language"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	assertMissing(t, tree, "language")
	if current := tree.memtable["language"]; !current.deleted {
		t.Fatal("memtable entry is not a tombstone")
	}
}

func TestTreeFlushPersistsDeleteTombstone(t *testing.T) {
	tree := NewWithMemtableLimit(8)
	putPair(t, tree, "language", "go")

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := tree.Delete("language"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() tombstone error = %v", err)
	}

	assertMissing(t, tree, "language")
	if !tree.segments[0].entries[0].deleted {
		t.Fatal("flushed entry is not a tombstone")
	}
}

func TestTreeMultipleFlushesUseNewestSegmentFirst(t *testing.T) {
	tree := NewWithMemtableLimit(8)
	putPair(t, tree, "language", "go")

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	putPair(t, tree, "language", "golang")

	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush() second error = %v", err)
	}

	assertValue(t, tree, "language", "golang")
}

func TestTreeAutoFlushesMemtable(t *testing.T) {
	tree := NewWithMemtableLimit(2)
	putPair(t, tree, "a", 1)
	putPair(t, tree, "b", 2)

	if len(tree.memtable) != 0 {
		t.Fatalf("len(memtable) = %v, want 0", len(tree.memtable))
	}
	if len(tree.segments) != 1 {
		t.Fatalf("len(segments) = %v, want 1", len(tree.segments))
	}
	assertValue(t, tree, "a", 1)
	assertValue(t, tree, "b", 2)
}

func putPair(t *testing.T, tree *Tree, key string, value interface{}) {
	t.Helper()

	if err := tree.Put(key, value); err != nil {
		t.Fatalf("Put(%q) error = %v", key, err)
	}
}

func assertValue(t *testing.T, tree *Tree, key string, want interface{}) {
	t.Helper()

	got, err := tree.Get(key)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", key, err)
	}
	if got != want {
		t.Fatalf("Get(%q) = %v, want %v", key, got, want)
	}
}

func assertMissing(t *testing.T, tree *Tree, key string) {
	t.Helper()

	_, err := tree.Get(key)
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get(%q) error = %v, want %v", key, err, ErrKeyNotFound)
	}
}
