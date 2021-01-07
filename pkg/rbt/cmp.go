package rbt // nolint

// Compare A function about comparator
type Compare func(a, b interface{}) int

// CompareString A comparison function for strings
func CompareString(a, b interface{}) int {
	sa, sb := a.(string), b.(string)
	if sa < sb {
		return -1
	} else if sa > sb {
		return 1
	}
	return 0
}

// CompareInt A comparison function for ints
func CompareInt(a, b interface{}) int {
	ia, ib := a.(int), b.(int)
	if ia < ib {
		return -1
	} else if ia > ib {
		return 1
	}
	return 0
}

// CompareFloat64 A comparison function for float64s
func CompareFloat64(a, b interface{}) int {
	fa, fb := a.(float64), b.(float64)
	if fa < fb {
		return -1
	} else if fa > fb {
		return 1
	}
	return 0
}
