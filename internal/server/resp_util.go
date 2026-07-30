package server

import (
	"strconv"
	"strings"
)

// respUpper normalizes a command or option name for comparison.
func respUpper(arg []byte) string {
	return strings.ToUpper(string(arg))
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

func globMatch(pattern, name []byte) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			return globMatchStar(pattern, name)
		case '?':
			if len(name) == 0 {
				return false
			}
			pattern, name = pattern[1:], name[1:]
		case '[':
			rest, ok := matchGlobClass(pattern, name)
			if !ok {
				return false
			}
			pattern, name = rest, name[1:]
		default:
			if pattern[0] == '\\' && len(pattern) > 1 {
				pattern = pattern[1:]
			}
			if len(name) == 0 || pattern[0] != name[0] {
				return false
			}
			pattern, name = pattern[1:], name[1:]
		}
	}

	return len(name) == 0
}

// globMatchStar matches a run of stars at the head of pattern against any prefix of name.
func globMatchStar(pattern, name []byte) bool {
	for len(pattern) > 1 && pattern[1] == '*' {
		pattern = pattern[1:]
	}
	if len(pattern) == 1 {
		return true
	}

	// Try every split point; what follows the star has to match some suffix of name.
	for i := 0; i <= len(name); i++ {
		if globMatch(pattern[1:], name[i:]) {
			return true
		}
	}

	return false
}

// matchGlobClass matches the first byte of name against the character class at the head of
// pattern, returning what is left of the pattern after the class.
func matchGlobClass(pattern, name []byte) (rest []byte, ok bool) {
	if len(name) == 0 {
		return nil, false
	}

	i := 1
	negate := false
	if i < len(pattern) && pattern[i] == '^' {
		negate, i = true, i+1
	}

	matched := false
	for i < len(pattern) && pattern[i] != ']' {
		var hit bool
		hit, i = matchGlobClassItem(pattern, i, name[0])
		matched = matched || hit
	}

	// Skip the closing bracket. An unterminated class simply ends the pattern.
	if i < len(pattern) {
		i++
	}

	return pattern[i:], matched != negate
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
