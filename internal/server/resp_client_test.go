package server

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/skyoo2003/kvs"
)

// newGoRedisClient starts a RESP server and connects a real client library to it. Hand
// written bytes cover the wire format elsewhere; this exists to check the parts a client
// drives on its own, such as protocol negotiation, pipelining, and subscribe bookkeeping.
func newGoRedisClient(t *testing.T, opts *redis.Options) *redis.Client {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	server := NewRESPServer(kvs.NewStore(), opts.Password)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	opts.Addr = listener.Addr().String()
	client := redis.NewClient(opts)

	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		_ = listener.Close()

		if err := <-serveErr; err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	})

	return client
}

// TestGoRedisNegotiatesRESP2 is the compatibility check that matters most: go-redis asks for
// RESP3 by default, and kvs answers NOPROTO. The client has to fall back on its own for any
// of the rest of this to work.
func TestGoRedisNegotiatesRESP2(t *testing.T) {
	client := newGoRedisClient(t, &redis.Options{})

	if got, err := client.Ping(t.Context()).Result(); err != nil || got != respPong {
		t.Fatalf("Ping() = %q, %v, want %q", got, err, respPong)
	}
}

// TestGoRedisScriptFallsBackFromEvalSha is the shape scripting was added for: a client library
// sends EVALSHA first and the script itself only after a NOSCRIPT reply, so a server answering
// anything else leaves it reporting a failed EVAL for a call that never carried the script.
func TestGoRedisScriptFallsBackFromEvalSha(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	script := redis.NewScript(`
		local current = redis.call('GET', KEYS[1])
		if current == false then
			current = ARGV[1]
		else
			current = current .. ARGV[1]
		end
		redis.call('SET', KEYS[1], current)

		return current
	`)

	// The first run misses the cache and falls back to EVAL; the second finds the digest.
	for _, want := range []string{"a", "aa"} {
		got, err := script.Run(ctx, client, []string{"{default}:trie"}, "a").Text()
		if err != nil || got != want {
			t.Fatalf("Run() = %q, %v, want %q", got, err, want)
		}
	}

	if loaded, err := client.ScriptExists(ctx, script.Hash()).Result(); err != nil || !loaded[0] {
		t.Fatalf("ScriptExists() = %v, %v, want [true]", loaded, err)
	}
}

func TestGoRedisStringsAndExpiry(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	if err := client.Set(ctx, "greeting", "hello", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got, err := client.Get(ctx, "greeting").Result(); err != nil || got != "hello" {
		t.Fatalf("Get() = %q, %v", got, err)
	}

	// A missing key has to surface as redis.Nil, not as a protocol error.
	if _, err := client.Get(ctx, "missing").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("Get(missing) error = %v, want redis.Nil", err)
	}

	if err := client.Set(ctx, "temp", "v", time.Minute).Err(); err != nil {
		t.Fatalf("Set() with TTL error = %v", err)
	}
	if ttl, err := client.TTL(ctx, "temp").Result(); err != nil || ttl < 50*time.Second {
		t.Fatalf("TTL() = %v, %v, want about a minute", ttl, err)
	}

	if got, err := client.Incr(ctx, "counter").Result(); err != nil || got != 1 {
		t.Fatalf("Incr() = %d, %v", got, err)
	}
	if got, err := client.MGet(ctx, "greeting", "missing").Result(); err != nil || len(got) != 2 || got[1] != nil {
		t.Fatalf("MGet() = %v, %v", got, err)
	}
}

func TestGoRedisPipeline(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	pipe := client.Pipeline()
	for i := range 50 {
		pipe.Set(ctx, "key"+itoa(i), i, 0)
	}
	get := pipe.Get(ctx, "key7")

	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if got := get.Val(); got != "7" {
		t.Fatalf("pipelined Get() = %q, want %q", got, "7")
	}
	if size, err := client.DBSize(ctx).Result(); err != nil || size != 50 {
		t.Fatalf("DBSize() = %d, %v, want 50", size, err)
	}
}

