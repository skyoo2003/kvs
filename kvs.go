// Package kvs implements an in-memory key-value store.
package kvs

import (
	"errors"
	"sync"
)

var (
	// ErrKeyNotFound is returned when the requested key does not exist.
	ErrKeyNotFound = errors.New("key not found")
)

// Store is an in-memory key-value store.
type Store struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

// NewStore creates a new Store instance.
func NewStore() *Store {
	return &Store{
		data: make(map[string]interface{}),
	}
}

// Get gets a value by key.
func (s *Store) Get(key string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data == nil {
		return nil, ErrKeyNotFound
	}

	value, ok := s.data[key]
	if !ok {
		return nil, ErrKeyNotFound
	}

	return value, nil
}

// Put stores a value under key.
func (s *Store) Put(key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data == nil {
		s.data = make(map[string]interface{})
	}

	s.data[key] = value
	return nil
}

// Delete removes a key from the store.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data == nil {
		return ErrKeyNotFound
	}

	if _, ok := s.data[key]; !ok {
		return ErrKeyNotFound
	}

	delete(s.data, key)
	return nil
}
