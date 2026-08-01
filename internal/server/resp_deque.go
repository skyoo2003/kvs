package server

import "slices"

// respListCompact is the amount of vacated front space that has to build up before a list
// reclaims it, so a run of head pops does not recopy on every single one.
const respListCompact = 64

// respList is a list with room to grow at both ends. items[head:] is the live range, and a
// push at the head fills reserved space in front of it instead of copying the whole list, so
// both ends cost O(1) amortized. That matters because the common use of a Redis list is a
// queue: push one end, pop the other.
type respList struct {
	items []string
	head  int
}

func newRESPList(values []string) *respList {
	return &respList{items: slices.Clone(values)}
}

// live is the range that actually holds elements. It is safe on a nil list, which is what a
// read of a missing key produces.
func (l *respList) live() []string {
	if l == nil {
		return nil
	}

	return l.items[l.head:]
}

func (l *respList) len() int {
	return len(l.live())
}

func (l *respList) at(index int) string {
	return l.live()[index]
}

func (l *respList) set(index int, value string) {
	l.live()[index] = value
}

// slice returns a copy of the half-open range, so the caller can hold it after the list
// changes.
func (l *respList) slice(from, to int) []string {
	return slices.Clone(l.live()[from:to])
}

// pushFront prepends values one at a time, so the last of them ends up first, which is the
// order LPUSH defines.
func (l *respList) pushFront(values []string) {
	l.reserveFront(len(values))

	for _, value := range values {
		l.head--
		l.items[l.head] = value
	}
}

func (l *respList) pushBack(values []string) {
	l.items = append(l.items, values...)
}

// popFront removes up to n elements from the front and returns them in the order they came
// off.
func (l *respList) popFront(n int) []string {
	n = min(n, l.len())
	taken := slices.Clone(l.items[l.head : l.head+n])

	// Blank the vacated slots so their strings can be collected.
	clear(l.items[l.head : l.head+n])
	l.head += n

	if l.head >= respListCompact && l.head > l.len() {
		l.compact()
	}

	return taken
}

// popBack removes up to n elements from the back and returns them in the order they came off,
// which is last element first.
func (l *respList) popBack(n int) []string {
	n = min(n, l.len())
	end := len(l.items)

	taken := slices.Clone(l.items[end-n:])
	slices.Reverse(taken)

	clear(l.items[end-n:])
	l.items = l.items[:end-n]

	return taken
}

// replace swaps the contents for values, which is how a whole-list rewrite such as a trim
// lands.
func (l *respList) replace(values []string) {
	l.items, l.head = slices.Clone(values), 0
}

func (l *respList) clone() *respList {
	return newRESPList(l.live())
}

// reserveFront makes room for need elements ahead of the live range. It reserves the length
// of the list on top of what was asked, so a run of head pushes amortizes to O(1) the same
// way appending at the back does.
func (l *respList) reserveFront(need int) {
	if l.head >= need {
		return
	}

	live := l.live()
	headroom := need + len(live)
	items := make([]string, headroom+len(live))
	copy(items[headroom:], live)
	l.items, l.head = items, headroom
}

// compact drops the vacated front space.
func (l *respList) compact() {
	live := l.live()
	items := make([]string, len(live), 2*len(live)+1)
	copy(items, live)
	l.items, l.head = items, 0
}
