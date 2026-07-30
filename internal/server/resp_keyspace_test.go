package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/skyoo2003/kvs"
)

func TestRESPStringCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("$-1"+respCRLF, "GET", "missing")
	client.do("+OK"+respCRLF, "SET", "greeting", "hello")
	client.do("$5"+respCRLF+"hello"+respCRLF, "GET", "greeting")

	client.do(":11"+respCRLF, "APPEND", "greeting", " world")
	client.do("$10"+respCRLF+"hello worl"+respCRLF, "GETRANGE", "greeting", "0", "9")
	client.do(":11"+respCRLF, "STRLEN", "greeting")
	client.do("$5"+respCRLF+"world"+respCRLF, "GETRANGE", "greeting", "-5", "-1")

	client.do("$11"+respCRLF+"hello world"+respCRLF, "GETSET", "greeting", "replaced")
	client.do("$8"+respCRLF+"replaced"+respCRLF, "GETDEL", "greeting")
	client.do(":0"+respCRLF, "EXISTS", "greeting")

	client.do("+OK"+respCRLF, "MSET", "a", "1", "b", "2")
	client.do("*3"+respCRLF+"$1"+respCRLF+"1"+respCRLF+"$1"+respCRLF+"2"+respCRLF+"$-1"+respCRLF,
		"MGET", "a", "b", "missing")
	client.do(":0"+respCRLF, "MSETNX", "b", "9", "c", "3")
	client.do("$1"+respCRLF+"2"+respCRLF, "GET", "b")
	client.do(":1"+respCRLF, "MSETNX", "c", "3", "d", "4")

	client.do(":5"+respCRLF, "SETRANGE", "pad", "2", "abc")
	client.do("$5"+respCRLF+"\x00\x00abc"+respCRLF, "GET", "pad")
}

func TestRESPSetOptions(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":1"+respCRLF, "SETNX", "k", "first")
	client.do(":0"+respCRLF, "SETNX", "k", "second")
	client.do("$5"+respCRLF+"first"+respCRLF, "GET", "k")

	// NX on an existing key is a no-op that reports a null.
	client.do("$-1"+respCRLF, "SET", "k", "third", "NX")
	// XX on a missing key is likewise a no-op.
	client.do("$-1"+respCRLF, "SET", "missing", "v", "XX")
	client.do(":0"+respCRLF, "EXISTS", "missing")

	// The GET option answers with the previous value instead of OK.
	client.do("$5"+respCRLF+"first"+respCRLF, "SET", "k", "fourth", "GET")
	client.do("$-1"+respCRLF, "SET", "fresh", "v", "GET")

	client.do("+OK"+respCRLF, "SET", "ttl", "v", "EX", "100")
	client.do(":100"+respCRLF, "TTL", "ttl")
	// KEEPTTL leaves the expiry in place while a plain SET drops it.
	client.do("+OK"+respCRLF, "SET", "ttl", "w", "KEEPTTL")
	client.do(":100"+respCRLF, "TTL", "ttl")
	client.do("+OK"+respCRLF, "SET", "ttl", "x")
	client.do(":-1"+respCRLF, "TTL", "ttl")

	client.do("-"+respErrSyntax+respCRLF, "SET", "k", "v", "NX", "XX")
	client.do("-"+respErrSyntax+respCRLF, "SET", "k", "v", "EX", "10", "KEEPTTL")
	client.do("-"+respErrNotInteger+respCRLF, "SET", "k", "v", "EX", "abc")
	client.do("-"+respErrSyntax+respCRLF, "SET", "k", "v", "BOGUS")

	// An absolute expiry already in the past stores a key that is immediately invisible.
	client.do("+OK"+respCRLF, "SET", "past", "v", "PXAT", "1")
	client.do("$-1"+respCRLF, "GET", "past")
}

func TestRESPIncrementCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":1"+respCRLF, "INCR", "n")
	client.do(":3"+respCRLF, "INCRBY", "n", "2")
	client.do(":2"+respCRLF, "DECR", "n")
	client.do(":-3"+respCRLF, "DECRBY", "n", "5")
	client.do("$4"+respCRLF+"-2.5"+respCRLF, "INCRBYFLOAT", "n", "0.5")

	client.do("+OK"+respCRLF, "SET", "word", "abc")
	client.do("-"+respErrNotInteger+respCRLF, "INCR", "word")
	client.do("-"+errRESPNotFloat.Error()+respCRLF, "INCRBYFLOAT", "word", "1.0")

	// An increment keeps the key's expiry.
	client.do("+OK"+respCRLF, "SET", "counter", "1", "EX", "100")
	client.do(":2"+respCRLF, "INCR", "counter")
	client.do(":100"+respCRLF, "TTL", "counter")

	client.do("+OK"+respCRLF, "SET", "big", "9223372036854775807")
	client.do("-"+errRESPOverflow.Error()+respCRLF, "INCR", "big")
}

func TestRESPExpiryCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":-2"+respCRLF, "TTL", "missing")
	client.do("+OK"+respCRLF, "SET", "k", "v")
	client.do(":-1"+respCRLF, "TTL", "k")

	client.do(":1"+respCRLF, "EXPIRE", "k", "50")
	client.do(":50"+respCRLF, "TTL", "k")
	client.do(":1"+respCRLF, "PERSIST", "k")
	client.do(":-1"+respCRLF, "TTL", "k")
	client.do(":0"+respCRLF, "PERSIST", "k")
	client.do(":0"+respCRLF, "EXPIRE", "missing", "50")

	// An expiry in the past removes the key outright.
	client.do(":1"+respCRLF, "EXPIREAT", "k", "1")
	client.do(":0"+respCRLF, "EXISTS", "k")

	client.do("+OK"+respCRLF, "SET", "g", "v", "EX", "100")
	client.do("$1"+respCRLF+"v"+respCRLF, "GETEX", "g", "PERSIST")
	client.do(":-1"+respCRLF, "TTL", "g")
	client.do("$1"+respCRLF+"v"+respCRLF, "GETEX", "g", "EX", "60")
	client.do(":60"+respCRLF, "TTL", "g")
	// GETEX with no option leaves the expiry alone.
	client.do("$1"+respCRLF+"v"+respCRLF, "GETEX", "g")
	client.do(":60"+respCRLF, "TTL", "g")
}

func TestRESPKeyspaceCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MSET", "a", "1", "b", "2", "c", "3")
	client.do(":3"+respCRLF, "DBSIZE")
	client.do(":2"+respCRLF, "EXISTS", "a", "b", "nope")
	client.do("+string"+respCRLF, "TYPE", "a")
	client.do("+none"+respCRLF, "TYPE", "nope")

	client.do(":2"+respCRLF, "DEL", "a", "b")
	client.do(":0"+respCRLF, "UNLINK", "a")

	client.do("+OK"+respCRLF, "RENAME", "c", "renamed")
	client.do("$1"+respCRLF+"3"+respCRLF, "GET", "renamed")
	client.do("-"+errRESPNoSuchKey.Error()+respCRLF, "RENAME", "nope", "x")
	client.do("+OK"+respCRLF, "SET", "target", "keep")
	client.do(":0"+respCRLF, "RENAMENX", "renamed", "target")
	client.do("$4"+respCRLF+"keep"+respCRLF, "GET", "target")

	client.do(":0"+respCRLF, "COPY", "renamed", "target")
	client.do(":1"+respCRLF, "COPY", "renamed", "target", "REPLACE")
	client.do("$1"+respCRLF+"3"+respCRLF, "GET", "target")
	client.do(":0"+respCRLF, "COPY", "nope", "x")

	client.do("+OK"+respCRLF, "FLUSHALL")
	client.do(":0"+respCRLF, "DBSIZE")
	client.do("$-1"+respCRLF, "RANDOMKEY")
}

