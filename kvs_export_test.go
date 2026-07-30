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

// sortedKeys reads the cached key order the way a scan does, through a read transaction.
func (s *Store) sortedKeys() []string {
	var keys []string
	_ = s.Read(func(tx *ReadTx) error {
		keys = tx.SortedKeys()

		return nil
	})

	return keys
}
