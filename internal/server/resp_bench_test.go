package server

import (
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9"
)

// These go through a real client library over loopback TCP, so they price the whole RESP path:
// parsing, dispatch, the store, and the reply. The network is the machine's own and costs little.

// benchKeys is a fixed keyspace overwritten in place, so a run measures the command path rather
// than a map that keeps growing. The store's benchmarks use the same shape.
const benchKeys = 1000

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

func BenchmarkRESPSet(b *testing.B) {
	client := newGoRedisClient(b, &redis.Options{})
	ctx := b.Context()

	i := 0
	for b.Loop() {
		if err := client.Set(ctx, benchKey(i), benchValue, 0).Err(); err != nil {
			b.Fatalf("Set(%q) error = %v", benchKey(i), err)
		}
		i++
	}
}

func BenchmarkRESPGet(b *testing.B) {
	client := newGoRedisClient(b, &redis.Options{})
	ctx := b.Context()

	for i := range benchKeys {
		if err := client.Set(ctx, benchKey(i), benchValue, 0).Err(); err != nil {
			b.Fatalf("Set(%q) error = %v", benchKey(i), err)
		}
	}

	i := 0
	for b.Loop() {
		if err := client.Get(ctx, benchKey(i)).Err(); err != nil {
			b.Fatalf("Get(%q) error = %v", benchKey(i), err)
		}
		i++
	}
}

// BenchmarkRESPSetParallel shares one client, and so its connection pool, across goroutines,
// which is how a service using go-redis sends writes.
func BenchmarkRESPSetParallel(b *testing.B) {
	client := newGoRedisClient(b, &redis.Options{})
	ctx := b.Context()

	// RunParallel, unlike b.Loop, would otherwise time the server starting.
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if err := client.Set(ctx, benchKey(i), benchValue, 0).Err(); err != nil {
				b.Errorf("Set(%q) error = %v", benchKey(i), err)

				return
			}
		}
	})
}
