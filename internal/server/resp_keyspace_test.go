package server

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

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

	// GETEX shares its option parser with SET, so the flags only SET takes have to be
	// refused rather than accepted and quietly dropped.
	client.do("-"+respErrSyntax+respCRLF, "GETEX", "g", "EX", "10", "NX")
	client.do("-"+respErrSyntax+respCRLF, "GETEX", "g", "KEEPTTL")
	// An unusable expiry names the command the client sent, not the parser it shares.
	client.do("-ERR invalid expire time in 'getex' command"+respCRLF, "GETEX", "g", "EX", "0")
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

	// A cursor the server does not hold is refused, not treated as a finished iteration.
	client.do("-"+errRESPInvalidCursor.Error()+respCRLF, "SCAN", "17")

	client.do("-"+respErrSyntax+respCRLF, "SCAN", "0", "MATCH")
	client.do("-"+errRESPInvalidCursor.Error()+respCRLF, "SCAN", "abc")
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
	client.do("+OK"+respCRLF, "SET", "ttl", "v", "EX", "100")
	client.send("INFO")
	// expires counts the keys carrying a TTL, which the line used to report as zero always.
	if info := client.readBulk(); !strings.Contains(info, "db0:keys=2,expires=1,avg_ttl=0") {
		t.Fatalf("INFO = %q, want the keyspace section", info)
	}
}

// TestRESPRandomKeySkipsExpiredKeys covers the sampling. RANDOMKEY draws from the cached key
// order, which still lists keys that expired without being reclaimed, so a draw landing on one
// has to keep looking rather than report it.
func TestRESPRandomKeySkipsExpiredKeys(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "live", "v")
	for i := range 3 {
		client.do("+OK"+respCRLF, "SET", "gone:"+itoa(i), "v", "PX", "1")
	}
	// Nothing writes past this point, so the expired keys stay unreclaimed for the draw.
	time.Sleep(20 * time.Millisecond)

	for range 20 {
		client.send("RANDOMKEY")
		if got := client.readBulk(); got != "live" {
			t.Fatalf("RANDOMKEY = %q, want the only live key", got)
		}
	}
}

