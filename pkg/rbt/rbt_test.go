package rbt

import "testing"

func Test_RBNode_getGrandparent(t *testing.T) {
	n1 := &RBNode{"n1", nil, false, nil, nil, nil}
	n2 := &RBNode{"n2", nil, true, n1, nil, nil}
	n1.Left = n2
	n3 := &RBNode{"n3", nil, false, n2, nil, nil}
	n2.Left = n3

	if n1.getGrandparent() != nil {
		t.Error("The return value of n1.getGrandparent() is not nil")
	}
	if n2.getGrandparent() != nil {
		t.Error("The return value of n2.getGrandparent() is not nil")
	}
	if n3.getGrandparent() != n1 {
		t.Error("The return value of n3.getGrandparent() is not equal to n1")
	}
}

func Test_RBNode_getUncle(t *testing.T) {
	n1 := &RBNode{"n1", nil, false, nil, nil, nil}
	n2 := &RBNode{"n2", nil, true, n1, nil, nil}
	n3 := &RBNode{"n3", nil, true, n1, nil, nil}
	n1.Left, n1.Right = n2, n3
	n4 := &RBNode{"n4", nil, false, n2, nil, nil}
	n2.Left = n4
	n5 := &RBNode{"n5", nil, false, n3, nil, nil}
	n3.Left = n5

	if n1.getUncle() != nil {
		t.Error("The return value of n1.getUncle() is not nil")
	}
	if n2.getUncle() != nil {
		t.Error("The return value of n2.getUncle() is not nil")
	}
	if n3.getUncle() != nil {
		t.Error("The return value of n3.getUncle() is not nil")
	}
	if n4.getUncle() != n3 {
		t.Error("The return value of n4.getUncle() is not equal to n3")
	}
	if n5.getUncle() != n2 {
		t.Error("The return value of n5.getUncle() is not equal to n2")
	}
}

func Test_RBNode_getSibling(t *testing.T) {
	n1 := &RBNode{"n1", nil, false, nil, nil, nil}
	n2 := &RBNode{"n2", nil, true, n1, nil, nil}
	n3 := &RBNode{"n3", nil, true, n1, nil, nil}
	n1.Left, n1.Right = n2, n3

	if n1.getSibling() != nil {
		t.Error("The return value of n1.getSibling() is not nil")
	}
	if n2.getSibling() != n3 {
		t.Error("The return value of n2.getSibling() is not equal to n3")
	}
	if n3.getSibling() != n2 {
		t.Error("The return value of n3.getSibling() is not equal to n2")
	}
}

func Test_RBNode_rotateLeft(t *testing.T) {
	n1 := &RBNode{"n1", nil, false, nil, nil, nil}
	n2 := &RBNode{"n2", nil, true, n1, nil, nil}
	n3 := &RBNode{"n3", nil, true, n1, nil, nil}
	n1.Left, n1.Right = n2, n3

	// n2 << n1 >> n3 -> n2 << n1 << n3
	n1.rotateLeft()

	if n1.Parent != n3 {
		t.Error("n1.Parent is not equal to n3")
	}
	if n1.Left != n2 {
		t.Error("n1.Left is not equal to n2")
	}
	if n2.Parent != n1 {
		t.Error("n2.Parent is not equal to n1")
	}
	if n3.Left != n1 {
		t.Error("n3.Left is not equal to n1")
	}
}

func Test_RBNode_rotateRight(t *testing.T) {
	n1 := &RBNode{"n1", nil, false, nil, nil, nil}
	n2 := &RBNode{"n2", nil, true, n1, nil, nil}
	n3 := &RBNode{"n3", nil, true, n1, nil, nil}
	n1.Left, n1.Right = n2, n3

	// n2 << n1 >> n3 -> n2 >> n1 >> n3
	n1.rotateRight()

	if n1.Parent != n2 {
		t.Error("n1.Parent is not equal to n2")
	}
	if n1.Right != n3 {
		t.Error("n1.Right is not equal to n3")
	}
	if n2.Right != n1 {
		t.Error("n2.Right is not equal to n1")
	}
	if n3.Parent != n1 {
		t.Error("n3.Parent is not equal to n1")
	}
}
