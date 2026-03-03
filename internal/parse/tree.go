package parse

import ts "github.com/dcosson/treesitter-go"

// Tree wraps tree-sitter's raw Tree type so parser internals can evolve
// without leaking dependency details across package boundaries.
type Tree struct {
	raw *ts.Tree
}

func newTree(raw *ts.Tree) *Tree {
	if raw == nil {
		return nil
	}
	return &Tree{raw: raw}
}

func (t *Tree) Raw() *ts.Tree {
	if t == nil {
		return nil
	}
	return t.raw
}

func (t *Tree) RootNode() ts.Node {
	return t.raw.RootNode()
}

func (t *Tree) HasParseError() bool {
	if t == nil || t.raw == nil {
		return true
	}
	return hasErrorNode(t.raw.RootNode())
}

func hasErrorNode(node ts.Node) bool {
	if node.Type() == "ERROR" || node.Type() == "MISSING" {
		return true
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		if hasErrorNode(node.Child(i)) {
			return true
		}
	}

	return false
}
