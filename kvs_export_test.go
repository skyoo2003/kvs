package kvs

// tracked and expiringCount expose the derived indexes so the tests can assert that memory is
// actually reclaimed rather than merely hidden.

func (s *Store) tracked() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.data)
}

func (s *Store) expiringCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.expires)
}
