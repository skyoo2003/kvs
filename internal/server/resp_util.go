package server

import (
	"crypto/rand"
	"encoding/binary"
	"strconv"
	"strings"
)

// respUpper normalizes a command or option name for comparison.
func respUpper(arg []byte) string {
	return strings.ToUpper(string(arg))
}

// respRandomUint64 draws a number the protocol needs to be arbitrary. RANDOMKEY only wants an
// index, but a scan handle also has to be one a client cannot guess and then present as its
// own, so both draw from the strong source rather than leave a weak one in the package.
func respRandomUint64() uint64 {
	var buf [8]byte
	// crypto/rand.Read fills the buffer or panics; it does not report a short read.
	_, _ = rand.Read(buf[:])

	return binary.NativeEndian.Uint64(buf[:])
}

func boolToInt(value bool) int64 {
	if value {
		return 1
	}

	return 0
}

// addOverflows reports whether a+b leaves the int64 range.
func addOverflows(a, b int64) bool {
	sum := a + b

	return (b > 0 && sum < a) || (b < 0 && sum > a)
}

// respFormatFloat renders a float the way Redis does: the shortest form that reads back
// as the same number, with no exponent and no trailing zeros.
func respFormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// respRange resolves an inclusive Redis index range against a container of size elements
// into a half-open Go range. Negative indices count back from the end, out of range
// bounds are clamped, and an empty result reports false.
func respRange(start, end, size int) (from, to int, ok bool) {
	if size == 0 {
		return 0, 0, false
	}

	if start < 0 {
		start += size
	}
	if end < 0 {
		end += size
	}

	start = max(start, 0)
	end = min(end, size-1)
	if start > end {
		return 0, 0, false
	}

	return start, end + 1, true
}

// respGlobMatch reports whether name matches a Redis style glob: * for any run of bytes,
// ? for one byte, [...] classes with ranges and ^ negation, and \ to escape.
//
// The standard library path.Match is nearly this, but its * refuses to cross a '/', which
// would silently hide any key containing a slash from KEYS and SCAN.
func respGlobMatch(pattern, name string) bool {
	return globMatch([]byte(pattern), []byte(name))
}

// globMatch walks pattern and name together, remembering only the most recent star. When a
// later element fails to match, the walk hands that star one more byte and resumes from there.
//
// Recursing at every star instead would be shorter but exponential: "a*a*a*a*a*a*a*a*b"
// against a run of a's takes minutes, and KEYS runs the pattern against every key while
// holding the store's read lock. One remembered star is enough, because giving the earliest
// star the fewest bytes it can take never rules out a match, and it bounds the work at
// O(len(pattern) * len(name)).
func globMatch(pattern, name []byte) bool {
	at, seen := 0, 0
	starAt, starSeen := -1, 0

	for seen < len(name) {
		if at < len(pattern) && pattern[at] == '*' {
			starAt, starSeen = at, seen
			at++

			continue
		}
		if at < len(pattern) {
			if hit, next := matchGlobOne(pattern, at, name[seen]); hit {
				at, seen = next, seen+1

				continue
			}
		}
		if starAt < 0 {
			return false
		}

		// Let the remembered star swallow one more byte and retry what follows it.
		starSeen++
		at, seen = starAt+1, starSeen
	}

	// Trailing stars match the empty remainder of name.
	for at < len(pattern) && pattern[at] == '*' {
		at++
	}

	return at == len(pattern)
}

// matchGlobOne matches one pattern element, which is an escape, a character class, a ? or a
// literal byte, against ch and reports the index just past that element.
func matchGlobOne(pattern []byte, at int, ch byte) (hit bool, next int) {
	switch {
	case pattern[at] == '?':
		return true, at + 1
	case pattern[at] == '[':
		return matchGlobClass(pattern, at, ch)
	case pattern[at] == '\\' && at+1 < len(pattern):
		return pattern[at+1] == ch, at + 2
	default:
		return pattern[at] == ch, at + 1
	}
}

// matchGlobClass matches ch against the character class starting at pattern[at], returning the
// index just past the class.
func matchGlobClass(pattern []byte, at int, ch byte) (hit bool, next int) {
	i := at + 1
	negate := false
	if i < len(pattern) && pattern[i] == '^' {
		negate, i = true, i+1
	}

	matched := false
	for i < len(pattern) && pattern[i] != ']' {
		var found bool
		found, i = matchGlobClassItem(pattern, i, ch)
		matched = matched || found
	}

	// Skip the closing bracket. An unterminated class simply ends the pattern.
	if i < len(pattern) {
		i++
	}

	return matched != negate, i
}

// matchGlobClassItem matches one item inside a character class, which is an escape, a
// range, or a single byte, and reports the index just past it.
func matchGlobClassItem(pattern []byte, i int, ch byte) (hit bool, next int) {
	switch {
	case pattern[i] == '\\' && i+1 < len(pattern):
		return pattern[i+1] == ch, i + 2
	case i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']':
		low, high := pattern[i], pattern[i+2]
		if low > high {
			low, high = high, low
		}

		return ch >= low && ch <= high, i + 3
	default:
		return pattern[i] == ch, i + 1
	}
}
