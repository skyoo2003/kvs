package server

import (
	"maps"
	"slices"
	"testing"

	"github.com/skyoo2003/kvs"
)

func TestRESPHashCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":2"+respCRLF, "HSET", "h", "a", "1", "b", "2")
	// Overwriting a field is not a new field, so nothing is added.
	client.do(":0"+respCRLF, "HSET", "h", "a", "9")
	client.do("$1"+respCRLF+"9"+respCRLF, "HGET", "h", "a")
	client.do("$-1"+respCRLF, "HGET", "h", "missing")
	client.do("$-1"+respCRLF, "HGET", "missing", "a")
	client.do(":2"+respCRLF, "HLEN", "h")
	client.do(":1"+respCRLF, "HEXISTS", "h", "a")
	client.do(":0"+respCRLF, "HEXISTS", "h", "zz")
	client.do("+hash"+respCRLF, "TYPE", "h")

	client.do(":0"+respCRLF, "HSETNX", "h", "a", "nope")
	client.do(":1"+respCRLF, "HSETNX", "h", "c", "3")

	client.send("HGETALL", "h")
	if got := client.readStringMap(); !maps.Equal(got, map[string]string{"a": "9", "b": "2", "c": "3"}) {
		t.Fatalf("HGETALL = %v", got)
	}

	client.send("HKEYS", "h")
	keys := client.readStringArray()
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"a", "b", "c"}) {
		t.Fatalf("HKEYS = %v", keys)
	}

	client.send("HVALS", "h")
	values := client.readStringArray()
	slices.Sort(values)
	if !slices.Equal(values, []string{"2", "3", "9"}) {
		t.Fatalf("HVALS = %v", values)
	}

	client.do("*2"+respCRLF+"$1"+respCRLF+"9"+respCRLF+"$-1"+respCRLF, "HMGET", "h", "a", "zz")

	client.do(":5"+respCRLF, "HINCRBY", "h", "b", "3")
	client.do("$3"+respCRLF+"5.5"+respCRLF, "HINCRBYFLOAT", "h", "b", "0.5")
	client.do(":1"+respCRLF, "HINCRBY", "h", "fresh", "1")

	// A field that does not hold a number cannot be incremented.
	client.do(":1"+respCRLF, "HSET", "words", "w", "abc")
	client.do("-"+errRESPHashNotInteger.Error()+respCRLF, "HINCRBY", "words", "w", "1")
	client.do("-"+errRESPHashNotFloat.Error()+respCRLF, "HINCRBYFLOAT", "words", "w", "1.0")

	client.send("HSCAN", "h", "0", "MATCH", "a*")
	client.expect("*2" + respCRLF + "$1" + respCRLF + "0" + respCRLF)
	if got := client.readStringMap(); !maps.Equal(got, map[string]string{"a": "9"}) {
		t.Fatalf("HSCAN MATCH = %v", got)
	}

	// Removing the last field removes the key, since Redis has no empty collections.
	client.do(":4"+respCRLF, "HDEL", "h", "a", "b", "c", "fresh")
	client.do(":0"+respCRLF, "EXISTS", "h")
	client.do("+none"+respCRLF, "TYPE", "h")
}

func TestRESPListCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	// Each argument is pushed in turn, so the last one ends up at the head.
	client.do(":3"+respCRLF, "RPUSH", "l", "a", "b", "c")
	client.do(":5"+respCRLF, "LPUSH", "l", "x", "y")
	client.do("*5"+respCRLF+"$1"+respCRLF+"y"+respCRLF+"$1"+respCRLF+"x"+respCRLF+
		"$1"+respCRLF+"a"+respCRLF+"$1"+respCRLF+"b"+respCRLF+"$1"+respCRLF+"c"+respCRLF,
		"LRANGE", "l", "0", "-1")
	client.do(":5"+respCRLF, "LLEN", "l")
	client.do("+list"+respCRLF, "TYPE", "l")

	client.do("$1"+respCRLF+"y"+respCRLF, "LINDEX", "l", "0")
	client.do("$1"+respCRLF+"c"+respCRLF, "LINDEX", "l", "-1")
	client.do("$-1"+respCRLF, "LINDEX", "l", "99")

	client.do("+OK"+respCRLF, "LSET", "l", "0", "Y")
	client.do("$1"+respCRLF+"Y"+respCRLF, "LINDEX", "l", "0")
	client.do("-"+errRESPIndexRange.Error()+respCRLF, "LSET", "l", "99", "z")
	client.do("-"+errRESPNoSuchKey.Error()+respCRLF, "LSET", "missing", "0", "z")

	// Without a count the reply is one bulk string; with a count it is an array.
	client.do("$1"+respCRLF+"Y"+respCRLF, "LPOP", "l")
	client.do("*2"+respCRLF+"$1"+respCRLF+"c"+respCRLF+"$1"+respCRLF+"b"+respCRLF, "RPOP", "l", "2")
	client.do("*2"+respCRLF+"$1"+respCRLF+"x"+respCRLF+"$1"+respCRLF+"a"+respCRLF, "LRANGE", "l", "0", "-1")

	client.do(":0"+respCRLF, "LPUSHX", "absent", "v")
	client.do(":0"+respCRLF, "EXISTS", "absent")
	client.do(":3"+respCRLF, "LPUSHX", "l", "a")

	// A positive count removes from the head, a negative one from the tail.
	client.do(":1"+respCRLF, "LREM", "l", "1", "a")
	client.do("*2"+respCRLF+"$1"+respCRLF+"x"+respCRLF+"$1"+respCRLF+"a"+respCRLF, "LRANGE", "l", "0", "-1")
	client.do(":4"+respCRLF, "RPUSH", "l", "a", "a")
	client.do(":1"+respCRLF, "LREM", "l", "-1", "a")
	client.do("*3"+respCRLF+"$1"+respCRLF+"x"+respCRLF+"$1"+respCRLF+"a"+respCRLF+"$1"+respCRLF+"a"+respCRLF,
		"LRANGE", "l", "0", "-1")
	client.do(":2"+respCRLF, "LREM", "l", "0", "a")

	client.do("+OK"+respCRLF, "LTRIM", "l", "0", "0")
	client.do(":1"+respCRLF, "LLEN", "l")
	// Trimming everything away removes the key.
	client.do("+OK"+respCRLF, "LTRIM", "l", "5", "10")
	client.do(":0"+respCRLF, "EXISTS", "l")

	client.do("$-1"+respCRLF, "LPOP", "gone")
	client.do("*-1"+respCRLF, "LPOP", "gone", "2")
	client.do("*0"+respCRLF, "LRANGE", "gone", "0", "-1")
}

func TestRESPSetCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":3"+respCRLF, "SADD", "s", "a", "b", "c")
	client.do(":0"+respCRLF, "SADD", "s", "a")
	client.do(":3"+respCRLF, "SCARD", "s")
	client.do(":1"+respCRLF, "SISMEMBER", "s", "a")
	client.do(":0"+respCRLF, "SISMEMBER", "s", "zz")
	client.do("+set"+respCRLF, "TYPE", "s")

	client.send("SMEMBERS", "s")
	members := client.readStringArray()
	slices.Sort(members)
	if !slices.Equal(members, []string{"a", "b", "c"}) {
		t.Fatalf("SMEMBERS = %v", members)
	}

	client.do(":2"+respCRLF, "SADD", "other", "c", "d")

	assertSet := func(want []string, args ...string) {
		t.Helper()

		client.send(args...)
		got := client.readStringArray()
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("%v = %v, want %v", args, got, want)
		}
	}

	assertSet([]string{"a", "b", "c", "d"}, "SUNION", "s", "other")
	assertSet([]string{"c"}, "SINTER", "s", "other")
	assertSet([]string{"a", "b"}, "SDIFF", "s", "other")

	client.send("SSCAN", "s", "0")
	client.expect("*2" + respCRLF + "$1" + respCRLF + "0" + respCRLF)
	scanned := client.readStringArray()
	slices.Sort(scanned)
	if !slices.Equal(scanned, []string{"a", "b", "c"}) {
		t.Fatalf("SSCAN = %v", scanned)
	}

	client.send("SPOP", "s")
	if got := client.readBulk(); got == "" {
		t.Fatal("SPOP returned nothing, want a member")
	}
	client.do(":2"+respCRLF, "SCARD", "s")

	client.send("SRANDMEMBER", "s", "-4")
	if got := client.readStringArray(); len(got) != 4 {
		t.Fatalf("SRANDMEMBER -4 = %v, want 4 picks with repeats allowed", got)
	}

	client.do(":2"+respCRLF, "SREM", "s", "a", "b", "c")
	client.do(":0"+respCRLF, "EXISTS", "s")
	client.do("$-1"+respCRLF, "SPOP", "gone")
	client.do("*0"+respCRLF, "SMEMBERS", "gone")
}

func TestRESPSortedSetCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":3"+respCRLF, "ZADD", "z", "2", "b", "1", "a", "3", "c")
	client.do(":3"+respCRLF, "ZCARD", "z")
	client.do("$1"+respCRLF+"1"+respCRLF, "ZSCORE", "z", "a")
	client.do("$-1"+respCRLF, "ZSCORE", "z", "zz")
	client.do("+zset"+respCRLF, "TYPE", "z")

	client.do("*3"+respCRLF+"$1"+respCRLF+"a"+respCRLF+"$1"+respCRLF+"b"+respCRLF+"$1"+respCRLF+"c"+respCRLF,
		"ZRANGE", "z", "0", "-1")
	client.do("*3"+respCRLF+"$1"+respCRLF+"c"+respCRLF+"$1"+respCRLF+"b"+respCRLF+"$1"+respCRLF+"a"+respCRLF,
		"ZREVRANGE", "z", "0", "-1")
	client.do("*4"+respCRLF+"$1"+respCRLF+"a"+respCRLF+"$1"+respCRLF+"1"+respCRLF+
		"$1"+respCRLF+"b"+respCRLF+"$1"+respCRLF+"2"+respCRLF,
		"ZRANGE", "z", "0", "1", "WITHSCORES")

	client.do(":0"+respCRLF, "ZRANK", "z", "a")
	client.do(":2"+respCRLF, "ZRANK", "z", "c")
	client.do(":0"+respCRLF, "ZREVRANK", "z", "c")
	client.do("$-1"+respCRLF, "ZRANK", "z", "zz")

	client.do(":3"+respCRLF, "ZCOUNT", "z", "-inf", "+inf")
	client.do(":2"+respCRLF, "ZCOUNT", "z", "1", "2")
	// A leading parenthesis is an exclusive bound.
	client.do(":1"+respCRLF, "ZCOUNT", "z", "(1", "2")
	client.do("-"+errRESPMinMaxFloat.Error()+respCRLF, "ZCOUNT", "z", "abc", "2")

	client.do("*2"+respCRLF+"$1"+respCRLF+"b"+respCRLF+"$1"+respCRLF+"c"+respCRLF,
		"ZRANGEBYSCORE", "z", "(1", "+inf")
	// The reverse form takes its bounds highest first.
	client.do("*2"+respCRLF+"$1"+respCRLF+"c"+respCRLF+"$1"+respCRLF+"b"+respCRLF,
		"ZREVRANGEBYSCORE", "z", "+inf", "(1")

	client.do("$3"+respCRLF+"1.5"+respCRLF, "ZINCRBY", "z", "0.5", "a")
	client.do("$3"+respCRLF+"1.5"+respCRLF, "ZSCORE", "z", "a")
	client.do("*2"+respCRLF+"$3"+respCRLF+"1.5"+respCRLF+"$1"+respCRLF+"2"+respCRLF, "ZMSCORE", "z", "a", "b")

	client.do(":2"+respCRLF, "ZREM", "z", "a", "b", "zz")
	client.do(":1"+respCRLF, "ZCARD", "z")
	client.do(":1"+respCRLF, "ZREM", "z", "c")
	client.do(":0"+respCRLF, "EXISTS", "z")
}

