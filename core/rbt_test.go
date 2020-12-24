package core

import "testing"

func Test_RBNode_getGrandparent(t *testing.T) {
	n1 := &RBNode{nil, ColorBlack, nil, nil, nil}
	n2 := &RBNode{nil, ColorRed, n1, nil, nil}
	n1.Left = n2
	n3 := &RBNode{nil, ColorBlack, n2, nil, nil}
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
	n1 := &RBNode{nil, ColorBlack, nil, nil, nil}
	n2 := &RBNode{nil, ColorRed, n1, nil, nil}
	n3 := &RBNode{nil, ColorRed, n1, nil, nil}
	n1.Left, n1.Right = n2, n3
	n4 := &RBNode{nil, ColorBlack, n2, nil, nil}
	n2.Left = n4
	n5 := &RBNode{nil, ColorBlack, n3, nil, nil}
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
