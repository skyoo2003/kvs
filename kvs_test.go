package kvs

import (
	"errors"
	"testing"
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
