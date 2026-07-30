package server

import (
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/skyoo2003/kvs"
)

func TestRespListBothEnds(t *testing.T) {
	list := newRESPList(nil)

	// Each argument is prepended in turn, so the last ends up first.
	list.pushFront([]string{"b", "a"})
	list.pushBack([]string{"c", "d"})

	if got := list.live(); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("live() = %v, want [a b c d]", got)
	}
	if got := list.len(); got != 4 {
		t.Fatalf("len() = %d, want 4", got)
	}
	if got := list.at(0); got != "a" {
		t.Fatalf("at(0) = %q, want a", got)
	}

	list.set(1, "B")
	if got := list.at(1); got != "B" {
		t.Fatalf("at(1) = %q, want B", got)
	}
	if got := list.slice(1, 3); !slices.Equal(got, []string{"B", "c"}) {
		t.Fatalf("slice(1, 3) = %v, want [B c]", got)
	}

	if got := list.popFront(2); !slices.Equal(got, []string{"a", "B"}) {
		t.Fatalf("popFront(2) = %v, want [a B]", got)
	}
	// A back pop reports elements in the order they came off, so last element first.
	if got := list.popBack(2); !slices.Equal(got, []string{"d", "c"}) {
		t.Fatalf("popBack(2) = %v, want [d c]", got)
	}
	if got := list.len(); got != 0 {
		t.Fatalf("len() = %d, want the list drained", got)
	}

	// Popping more than there is takes what is available.
	list.pushBack([]string{"x"})
	if got := list.popFront(9); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("popFront(9) = %v, want [x]", got)
	}
}

// TestRespListReclaimsFrontSpace covers the compaction rule: a queue drained from the front
// must not keep the vacated prefix forever.
func TestRespListReclaimsFrontSpace(t *testing.T) {
	const size = 4 * respListCompact

	list := newRESPList(nil)
	for i := range size {
		list.pushBack([]string{strconv.Itoa(i)})
	}

	for range size {
		list.popFront(1)
	}

	if list.head > respListCompact {
		t.Fatalf("head = %d after draining, want the front space reclaimed", list.head)
	}
	if got := len(list.items); got > respListCompact {
		t.Fatalf("backing array = %d entries for an empty list, want it compacted", got)
	}
}

// TestRespListInterleavedEndsStayConsistent is the property check for the deque: a random mix
// of pushes and pops has to behave exactly like the naive slice it replaced.
func TestRespListInterleavedEndsStayConsistent(t *testing.T) {
	list := newRESPList(nil)
	var want []string

	for i := range 500 {
		value := strconv.Itoa(i)

		switch i % 4 {
		case 0:
			list.pushFront([]string{value})
			want = append([]string{value}, want...)
		case 1:
			list.pushBack([]string{value})
			want = append(want, value)
		case 2:
			if got := list.popFront(1); len(want) > 0 {
				if !slices.Equal(got, want[:1]) {
					t.Fatalf("step %d: popFront() = %v, want %v", i, got, want[:1])
				}
				want = want[1:]
			}
		default:
			if got := list.popBack(1); len(want) > 0 {
				if !slices.Equal(got, want[len(want)-1:]) {
					t.Fatalf("step %d: popBack() = %v, want %v", i, got, want[len(want)-1:])
				}
				want = want[:len(want)-1]
			}
		}

		if got := list.live(); !slices.Equal(got, want) {
			t.Fatalf("step %d: live() = %v, want %v", i, got, want)
		}
	}
}

// TestRespZSetCachesOrderUntilChanged covers the memoization: the order is reused while nothing
// changes and rebuilt once something does.
func TestRespZSetCachesOrderUntilChanged(t *testing.T) {
	zset := newRESPZSet()
	zset.set("b", 2)
	zset.set("a", 1)

	first := zset.sorted()
	if !slices.Equal(first, []string{"a", "b"}) {
		t.Fatalf("sorted() = %v, want [a b]", first)
	}

	// An unchanged set hands back the same slice rather than sorting again.
	if second := zset.sorted(); &second[0] != &first[0] {
		t.Fatal("sorted() rebuilt the order for an unchanged set, want the cache reused")
	}

	zset.set("c", 0)
	if got := zset.sorted(); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("sorted() after a change = %v, want [c a b]", got)
	}

	if !zset.remove("c") {
		t.Fatal("remove() = false, want the member removed")
	}
	if got := zset.sorted(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("sorted() after a removal = %v, want [a b]", got)
	}
	if zset.remove("zz") {
		t.Fatal("remove() of a missing member = true")
	}

	// reversed must not disturb the cache it reads from.
	if got := zset.reversed(); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("reversed() = %v, want [b a]", got)
	}
	if got := zset.sorted(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("sorted() after reversed() = %v, want [a b]", got)
	}
}

// TestRespZSetConcurrentRangeQueries runs range queries from several connections at once. The
// order cache is filled by readers holding only a read lock, so this is what proves the cache
// is safe to share; run under -race it fails if it is not.
func TestRespZSetConcurrentRangeQueries(t *testing.T) {
	const readers = 8

	store := kvs.NewStore()
	clients := make([]*respClient, 0, readers)
	for range readers {
		clients = append(clients, newRESPClient(t, store))
	}

	clients[0].do(":3"+respCRLF, "ZADD", "z", "2", "b", "1", "a", "3", "c")

	want := "*3" + respCRLF + "$1" + respCRLF + "a" + respCRLF +
		"$1" + respCRLF + "b" + respCRLF + "$1" + respCRLF + "c" + respCRLF

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range 50 {
				client.do(want, "ZRANGE", "z", "0", "-1")
			}
		}()
	}
	wg.Wait()
}