// TestRESPScanCursorsAreNotGuessable covers the cursor table being server-wide. Handing out
// counted-up ids meant a client presenting a small number of its own landed on someone else's
// walk and moved its resume point, which made that walk skip keys.
func TestRESPScanCursorsAreNotGuessable(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	const keyCount = 40
	for i := range keyCount {
		client.do("+OK"+respCRLF, "SET", "k:"+itoa(i), "v")
	}

	client.send("SCAN", "0", "COUNT", "5")
	client.expect("*2" + respCRLF)
	cursor := client.readBulk()
	seen := len(client.readStringArray())
	if cursor == "0" {
		t.Fatal("SCAN finished in one page, want an open cursor to interfere with")
	}

	// Every id a client would plausibly invent has to be unknown, so that presenting one is
	// refused rather than moving the walk above.
	for id := 1; id <= 64; id++ {
		client.do("-"+errRESPInvalidCursor.Error()+respCRLF, "SCAN", itoa(id), "COUNT", "5")
	}

	// The original walk still reaches every key.
	for cursor != "0" {
		client.send("SCAN", cursor, "COUNT", "5")
		client.expect("*2" + respCRLF)
		cursor = client.readBulk()
		seen += len(client.readStringArray())
	}
	if seen != keyCount {
		t.Fatalf("SCAN saw %d keys, want %d", seen, keyCount)
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
		{pattern: "*a", name: "ba", want: true},
		{pattern: "*a", name: "ab", want: false},
		{pattern: "a*b*c", name: "axxbyyc", want: true},
		{pattern: "a*b*c", name: "axxcyyb", want: false},
		{pattern: "*[a-c]*", name: "zzbzz", want: true},
		{pattern: "*[a-c]*", name: "zzzzz", want: false},
		{pattern: "a*", name: "a", want: true},
		{pattern: "*?", name: "", want: false},
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

// TestRESPScanRejectsUnknownCursor makes a cursor the server does not hold an error rather than
// a finished page.
//
// This used to answer "0" with no keys, so that a client reconnecting with a cursor its own
// socket never issued was not handed an error. Cursors moved to the server precisely so that a
// pooled client's continuation is found, which leaves eviction, invention, and a restart as the
// only ways to present an unknown one. "0" with no keys is the protocol's signal that the walk
// finished, so answering it there told those clients they had enumerated a keyspace they had
// barely started.
func TestRESPScanRejectsUnknownCursor(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "k", "v")
	client.do("-"+errRESPInvalidCursor.Error()+respCRLF, "SCAN", "9999")
	client.do("-"+errRESPInvalidCursor.Error()+respCRLF, "HSCAN", "h", "9999")
}

// TestRESPScanRejectsEvictedCursor is the case that makes a silently finished page cost a client
// real keys: the table evicts by map order once it is full, so unrelated walks can drop a live
// iteration's entry.
func TestRESPScanRejectsEvictedCursor(t *testing.T) {
	store := kvs.NewStore()
	const keys = 3000
	for i := range keys {
		if err := store.Put("key:"+itoa(i), "v"); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
	}

	client := newRESPClient(t, store)

	client.send("SCAN", "0", "COUNT", "10")
	client.expect("*2" + respCRLF)
	cursor := client.readBulk()
	if seen := client.readStringArray(); len(seen) != 10 {
		t.Fatalf("first page = %d keys, want 10", len(seen))
	}

	// Open far more iterations than the table holds. Each one past the cap evicts an arbitrary
	// entry, so after this many the cursor above is gone with near certainty.
	for range 20 * respScanCursorLimit {
		client.send("SCAN", "0", "COUNT", "10")
		client.expect("*2" + respCRLF)
		client.readBulk()
		client.readStringArray()
	}

	client.do("-"+errRESPInvalidCursor.Error()+respCRLF, "SCAN", cursor, "COUNT", "10")
}

// TestRespGlobMatchStaysLinear guards the matcher against exponential backtracking. Recursing
// at every star made this pattern take about 49 seconds for one 40 byte name, and KEYS runs the
// pattern against every key while holding the store's read lock.
func TestRespGlobMatchStaysLinear(t *testing.T) {
	name := strings.Repeat("a", 40)
	pattern := strings.Repeat("a*", 12) + "b"

	start := time.Now()
	if respGlobMatch(pattern, name) {
		t.Fatalf("respGlobMatch(%q, %q) = true, want false", pattern, name)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("respGlobMatch(%q, 40 bytes) took %v, want a linear-time match", pattern, elapsed)
	}
}

// TestRESPRefusesOutOfRangeArguments covers the arguments that used to be taken at face value.
// Each one either panicked the connection's goroutine, which takes the process down with it, or
// wrapped around into a value that silently destroyed data.
func TestRESPRefusesOutOfRangeArguments(t *testing.T) {
	const maxInt64 = "9223372036854775807"
	const minInt64 = "-9223372036854775808"

	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "k", "v")

	// offset+len(value) overflowed, skipped the zero padding, and panicked on the copy.
	client.do("-"+respErrStringTooLong+respCRLF, "SETRANGE", "k", maxInt64, "x")
	client.do("-"+respErrStringTooLong+respCRLF, "SETRANGE", "k", "536870911", "xx")

	// These wrapped into the past, so the key was dropped while the reply claimed success.
	client.do("-ERR invalid expire time in 'expire' command"+respCRLF, "EXPIRE", "k", maxInt64)
	client.do("-ERR invalid expire time in 'pexpire' command"+respCRLF, "PEXPIRE", "k", maxInt64)
	client.do("-ERR invalid expire time in 'set' command"+respCRLF, "SET", "k2", "v", "EX", maxInt64)
	client.do("-ERR invalid expire time in 'setex' command"+respCRLF, "SETEX", "k2", maxInt64, "v")
	client.do("$1"+respCRLF+"v"+respCRLF, "GET", "k")

	// abs(math.MinInt) stays negative, which made this a slice bound out of range.
	client.do(":2"+respCRLF, "RPUSH", "l", "v", "v")
	client.do(":2"+respCRLF, "LREM", "l", minInt64, "v")

	// The connection is still usable, which is the point of all of the above.
	client.do("+PONG"+respCRLF, "PING")
}

// TestRespPickMembersClampsRepeats keeps SRANDMEMBER's negative count from asking for a slice
// the server cannot allocate. math.MinInt has no positive counterpart, so negating it left a
// negative capacity and panicked.
func TestRespPickMembersClampsRepeats(t *testing.T) {
	members := []string{"a", "b"}

	if got := len(respPickMembers(members, math.MinInt)); got != respRepeatLimit {
		t.Fatalf("respPickMembers(math.MinInt) returned %d members, want %d", got, respRepeatLimit)
	}
	if got := len(respPickMembers(members, -3)); got != 3 {
		t.Fatalf("respPickMembers(-3) returned %d members, want 3", got)
	}
}

// TestRESPScanCursorResumesOnAnotherConnection is the case a per-connection cursor table got
// wrong: client libraries pool connections, so the SCAN that opens a cursor and the one that
// continues it routinely land on different sockets.
func TestRESPScanCursorResumesOnAnotherConnection(t *testing.T) {
	clients := newRESPClients(t, 2)
	opener, resumer := clients[0], clients[1]

	for i := range 30 {
		opener.do("+OK"+respCRLF, "SET", fmt.Sprintf("k%02d", i), "v")
	}

	seen := make(map[string]struct{}, 30)
	cursor := "0"
	for range 30 {
		// Alternate connections to prove the cursor is not tied to either one.
		client := opener
		if len(seen)%2 == 1 {
			client = resumer
		}

		client.send("SCAN", cursor, "COUNT", "5")
		client.expect("*2" + respCRLF)
		cursor = client.readBulk()
		for _, key := range client.readStringArray() {
			seen[key] = struct{}{}
		}

		if cursor == "0" {
			break
		}
	}

	if len(seen) != 30 {
		t.Fatalf("SCAN across two connections saw %d keys, want 30", len(seen))
	}
}