func TestRESPSortedSetAddOptions(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":1"+respCRLF, "ZADD", "z", "5", "m")
	// NX refuses to touch a member that is already there.
	client.do(":0"+respCRLF, "ZADD", "z", "NX", "9", "m")
	client.do("$1"+respCRLF+"5"+respCRLF, "ZSCORE", "z", "m")
	// XX refuses to create one.
	client.do(":0"+respCRLF, "ZADD", "z", "XX", "1", "fresh")
	client.do(":0"+respCRLF, "EXISTS", "fresh")

	// CH counts changed scores on top of new members.
	client.do(":1"+respCRLF, "ZADD", "z", "CH", "7", "m")
	client.do("$1"+respCRLF+"7"+respCRLF, "ZSCORE", "z", "m")
	client.do(":0"+respCRLF, "ZADD", "z", "CH", "7", "m")

	// GT only raises a score, LT only lowers it.
	client.do(":0"+respCRLF, "ZADD", "z", "GT", "3", "m")
	client.do("$1"+respCRLF+"7"+respCRLF, "ZSCORE", "z", "m")
	client.do(":0"+respCRLF, "ZADD", "z", "GT", "8", "m")
	client.do("$1"+respCRLF+"8"+respCRLF, "ZSCORE", "z", "m")
	client.do(":0"+respCRLF, "ZADD", "z", "LT", "9", "m")
	client.do("$1"+respCRLF+"8"+respCRLF, "ZSCORE", "z", "m")

	client.do("-"+respErrSyntax+respCRLF, "ZADD", "z", "NX", "XX", "1", "m")
	client.do("-"+respErrSyntax+respCRLF, "ZADD", "z", "GT", "LT", "1", "m")
	client.do("-"+errRESPNoIncrOption.Error()+respCRLF, "ZADD", "z", "INCR", "1", "m")
	client.do("-"+errRESPNotFloat.Error()+respCRLF, "ZADD", "z", "abc", "m")
	client.do("-ERR wrong number of arguments for 'zadd' command"+respCRLF, "ZADD", "z", "NX", "1")
}

// TestRESPSortedSetOrdersTiesByMember covers the tie rule: equal scores order
// lexicographically by member name.
func TestRESPSortedSetOrdersTiesByMember(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":3"+respCRLF, "ZADD", "z", "1", "c", "1", "a", "1", "b")
	client.do("*3"+respCRLF+"$1"+respCRLF+"a"+respCRLF+"$1"+respCRLF+"b"+respCRLF+"$1"+respCRLF+"c"+respCRLF,
		"ZRANGE", "z", "0", "-1")
}

func TestRESPWrongTypeAcrossCollections(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "str", "v")
	client.do(":1"+respCRLF, "HSET", "hash", "f", "v")
	client.do(":1"+respCRLF, "RPUSH", "list", "v")
	client.do(":1"+respCRLF, "SADD", "set", "v")
	client.do(":1"+respCRLF, "ZADD", "zset", "1", "v")

	wrongType := "-" + respErrWrongType + respCRLF
	for _, args := range [][]string{
		{"HGET", "str", "f"},
		{"HSET", "list", "f", "v"},
		{"LPUSH", "hash", "v"},
		{"LRANGE", "set", "0", "-1"},
		{"SADD", "zset", "v"},
		{"SMEMBERS", "list"},
		{"ZADD", "hash", "1", "v"},
		{"ZRANGE", "str", "0", "-1"},
		{"GET", "hash"},
		{"APPEND", "list", "v"},
		{"INCR", "set"},
	} {
		client.do(wrongType, args...)
	}
}

// TestRESPCopyClonesCollections guards the aliasing trap: a copied collection must not
// share its container with the original.
func TestRESPCopyClonesCollections(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":2"+respCRLF, "SADD", "src", "a", "b")
	client.do(":1"+respCRLF, "COPY", "src", "dst")
	client.do(":1"+respCRLF, "SADD", "dst", "c")
	client.do(":2"+respCRLF, "SCARD", "src")
	client.do(":3"+respCRLF, "SCARD", "dst")

	client.do(":2"+respCRLF, "RPUSH", "srclist", "a", "b")
	client.do(":1"+respCRLF, "COPY", "srclist", "dstlist")
	client.do(":3"+respCRLF, "RPUSH", "dstlist", "c")
	client.do(":2"+respCRLF, "LLEN", "srclist")
}

// TestRESPCollectionsKeepExpiry checks that mutating a collection does not clear the TTL the
// key was given.
func TestRESPCollectionsKeepExpiry(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":1"+respCRLF, "HSET", "h", "a", "1")
	client.do(":1"+respCRLF, "EXPIRE", "h", "100")
	client.do(":1"+respCRLF, "HSET", "h", "b", "2")
	client.do(":100"+respCRLF, "TTL", "h")

	client.do(":1"+respCRLF, "RPUSH", "l", "a")
	client.do(":1"+respCRLF, "EXPIRE", "l", "100")
	client.do(":2"+respCRLF, "RPUSH", "l", "b")
	client.do(":100"+respCRLF, "TTL", "l")
}

