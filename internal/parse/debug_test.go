package parse

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	ts "github.com/dcosson/treesitter-go"
)

func TestDebugASTStructure(t *testing.T) {
	if os.Getenv("DCG_DEBUG_AST") != "1" {
		t.Skip("set DCG_DEBUG_AST=1 to run debug AST dump")
	}

	parser := NewBashParser()
	inputs := []string{
		"ls",
		"/usr/bin/git push --force origin main",
		"cat file | grep foo",
		"echo ok && rm -rf /tmp/a",
		"DIR=/tmp; rm -rf $DIR",
		"RAILS_ENV=production rails db:reset",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			tree, _ := parser.Parse(context.Background(), input)
			if tree == nil {
				t.Fatal("nil tree")
			}
			root := tree.RootNode()
			var buf strings.Builder
			dumpNode(&buf, root, input, 0)
			fmt.Println(buf.String())
		})
	}
}

func dumpNode(buf *strings.Builder, node ts.Node, source string, depth int) {
	indent := strings.Repeat("  ", depth)
	text := nodeText(source, node)
	named := ""
	if !node.IsNamed() {
		named = " (anon)"
	}
	fmt.Fprintf(buf, "%s%s [%d-%d]%s %q\n", indent, node.Type(), node.StartByte(), node.EndByte(), named, text)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		dumpNode(buf, child, source, depth+1)
	}
}