func TestGoRedisHash(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	if err := client.HSet(ctx, "h", "a", "1", "b", "2").Err(); err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	if got, err := client.HGetAll(ctx, "h").Result(); err != nil || got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("HGetAll() = %v, %v", got, err)
	}
	if got, err := client.HIncrBy(ctx, "h", "a", 4).Result(); err != nil || got != 5 {
		t.Fatalf("HIncrBy() = %d, %v", got, err)
	}
	if got, err := client.Type(ctx, "h").Result(); err != nil || got != respTypeHash {
		t.Fatalf("Type() = %q, %v", got, err)
	}
}

func TestGoRedisList(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	if err := client.RPush(ctx, "l", "x", "y", "z").Err(); err != nil {
		t.Fatalf("RPush() error = %v", err)
	}
	if got, err := client.LRange(ctx, "l", 0, -1).Result(); err != nil || len(got) != 3 || got[0] != "x" {
		t.Fatalf("LRange() = %v, %v", got, err)
	}
	if got, err := client.LPop(ctx, "l").Result(); err != nil || got != "x" {
		t.Fatalf("LPop() = %q, %v", got, err)
	}
	if got, err := client.Type(ctx, "l").Result(); err != nil || got != respTypeList {
		t.Fatalf("Type() = %q, %v", got, err)
	}
}

func TestGoRedisSet(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	if err := client.SAdd(ctx, "s", "a", "b").Err(); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	if got, err := client.SCard(ctx, "s").Result(); err != nil || got != 2 {
		t.Fatalf("SCard() = %d, %v", got, err)
	}
	if got, err := client.SIsMember(ctx, "s", "a").Result(); err != nil || !got {
		t.Fatalf("SIsMember() = %t, %v", got, err)
	}
	if got, err := client.Type(ctx, "s").Result(); err != nil || got != respTypeSet {
		t.Fatalf("Type() = %q, %v", got, err)
	}
}

func TestGoRedisSortedSet(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	if err := client.ZAdd(ctx, "z", redis.Z{Score: 2, Member: "b"}, redis.Z{Score: 1, Member: "a"}).Err(); err != nil {
		t.Fatalf("ZAdd() error = %v", err)
	}

	got, err := client.ZRangeWithScores(ctx, "z", 0, -1).Result()
	if err != nil || len(got) != 2 {
		t.Fatalf("ZRangeWithScores() = %v, %v", got, err)
	}
	if got[0].Member != "a" || got[0].Score != 1 {
		t.Fatalf("ZRangeWithScores() first = %+v, want a at score 1", got[0])
	}

	if rank, err := client.ZRank(ctx, "z", "b").Result(); err != nil || rank != 1 {
		t.Fatalf("ZRank() = %d, %v", rank, err)
	}
	if name, err := client.Type(ctx, "z").Result(); err != nil || name != respTypeZSet {
		t.Fatalf("Type() = %q, %v", name, err)
	}
}

func TestGoRedisScanIterator(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	for i := range 20 {
		if err := client.Set(ctx, "user:"+itoa(i), i, 0).Err(); err != nil {
			t.Fatalf("Set() error = %v", err)
		}
	}
	if err := client.Set(ctx, "other", "x", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// The iterator has to terminate on the cursor kvs reports, which is always zero.
	seen := 0
	iter := client.Scan(ctx, 0, "user:*", 10).Iterator()
	for iter.Next(ctx) {
		seen++
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("Scan iterator error = %v", err)
	}
	if seen != 20 {
		t.Fatalf("Scan iterator saw %d keys, want 20", seen)
	}
}

func TestGoRedisTransaction(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	pipe := client.TxPipeline()
	pipe.Set(ctx, "k", "v", 0)
	incr := pipe.Incr(ctx, "n")

	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("TxPipeline Exec() error = %v", err)
	}
	if got := incr.Val(); got != 1 {
		t.Fatalf("queued Incr() = %d, want 1", got)
	}

	// Watch has to report the conflict as redis.TxFailedErr so the client can retry.
	err := client.Watch(ctx, func(tx *redis.Tx) error {
		if err := client.Set(ctx, "k", "raced", 0).Err(); err != nil {
			return err
		}

		_, execErr := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, "k", "changed", 0)

			return nil
		})

		return execErr
	}, "k")
	if !errors.Is(err, redis.TxFailedErr) {
		t.Fatalf("Watch() error = %v, want redis.TxFailedErr", err)
	}
	if got := client.Get(ctx, "k").Val(); got != "raced" {
		t.Fatalf("Get() after aborted transaction = %q, want the racing write to stand", got)
	}
}

