package server

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/skyoo2003/kvs"
)

func TestRESPCodecRoundTrip(t *testing.T) {
	zset := newRESPZSet()
	zset.set("alice", 1.5)
	zset.set("bob", 2)

	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "string", value: "hello"},
		{name: "list", value: newRESPList([]string{"go", "rust"})},
		{name: "hash", value: map[string]string{"name": "kvs", "language": "go"}},
		{name: "set", value: map[string]struct{}{"kv": {}, "go": {}}},
		{name: "zset", value: zset},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := respCodec{}.Encode(test.value)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			got, err := respCodec{}.Decode(data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			want := respFingerprint(test.value)
			if respFingerprint(got) != want {
				t.Fatalf("Decode() = %s, want %s", respFingerprint(got), want)
			}
		})
	}
}

func TestRESPCodecRejectsWhatItCannotStore(t *testing.T) {
	if _, err := (respCodec{}).Encode(42); !errors.Is(err, kvs.ErrUnsupportedValue) {
		t.Fatalf("Encode(42) error = %v, want %v", err, kvs.ErrUnsupportedValue)
	}

	_, err := respCodec{}.Decode([]byte(`{"type":"stream","value":null}`))
	if !errors.Is(err, kvs.ErrUnsupportedValue) {
		t.Fatalf("Decode() error = %v, want %v", err, kvs.ErrUnsupportedValue)
	}
}

// Without a DataDir the store is the in-memory one kvs has always had, which takes values no
// codec has to understand.
func TestOpenStoreWithoutDataDirStaysInMemory(t *testing.T) {
	store, err := OpenStore(Config{})
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Put("answer", 42); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
}

// The end-to-end claim: what a Redis client writes is still there after the process that took
// it is gone.
func TestRESPValuesSurviveRestart(t *testing.T) {
	dir := t.TempDir()

	first, err := OpenStore(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	client, stop := serveRESP(t, first)
	writeOneOfEachType(t, client)
	stop()

	if err = first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := OpenStore(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenStore() after restart error = %v", err)
	}
	defer func() { _ = second.Close() }()

	restarted, stopAgain := serveRESP(t, second)
	defer stopAgain()

	assertOneOfEachType(t, restarted)
}

func writeOneOfEachType(t *testing.T, client *redis.Client) {
	t.Helper()
	ctx := t.Context()

	mustRESP(t, "SET", client.Set(ctx, "greeting", "hello", 0).Err())
	mustRESP(t, "RPUSH", client.RPush(ctx, "langs", "go", "rust").Err())
	mustRESP(t, "HSET", client.HSet(ctx, "meta", "name", "kvs").Err())
	mustRESP(t, "SADD", client.SAdd(ctx, "tags", "kv", "go").Err())
	mustRESP(t, "ZADD", client.ZAdd(ctx, "scores", redis.Z{Score: 1.5, Member: "alice"}).Err())
}

func assertOneOfEachType(t *testing.T, client *redis.Client) {
	t.Helper()
	ctx := t.Context()

	greeting, err := client.Get(ctx, "greeting").Result()
	mustRESP(t, "GET", err)
	if greeting != "hello" {
		t.Fatalf("GET after restart = %q, want %q", greeting, "hello")
	}

	langs, err := client.LRange(ctx, "langs", 0, -1).Result()
	mustRESP(t, "LRANGE", err)
	if want := []string{"go", "rust"}; !slices.Equal(langs, want) {
		t.Fatalf("LRANGE after restart = %v, want %v", langs, want)
	}

	name, err := client.HGet(ctx, "meta", "name").Result()
	mustRESP(t, "HGET", err)
	if name != "kvs" {
		t.Fatalf("HGET after restart = %q, want %q", name, "kvs")
	}

	tags, err := client.SMembers(ctx, "tags").Result()
	mustRESP(t, "SMEMBERS", err)
	slices.Sort(tags)
	if want := []string{"go", "kv"}; !slices.Equal(tags, want) {
		t.Fatalf("SMEMBERS after restart = %v, want %v", tags, want)
	}

	score, err := client.ZScore(ctx, "scores", "alice").Result()
	mustRESP(t, "ZSCORE", err)
	if score != 1.5 {
		t.Fatalf("ZSCORE after restart = %v, want %v", score, 1.5)
	}
}

func mustRESP(t *testing.T, name string, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s error = %v", name, err)
	}
}

// respFingerprint renders a stored value in a canonical form, so a round trip is checked by
// comparing two strings rather than by a bespoke assertion per type. The type name leads, which
// makes a value that came back as the wrong container fail as loudly as a wrong element.
func respFingerprint(value interface{}) string {
	name := respTypeName(value)

	switch typed := value.(type) {
	case string:
		return name + ":" + typed
	case *respList:
		return name + ":" + strings.Join(typed.live(), ",")
	case map[string]string:
		pairs := make([]string, 0, len(typed))
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			pairs = append(pairs, key+"="+typed[key])
		}

		return name + ":" + strings.Join(pairs, ",")
	case map[string]struct{}:
		return name + ":" + strings.Join(slices.Sorted(maps.Keys(typed)), ",")
	case *respZSet:
		// sorted() rather than the score map, so the order a restart has to rebuild from the
		// scores alone is part of what is compared.
		pairs := make([]string, 0, typed.len())
		for _, member := range typed.sorted() {
			score, _ := typed.score(member)
			pairs = append(pairs, fmt.Sprintf("%s=%g", member, score))
		}

		return name + ":" + strings.Join(pairs, ",")
	}

	return fmt.Sprintf("%s:%v", name, value)
}

// serveRESP puts a RESP server in front of store and returns a client for it, along with the
// shutdown the restart test has to call part-way through rather than at the end.
func serveRESP(t *testing.T, store *kvs.Store) (client *redis.Client, stop func()) {
	t.Helper()

	return serveRESPAt(t, store, "127.0.0.1:0")
}

// serveRESPAt is serveRESP bound to a chosen address, which the replication tests need so that
// a second leader can take the first one's place.
func serveRESPAt(t *testing.T, store *kvs.Store, addr string) (client *redis.Client, stop func()) {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	server := NewRESPServer(store, "")
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	client = redis.NewClient(&redis.Options{Addr: listener.Addr().String()})
	stopped := false

	return client, func() {
		if stopped {
			return
		}
		stopped = true

		_ = client.Close()
		_ = server.Close()
		_ = listener.Close()

		if err := <-serveErr; err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	}
}
