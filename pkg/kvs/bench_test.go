package kvs

import (
	"strconv"
	"testing"
)

// benchKeys is a fixed keyspace overwritten in place, the same shape the soak uses, so a run
// measures the write path rather than a map that keeps growing.
const benchKeys = 1000

// benchValue is small on purpose: the question is what a write costs, not how fast bytes copy.
const benchValue = "value"

// benchKeyNames is built once so that strconv is not what the benchmarks end up measuring.
var benchKeyNames = func() []string {
	names := make([]string, benchKeys)
	for i := range names {
		names[i] = "bench-" + strconv.Itoa(i)
	}

	return names
}()

func benchKey(i int) string { return benchKeyNames[i%benchKeys] }

func BenchmarkPut(b *testing.B) {
	benchPut(b, NewStore())
}

// BenchmarkPutDurable is the same write with the append log on. Every write is flushed to disk
// under the store's write lock, so this is the disk's sync rate more than anything kvs does.
func BenchmarkPutDurable(b *testing.B) {
	benchPut(b, openTestStore(b, b.TempDir()))
}

func benchPut(b *testing.B, store *Store) {
	i := 0
	for b.Loop() {
		if err := store.Put(benchKey(i), benchValue); err != nil {
			b.Fatalf("Put(%q) error = %v", benchKey(i), err)
		}
		i++
	}
}

func BenchmarkPutParallel(b *testing.B) {
	store := NewStore()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if err := store.Put(benchKey(i), benchValue); err != nil {
				b.Errorf("Put(%q) error = %v", benchKey(i), err)

				return
			}
		}
	})
}

func BenchmarkGet(b *testing.B) {
	store := NewStore()
	preload(b, store)

	i := 0
	for b.Loop() {
		if _, err := store.Get(benchKey(i)); err != nil {
			b.Fatalf("Get(%q) error = %v", benchKey(i), err)
		}
		i++
	}
}

// BenchmarkGetParallel is the read path under contention: every reader shares the store's one
// read lock, so this is what that lock costs once there are enough cores to fight over it.
func BenchmarkGetParallel(b *testing.B) {
	store := NewStore()
	preload(b, store)

	// RunParallel, unlike b.Loop, times everything since the function started.
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if _, err := store.Get(benchKey(i)); err != nil {
				b.Errorf("Get(%q) error = %v", benchKey(i), err)

				return
			}
		}
	})
}

// preload fills every benchmark key, so a Get never takes the not-found path.
func preload(b *testing.B, store *Store) {
	b.Helper()

	for i := range benchKeys {
		if err := store.Put(benchKey(i), benchValue); err != nil {
			b.Fatalf("Put(%q) error = %v", benchKey(i), err)
		}
	}
}
