// Package rbt implements the Red-Black tree
package rbt

import "errors"

var (
	// ErrNoCompareKey There is no comparator to compare keys
	ErrNoCompareKey = errors.New("no compare function for key")
)

// RBTree A structure of red-black tree
type RBTree struct {
	compareKey Compare
	root       *RBNode
}

// New Create a RBTree instance
func New(compareKey Compare) (*RBTree, error) {
	if compareKey == nil {
		return nil, ErrNoCompareKey
	}
	t := &RBTree{
		compareKey: compareKey,
		root:       nil,
	}
	return t, nil
}

// Empty Whether the tree is empty
func (t *RBTree) Empty() bool {
	return false
}

// Size Get number of elements in the tree
func (t *RBTree) Size() uint {
	return 0
}

// Clear Reset all elements in the tree
func (t *RBTree) Clear() error {
	return nil
}

// Put Set a value of the key to the tree
func (t *RBTree) Put(key, value interface{}) error {
	return nil
}

// Get Get a value of the key from the tree
func (t *RBTree) Get(key interface{}) (interface{}, error) {
	return nil, nil
}

// Remove Remove the key in the tree
func (t *RBTree) Remove(key interface{}) error {
	return nil
}

// RBNode A structure of red-black tree node
type RBNode struct {
	Key         interface{}
	Value       interface{}
	IsRed       bool
	Parent      *RBNode
	Left, Right *RBNode
}

func (n *RBNode) getGrandparent() *RBNode {
	if n.Parent != nil {
		return n.Parent.Parent
	}
	return nil
}

func (n *RBNode) getUncle() *RBNode {
	gp := n.getGrandparent()
	if gp == nil {
		return nil
	}
	if gp.Left == n.Parent {
		return gp.Right
	}
	return gp.Left
}

func (n *RBNode) getSibling() *RBNode {
	if n.Parent == nil {
		return nil
	}
	if n == n.Parent.Left {
		return n.Parent.Right
	}
	return n.Parent.Left
}

func (n *RBNode) rotateLeft() {
	child, parent := n.Right, n.Parent

	if child.Left != nil {
		child.Left.Parent = n
	}
	n.Right = child.Left
	n.Parent = child
	child.Left = n
	child.Parent = parent
	if parent != nil {
		if parent.Left == n {
			parent.Left = child
		} else {
			parent.Right = child
		}
	}
}

func (n *RBNode) rotateRight() {
	child, parent := n.Left, n.Parent

	if child.Right != nil {
		child.Right.Parent = n
	}
	n.Left = child.Right
	n.Parent = child
	child.Right = n
	child.Parent = parent
	if parent != nil {
		if parent.Right == n {
			parent.Right = child
		} else {
			parent.Left = child
		}
	}
}
