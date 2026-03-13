// Package rbt implements the Red-Black tree
package rbt

import "errors"

var (
	// ErrNoCompareKey There is no comparator to compare keys
	ErrNoCompareKey = errors.New("no compare function for key")
	// ErrKeyNotFound is returned when a key does not exist in the tree.
	ErrKeyNotFound = errors.New("key not found")
)

// RBTree A structure of red-black tree
type RBTree struct {
	compareKey Compare
	root       *RBNode
	size       uint
}

// New Create a RBTree instance
func New(compareKey Compare) (*RBTree, error) {
	if compareKey == nil {
		return nil, ErrNoCompareKey
	}
	t := &RBTree{
		compareKey: compareKey,
		root:       nil,
		size:       0,
	}
	return t, nil
}

// Empty Whether the tree is empty
func (t *RBTree) Empty() bool {
	return t == nil || t.size == 0
}

// Size Get number of elements in the tree
func (t *RBTree) Size() uint {
	if t == nil {
		return 0
	}

	return t.size
}

// Clear Reset all elements in the tree
func (t *RBTree) Clear() error {
	if t == nil {
		return nil
	}

	t.root = nil
	t.size = 0

	return nil
}

// Put Set a value of the key to the tree
func (t *RBTree) Put(key, value interface{}) error {
	if err := t.requireComparator(); err != nil {
		return err
	}

	if t.root == nil {
		t.root = &RBNode{Key: key, Value: value}
		t.size = 1
		return nil
	}

	parent := t.root
	current := t.root
	cmp := 0
	for current != nil {
		parent = current
		cmp = t.compareKey(key, current.Key)
		switch {
		case cmp < 0:
			current = current.Left
		case cmp > 0:
			current = current.Right
		default:
			current.Value = value
			return nil
		}
	}

	node := &RBNode{Key: key, Value: value, IsRed: true, Parent: parent}
	if cmp < 0 {
		parent.Left = node
	} else {
		parent.Right = node
	}

	t.insertFix(node)
	t.size++

	return nil
}

// Get Get a value of the key from the tree
func (t *RBTree) Get(key interface{}) (interface{}, error) {
	if err := t.requireComparator(); err != nil {
		return nil, err
	}

	node := t.findNode(key)
	if node == nil {
		return nil, ErrKeyNotFound
	}

	return node.Value, nil
}

// Remove Remove the key in the tree
func (t *RBTree) Remove(key interface{}) error {
	if err := t.requireComparator(); err != nil {
		return err
	}

	if t.findNode(key) == nil {
		return ErrKeyNotFound
	}

	entries := t.entriesExcept(key)
	t.root = nil
	t.size = 0
	for _, entry := range entries {
		if err := t.Put(entry.key, entry.value); err != nil {
			return err
		}
	}

	return nil
}

type treeEntry struct {
	key   interface{}
	value interface{}
}

func (t *RBTree) findNode(key interface{}) *RBNode {
	for node := t.root; node != nil; {
		cmp := t.compareKey(key, node.Key)
		switch {
		case cmp < 0:
			node = node.Left
		case cmp > 0:
			node = node.Right
		default:
			return node
		}
	}

	return nil
}

func (t *RBTree) requireComparator() error {
	if t == nil || t.compareKey == nil {
		return ErrNoCompareKey
	}

	return nil
}

func (t *RBTree) entriesExcept(key interface{}) []treeEntry {
	entries := make([]treeEntry, 0, t.size)

	var walk func(node *RBNode)
	walk = func(node *RBNode) {
		if node == nil {
			return
		}

		walk(node.Left)
		if t.compareKey(node.Key, key) != 0 {
			entries = append(entries, treeEntry{key: node.Key, value: node.Value})
		}
		walk(node.Right)
	}

	walk(t.root)

	return entries
}

func (t *RBTree) insertFix(node *RBNode) {
	for node != t.root && node.Parent != nil && node.Parent.IsRed {
		grandparent := node.getGrandparent()
		if grandparent == nil {
			break
		}

		if node.Parent == grandparent.Left {
			uncle := grandparent.Right
			if isRed(uncle) {
				node.Parent.IsRed = false
				uncle.IsRed = false
				grandparent.IsRed = true
				node = grandparent
				continue
			}

			if node == node.Parent.Right {
				node = node.Parent
				t.rotateLeft(node)
			}

			node.Parent.IsRed = false
			grandparent.IsRed = true
			t.rotateRight(grandparent)
			continue
		}

		uncle := grandparent.Left
		if isRed(uncle) {
			node.Parent.IsRed = false
			uncle.IsRed = false
			grandparent.IsRed = true
			node = grandparent
			continue
		}

		if node == node.Parent.Left {
			node = node.Parent
			t.rotateRight(node)
		}

		node.Parent.IsRed = false
		grandparent.IsRed = true
		t.rotateLeft(grandparent)
	}

	t.root.IsRed = false
}

func (t *RBTree) rotateLeft(node *RBNode) {
	wasRoot := node.Parent == nil
	node.rotateLeft()
	if wasRoot {
		t.root = node.Parent
	}
}

func (t *RBTree) rotateRight(node *RBNode) {
	wasRoot := node.Parent == nil
	node.rotateRight()
	if wasRoot {
		t.root = node.Parent
	}
}

func isRed(node *RBNode) bool {
	return node != nil && node.IsRed
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
