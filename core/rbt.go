package core

const (
	ColorBlack = iota
	ColorRed
)

type RBTree struct {
}

type RBNode struct {
	Value       interface{}
	Color       uint8
	Parent      *RBNode
	Left, Right *RBNode
}

func (n *RBNode) getGrandparent() *RBNode {
	if n.Parent != nil {
		return n.Parent.Parent
	} else {
		return nil
	}
}

func (n *RBNode) getUncle() *RBNode {
	gp := n.getGrandparent()
	if gp == nil {
		return nil
	}
	if gp.Left == n.Parent {
		return gp.Right
	} else {
		return gp.Left
	}
}

func (n *RBNode) getSibling() *RBNode {
	if n.Parent == nil {
		return nil
	}
	if n == n.Parent.Left {
		return n.Parent.Right
	} else {
		return n.Parent.Left
	}
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