// TestRESPCollectionScansPage covers paging for HSCAN, SSCAN, and ZSCAN: every call returns a
// bounded page, and the walk as a whole reports each element exactly once with its value.
func TestRESPCollectionScansPage(t *testing.T) {
	const elements = 45

	tests := []struct {
		command    string
		seed       func(client *respClient, i int)
		perElement int
	}{
		{
			command:    "HSCAN",
			seed:       func(c *respClient, i int) { c.do(":1"+respCRLF, "HSET", "coll", "e"+itoa(i), itoa(i)) },
			perElement: 2,
		},
		{
			command:    "SSCAN",
			seed:       func(c *respClient, i int) { c.do(":1"+respCRLF, "SADD", "coll", "e"+itoa(i)) },
			perElement: 1,
		},
		{
			command:    "ZSCAN",
			seed:       func(c *respClient, i int) { c.do(":1"+respCRLF, "ZADD", "coll", itoa(i), "e"+itoa(i)) },
			perElement: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			client := newRESPClient(t, kvs.NewStore())
			for i := range elements {
				tt.seed(client, i)
			}

			seen := map[string]int{}
			values := map[string]string{}
			cursor := "0"
			rounds := 0

			for {
				client.send(tt.command, "coll", cursor, "COUNT", "10")
				client.expect("*2" + respCRLF)
				cursor = client.readBulk()
				page := client.readStringArray()

				if len(page) > 10*tt.perElement {
					t.Fatalf("page carried %d values, want COUNT to bound it", len(page))
				}
				for i := 0; i < len(page); i += tt.perElement {
					seen[page[i]]++
					if tt.perElement == 2 {
						values[page[i]] = page[i+1]
					}
				}

				rounds++
				if cursor == "0" {
					break
				}
				if rounds > elements {
					t.Fatalf("%s did not terminate", tt.command)
				}
			}

			if rounds < 2 {
				t.Fatalf("%s finished in %d round(s), want the walk to be paged", tt.command, rounds)
			}
			if len(seen) != elements {
				t.Fatalf("%s saw %d distinct elements, want %d", tt.command, len(seen), elements)
			}
			for name, count := range seen {
				if count != 1 {
					t.Fatalf("%s returned %q %d times, want once", tt.command, name, count)
				}
			}

			// A hash and a sorted set carry a value alongside each element.
			for name, value := range values {
				if want := name[1:]; value != want {
					t.Fatalf("%s value for %q = %q, want %q", tt.command, name, value, want)
				}
			}
		})
	}
}

// TestRESPCollectionScanKeepsSeenElementsWhenEarlierOnesVanish is why a collection cursor is an
// element name too: removing fields the walk already passed must not shift the remainder.
func TestRESPCollectionScanKeepsSeenElementsWhenEarlierOnesVanish(t *testing.T) {
	const fields = 30

	client := newRESPClient(t, kvs.NewStore())
	for i := range fields {
		client.do(":1"+respCRLF, "HSET", "h", "e"+itoa(i), itoa(i))
	}

	client.send("HSCAN", "h", "0", "COUNT", "10")
	client.expect("*2" + respCRLF)
	cursor := client.readBulk()
	first := client.readStringArray()
	if cursor == "0" {
		t.Fatal("HSCAN finished in one round, want more pages to remain")
	}

	seen := make(map[string]struct{}, fields)
	for i := 0; i < len(first); i += 2 {
		seen[first[i]] = struct{}{}
		client.do(":1"+respCRLF, "HDEL", "h", first[i])
	}

	for cursor != "0" {
		client.send("HSCAN", "h", cursor, "COUNT", "10")
		client.expect("*2" + respCRLF)
		cursor = client.readBulk()

		page := client.readStringArray()
		for i := 0; i < len(page); i += 2 {
			seen[page[i]] = struct{}{}
		}
	}

	if len(seen) != fields {
		t.Fatalf("the iteration saw %d of %d fields, want none skipped", len(seen), fields)
	}
}