func TestGoRedisPubSub(t *testing.T) {
	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	sub := client.Subscribe(ctx, "news")
	t.Cleanup(func() { _ = sub.Close() })

	// Receive blocks until the subscription is confirmed, so it also checks the reply shape.
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("Receive() error = %v", err)
	}

	if got, err := client.Publish(ctx, "news", "hello").Result(); err != nil || got != 1 {
		t.Fatalf("Publish() = %d, %v, want 1 receiver", got, err)
	}

	select {
	case msg := <-sub.Channel():
		if msg.Channel != "news" || msg.Payload != "hello" {
			t.Fatalf("message = %+v", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no message arrived")
	}
}

func TestGoRedisAuth(t *testing.T) {
	ctx := t.Context()

	client := newGoRedisClient(t, &redis.Options{Password: "s3cret"})
	if err := client.Set(ctx, "k", "v", 0).Err(); err != nil {
		t.Fatalf("Set() with the right password error = %v", err)
	}

	wrong := newGoRedisClient(t, &redis.Options{Password: "s3cret"})
	wrong.Options().Password = "nope"
	if err := wrong.Ping(ctx).Err(); err == nil {
		t.Fatal("Ping() with the wrong password succeeded, want it refused")
	}
}

// TestGoRedisCollectionScanIterators drives the paged collection walks through the client
// library's own iterators, which is the path that has to follow the cursor correctly.
func TestGoRedisCollectionScanIterators(t *testing.T) {
	const elements = 120

	ctx := t.Context()
	client := newGoRedisClient(t, &redis.Options{})

	for i := range elements {
		name := "e" + itoa(i)
		if err := client.HSet(ctx, "h", name, itoa(i)).Err(); err != nil {
			t.Fatalf("HSet() error = %v", err)
		}
		if err := client.SAdd(ctx, "s", name).Err(); err != nil {
			t.Fatalf("SAdd() error = %v", err)
		}
		if err := client.ZAdd(ctx, "z", redis.Z{Score: float64(i), Member: name}).Err(); err != nil {
			t.Fatalf("ZAdd() error = %v", err)
		}
	}

	tests := []struct {
		name     string
		iterator func() *redis.ScanIterator
		// A hash and a sorted set yield a value between each element.
		perElement int
	}{
		{
			name:       "HScan",
			iterator:   func() *redis.ScanIterator { return client.HScan(ctx, "h", 0, "", 25).Iterator() },
			perElement: 2,
		},
		{
			name:       "SScan",
			iterator:   func() *redis.ScanIterator { return client.SScan(ctx, "s", 0, "", 25).Iterator() },
			perElement: 1,
		},
		{
			name:       "ZScan",
			iterator:   func() *redis.ScanIterator { return client.ZScan(ctx, "z", 0, "", 25).Iterator() },
			perElement: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := 0
			iter := tt.iterator()
			for iter.Next(ctx) {
				seen++
			}
			if err := iter.Err(); err != nil {
				t.Fatalf("%s iterator error = %v", tt.name, err)
			}

			if want := elements * tt.perElement; seen != want {
				t.Fatalf("%s iterator yielded %d values, want %d", tt.name, seen, want)
			}
		})
	}
}
