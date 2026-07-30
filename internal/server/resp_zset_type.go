package server

import (
	"cmp"
	"maps"
	"slices"
	"sync/atomic"
)

// respZSet is a sorted set. Alongside the scores it caches the member order, so repeated range
// queries on a set nobody has changed do not re-sort it. Any change drops the cache, and the
// next query rebuilds it once.
//
// A read-mostly sorted set, which is what a leaderboard is, therefore sorts once rather than
// on every query. A true score-ordered index would also make the rebuild itself O(log n); this
// only removes the repeated work.
// The cache is an atomic pointer because range queries run under the store's read lock,
// where several readers may rebuild it at once. They all compute the same order from a map no
// writer can be touching, so the last store winning is harmless; what would not be safe is a
// plain field two readers write.
type respZSet struct {
	scores map[string]float64
	order  atomic.Pointer[[]string]
}

func newRESPZSet() *respZSet {
	return &respZSet{scores: make(map[string]float64)}
}

func (z *respZSet) len() int {
	if z == nil {
		return 0
	}

	return len(z.scores)
}

func (z *respZSet) score(member string) (float64, bool) {
	if z == nil {
		return 0, false
	}

	value, ok := z.scores[member]

	return value, ok
}

// members exposes the score map for callers that only need to list names. It is nil-safe, so a
// read of a missing key behaves like an empty set.
func (z *respZSet) members() map[string]float64 {
	if z == nil {
		return nil
	}

	return z.scores
}

func (z *respZSet) set(member string, score float64) {
	z.scores[member] = score
	z.order.Store(nil)
}

func (z *respZSet) remove(member string) bool {
	if _, ok := z.scores[member]; !ok {
		return false
	}

	delete(z.scores, member)
	z.order.Store(nil)

	return true
}

// sorted returns the members ordered by score, and by member name where scores tie, which is
// the order Redis defines.
func (z *respZSet) sorted() []string {
	if z == nil {
		return nil
	}
	if cached := z.order.Load(); cached != nil {
		return *cached
	}

	members := slices.Collect(maps.Keys(z.scores))
	slices.SortFunc(members, func(a, b string) int {
		if order := cmp.Compare(z.scores[a], z.scores[b]); order != 0 {
			return order
		}

		return cmp.Compare(a, b)
	})
	z.order.Store(&members)

	return members
}

// reversed returns the member order highest first. It copies, because the cached order must
// not be reversed in place.
func (z *respZSet) reversed() []string {
	members := slices.Clone(z.sorted())
	slices.Reverse(members)

	return members
}

func (z *respZSet) clone() *respZSet {
	return &respZSet{scores: maps.Clone(z.scores)}
}
