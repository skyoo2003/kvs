package rbt

import (
	"errors"
	"testing"
)

func TestNewRejectsNilComparator(t *testing.T) {
	tree, err := New(nil)
	if !errors.Is(err, ErrNoCompareKey) {
		t.Fatalf("New(nil) error = %v, want %v", err, ErrNoCompareKey)
	}
	if tree != nil {
		t.Fatalf("New(nil) tree = %v, want nil", tree)
	}
}

func TestRBTreePublicAPI(t *testing.T) {
	tree, err := New(CompareInt)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	entries := rbTreeEntries()
	assertTreeStartsEmpty(t, tree)
	insertEntries(t, tree, entries)
	assertTreeTracksInsertedEntries(t, tree, entries)
	updateAndVerifyEntry(t, tree, 20, "updated", uint(len(entries)))
	removeAndVerifyEntry(t, tree, entries, 20)
	assertTreeCanBeCleared(t, tree)
}

func rbTreeEntries() []struct {
	key   int
	value string
} {
	return []struct {
		key   int
		value string
	}{
		{key: 10, value: "ten"},
		{key: 20, value: "twenty"},
		{key: 30, value: "thirty"},
		{key: 15, value: "fifteen"},
		{key: 25, value: "twenty-five"},
		{key: 5, value: "five"},
	}
}

func assertTreeStartsEmpty(t *testing.T, tree *RBTree) {
	t.Helper()

	if !tree.Empty() {
		t.Fatal("Empty() = false, want true")
	}
	if tree.Size() != 0 {
		t.Fatalf("Size() = %v, want %v", tree.Size(), 0)
	}
}

func insertEntries(t *testing.T, tree *RBTree, entries []struct {
	key   int
	value string
}) {
	t.Helper()

	for _, entry := range entries {
		err := tree.Put(entry.key, entry.value)
		if err != nil {
			t.Fatalf("Put(%v) error = %v", entry.key, err)
		}
		assertValidTree(t, tree)
	}
}

func assertTreeTracksInsertedEntries(t *testing.T, tree *RBTree, entries []struct {
	key   int
	value string
}) {
	t.Helper()

	if tree.Empty() {
		t.Fatal("Empty() = true, want false")
	}
	if tree.Size() != uint(len(entries)) {
		t.Fatalf("Size() = %v, want %v", tree.Size(), len(entries))
	}
}

func updateAndVerifyEntry(t *testing.T, tree *RBTree, key int, value string, expectedSize uint) {
	t.Helper()

	err := tree.Put(key, value)
	if err != nil {
		t.Fatalf("Put(update) error = %v", err)
	}
	if tree.Size() != expectedSize {
		t.Fatalf("Size() after update = %v, want %v", tree.Size(), expectedSize)
	}

	got, err := tree.Get(key)
	if err != nil {
		t.Fatalf("Get(%v) error = %v", key, err)
	}
	if got != value {
		t.Fatalf("Get(%v) = %v, want %v", key, got, value)
	}
}

func removeAndVerifyEntry(t *testing.T, tree *RBTree, entries []struct {
	key   int
	value string
}, removedKey int) {
	t.Helper()

	err := tree.Remove(removedKey)
	if err != nil {
		t.Fatalf("Remove(%v) error = %v", removedKey, err)
	}
	assertValidTree(t, tree)
	if tree.Size() != uint(len(entries)-1) {
		t.Fatalf("Size() after remove = %v, want %v", tree.Size(), len(entries)-1)
	}
	_, err = tree.Get(removedKey)
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get(%v) after remove error = %v, want %v", removedKey, err, ErrKeyNotFound)
	}
	for _, entry := range entries {
		if entry.key == removedKey {
			continue
		}
		got, getErr := tree.Get(entry.key)
		if getErr != nil || got != entry.value {
			t.Fatalf("Get(%v) = (%v, %v), want (%v, nil)", entry.key, got, getErr, entry.value)
		}
	}
}

func assertTreeCanBeCleared(t *testing.T, tree *RBTree) {
	t.Helper()

	err := tree.Clear()
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if !tree.Empty() {
		t.Fatal("Empty() after Clear() = false, want true")
	}
	if tree.Size() != 0 {
		t.Fatalf("Size() after Clear() = %v, want %v", tree.Size(), 0)
	}
	if tree.root != nil {
		t.Fatalf("root after Clear() = %v, want nil", tree.root)
	}
}

func TestRBTreeGetAndRemoveMissingKey(t *testing.T) {
	tree, err := New(CompareString)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := tree.Get("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get(missing) error = %v, want %v", err, ErrKeyNotFound)
	}
	if err := tree.Remove("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Remove(missing) error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestRBTreeZeroValueRequiresComparator(t *testing.T) {
	var tree RBTree

	if err := tree.Put("key", "value"); !errors.Is(err, ErrNoCompareKey) {
		t.Fatalf("Put() error = %v, want %v", err, ErrNoCompareKey)
	}
	if _, err := tree.Get("key"); !errors.Is(err, ErrNoCompareKey) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNoCompareKey)
	}
	if err := tree.Remove("key"); !errors.Is(err, ErrNoCompareKey) {
		t.Fatalf("Remove() error = %v, want %v", err, ErrNoCompareKey)
	}
	if err := tree.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
}

func TestRBTreeRemoveOnlyElement(t *testing.T) {
	tree, err := New(CompareInt)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := tree.Put(1, "one"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := tree.Remove(1); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !tree.Empty() {
		t.Fatal("Empty() after removing only element = false, want true")
	}
	if tree.Size() != 0 {
		t.Fatalf("Size() after removing only element = %v, want 0", tree.Size())
	}
	if tree.root != nil {
		t.Fatalf("root after removing only element = %v, want nil", tree.root)
	}
}

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

func assertValidTree(t *testing.T, tree *RBTree) {
	t.Helper()

	if tree.root == nil {
		if tree.Size() != 0 {
			t.Fatalf("empty tree size = %v, want 0", tree.Size())
		}
		return
	}

	if tree.root.IsRed {
		t.Fatal("root is red")
	}
	assertNode(t, tree.root, tree.compareKey)
}

func assertNode(t *testing.T, node *RBNode, compare Compare) int {
	t.Helper()

	if node == nil {
		return 1
	}

	if node.Left != nil {
		if node.Left.Parent != node {
			t.Fatalf("left child parent mismatch for key %v", node.Key)
		}
		if compare(node.Left.Key, node.Key) >= 0 {
			t.Fatalf("left child %v is not less than parent %v", node.Left.Key, node.Key)
		}
	}
	if node.Right != nil {
		if node.Right.Parent != node {
			t.Fatalf("right child parent mismatch for key %v", node.Key)
		}
		if compare(node.Right.Key, node.Key) <= 0 {
			t.Fatalf("right child %v is not greater than parent %v", node.Right.Key, node.Key)
		}
	}
	if node.IsRed {
		if isRed(node.Left) || isRed(node.Right) {
			t.Fatalf("red node %v has red child", node.Key)
		}
	}

	leftBlackHeight := assertNode(t, node.Left, compare)
	rightBlackHeight := assertNode(t, node.Right, compare)
	if leftBlackHeight != rightBlackHeight {
		t.Fatalf("black height mismatch at key %v: left=%d right=%d", node.Key, leftBlackHeight, rightBlackHeight)
	}
	if node.IsRed {
		return leftBlackHeight
	}

	return leftBlackHeight + 1
}
