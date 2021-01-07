package rbt

import "testing"

func Test_CompareString(t *testing.T) {
	if r := CompareString("a", "b"); r != -1 {
		t.Error("Invalid comparison")
	}
	if r := CompareString("b", "a"); r != 1 {
		t.Error("Invalid comparison")
	}
	if r := CompareString("a", "a"); r != 0 {
		t.Error("Invalid comparison")
	}
}

func Test_CompareInt(t *testing.T) {
	if r := CompareInt(1, 2); r != -1 {
		t.Error("Invalid comparison")
	}
	if r := CompareInt(2, 1); r != 1 {
		t.Error("Invalid comparison")
	}
	if r := CompareInt(1, 1); r != 0 {
		t.Error("Invalid comparison")
	}
}

func Test_CompareFloat64(t *testing.T) {
	if r := CompareFloat64(1.0, 2.0); r != -1 {
		t.Error("Invalid comparison")
	}
	if r := CompareFloat64(2.0, 1.0); r != 1 {
		t.Error("Invalid comparison")
	}
	if r := CompareFloat64(1.0, 1.0); r != 0 {
		t.Error("Invalid comparison")
	}
}
