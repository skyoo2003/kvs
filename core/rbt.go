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

func (n *RBNode) rotateLeft() {
	child, parent := n.Right, n.Parent

	if child.Left != nil {
		child.Left.Parent = n
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

func (n *RBNode) rotateRight() {

}