func TestRESPKeysAndScan(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MSET", "user:1", "a", "user:2", "b", "post/1", "c")

	assertKeys := func(want []string, args ...string) {
		t.Helper()

		client.send(args...)
		got := client.readStringArray()
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("%v = %v, want %v", args, got, want)
		}
	}

	assertKeys([]string{"post/1", "user:1", "user:2"}, "KEYS", "*")
	assertKeys([]string{"user:1", "user:2"}, "KEYS", "user:*")
	assertKeys([]string{"user:1"}, "KEYS", "user:[1]")
	assertKeys([]string{"user:2"}, "KEYS", "user:[^1]")
	// A '*' has to cross a slash, which is where path.Match would have gone wrong.
	assertKeys([]string{"post/1"}, "KEYS", "post*1")

	// SCAN answers the whole keyspace in one batch and reports cursor 0.
	client.send("SCAN", "0")
	client.expect("*2" + respCRLF + "$1" + respCRLF + "0" + respCRLF)
	if got := client.readStringArray(); len(got) != 3 {
		t.Fatalf("SCAN keys = %v, want 3 keys", got)
	}

	client.send("SCAN", "0", "MATCH", "user:*", "COUNT", "10")
	client.expect("*2" + respCRLF + "$1" + respCRLF + "0" + respCRLF)
	got := client.readStringArray()
	slices.Sort(got)
	if !slices.Equal(got, []string{"user:1", "user:2"}) {
		t.Fatalf("SCAN MATCH = %v, want the user keys", got)
	}

	// A non-zero cursor is the end of the iteration.
	client.send("SCAN", "17")
	client.expect("*2" + respCRLF + "$1" + respCRLF + "0" + respCRLF + "*0" + respCRLF)

	client.do("-"+respErrSyntax+respCRLF, "SCAN", "0", "MATCH")
	client.do("-ERR invalid cursor"+respCRLF, "SCAN", "abc")
}

// TestRESPWrongTypeOnNonStringValue uses the Go API to store a value the string commands
// cannot handle, which is what a client sees for a key holding another type.
func TestRESPWrongTypeOnNonStringValue(t *testing.T) {
	store := kvs.NewStore()
	if err := store.Put("number", 42); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	client := newRESPClient(t, store)

	client.do("-"+respErrWrongType+respCRLF, "GET", "number")
	client.do("-"+respErrWrongType+respCRLF, "APPEND", "number", "x")
	client.do("-"+respErrWrongType+respCRLF, "STRLEN", "number")
	// MGET reports a wrongly typed key as absent rather than failing the whole lookup.
	client.do("*1"+respCRLF+"$-1"+respCRLF, "MGET", "number")
	// TYPE reports what it does not recognize as absent.
	client.do("+none"+respCRLF, "TYPE", "number")
}

func TestRESPInfoReportsKeyspace(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "k", "v")
	client.send("INFO")
	if info := client.readBulk(); !strings.Contains(info, "db0:keys=1,expires=0,avg_ttl=0") {
		t.Fatalf("INFO = %q, want the keyspace section", info)
	}
}

func TestRespGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{pattern: "*", name: "", want: true},
		{pattern: "*", name: "a/b:c", want: true},
		{pattern: "**", name: "abc", want: true},
		{pattern: "a*c", name: "abbbc", want: true},
		{pattern: "a*c", name: "abbb", want: false},
		{pattern: "a?c", name: "abc", want: true},
		{pattern: "a?c", name: "ac", want: false},
		{pattern: "h[ae]llo", name: "hello", want: true},
		{pattern: "h[ae]llo", name: "hillo", want: false},
		{pattern: "h[^e]llo", name: "hallo", want: true},
		{pattern: "h[^e]llo", name: "hello", want: false},
		{pattern: "k[a-c]y", name: "kby", want: true},
		{pattern: "k[a-c]y", name: "kdy", want: false},
		{pattern: "k[c-a]y", name: "kby", want: true},
		{pattern: `a\*c`, name: "a*c", want: true},
		{pattern: `a\*c`, name: "abc", want: false},
		{pattern: "a[", name: "a", want: false},
		{pattern: "", name: "", want: true},
		{pattern: "", name: "a", want: false},
		{pattern: "abc", name: "abc", want: true},
		{pattern: "abc", name: "abcd", want: false},
	}

	for _, tt := range tests {
		if got := respGlobMatch(tt.pattern, tt.name); got != tt.want {
			t.Errorf("respGlobMatch(%q, %q) = %t, want %t", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestRespRange(t *testing.T) {
	tests := []struct {
		start, end, size int
		wantFrom, wantTo int
		wantOK           bool
	}{
		{start: 0, end: 2, size: 5, wantFrom: 0, wantTo: 3, wantOK: true},
		{start: -2, end: -1, size: 5, wantFrom: 3, wantTo: 5, wantOK: true},
		{start: 0, end: 99, size: 3, wantFrom: 0, wantTo: 3, wantOK: true},
		{start: -99, end: 0, size: 3, wantFrom: 0, wantTo: 1, wantOK: true},
		{start: 2, end: 1, size: 5, wantOK: false},
		{start: 0, end: 0, size: 0, wantOK: false},
		{start: 0, end: -99, size: 3, wantOK: false},
	}

	for _, tt := range tests {
		from, to, ok := respRange(tt.start, tt.end, tt.size)
		if ok != tt.wantOK || (ok && (from != tt.wantFrom || to != tt.wantTo)) {
			t.Errorf("respRange(%d, %d, %d) = %d, %d, %t, want %d, %d, %t",
				tt.start, tt.end, tt.size, from, to, ok, tt.wantFrom, tt.wantTo, tt.wantOK)
		}
	}
}

// TestRESPScanPagesTheKeyspace covers the paged walk: each call returns a bounded page and the
// iteration as a whole still sees every key exactly once.
func TestRESPScanPagesTheKeyspace(t *testing.T) {
	const keys = 55

	client := newRESPClient(t, kvs.NewStore())
	for i := range keys {
		client.do("+OK"+respCRLF, "SET", "key"+itoa(i), "v")
	}

	seen := map[string]int{}
	cursor := "0"
	rounds := 0

	for {
		client.send("SCAN", cursor, "COUNT", "10")
		client.expect("*2" + respCRLF)
		cursor = client.readBulk()
		page := client.readStringArray()

		if len(page) > 10 {
			t.Fatalf("SCAN page = %d keys, want COUNT to bound it", len(page))
		}
		for _, key := range page {
			seen[key]++
		}

		rounds++
		if cursor == "0" {
			break
		}
		if rounds > keys {
			t.Fatal("SCAN did not terminate")
		}
	}

	if rounds < 2 {
		t.Fatalf("SCAN finished in %d round(s), want the walk to be paged", rounds)
	}
	if len(seen) != keys {
		t.Fatalf("SCAN saw %d distinct keys, want %d", len(seen), keys)
	}
	for key, count := range seen {
		if count != 1 {
			t.Fatalf("SCAN returned %q %d times, want once", key, count)
		}
	}
}

// TestRESPScanKeepsSeenKeysWhenEarlierOnesVanish is why the cursor is a key rather than an
// index: deleting keys the walk already passed must not shift the remainder and make it skip.
func TestRESPScanKeepsSeenKeysWhenEarlierOnesVanish(t *testing.T) {
	const keys = 30

	store := kvs.NewStore()
	client := newRESPClient(t, store)
	for i := range keys {
		client.do("+OK"+respCRLF, "SET", "key"+itoa(i), "v")
	}

	client.send("SCAN", "0", "COUNT", "10")
	client.expect("*2" + respCRLF)
	cursor := client.readBulk()
	first := client.readStringArray()
	if cursor == "0" {
		t.Fatal("SCAN finished in one round, want more pages to remain")
	}

	// Drop everything the first page already reported, which is what an index cursor could
	// not survive.
	for _, key := range first {
		if err := store.Delete(key); err != nil {
			t.Fatalf("Delete(%q) error = %v", key, err)
		}
	}

	seen := make(map[string]struct{}, keys)
	for _, key := range first {
		seen[key] = struct{}{}
	}
	for cursor != "0" {
		client.send("SCAN", cursor, "COUNT", "10")
		client.expect("*2" + respCRLF)
		cursor = client.readBulk()
		for _, key := range client.readStringArray() {
			seen[key] = struct{}{}
		}
	}

	if len(seen) != keys {
		t.Fatalf("the iteration saw %d of %d keys, want none skipped", len(seen), keys)
	}
}

// TestRESPScanUnknownCursorEndsIteration keeps a reconnecting client from getting an error for
// a cursor this connection never issued.
func TestRESPScanUnknownCursorEndsIteration(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "k", "v")
	client.do("*2"+respCRLF+"$1"+respCRLF+"0"+respCRLF+"*0"+respCRLF, "SCAN", "9999")
}
