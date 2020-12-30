// Package kv implements a key-value storage
package kv

// Store A structure of key-value store
type Store struct {
}

// Get get a value by key
func (s *Store) Get(key string) (interface{}, error) {
	return nil, nil
}

// Put put a value of key
func (s *Store) Put(key string, value interface{}) error {
	return nil
}

// Delete delete a key in store
func (s *Store) Delete(key string) error {
	return nil
}
