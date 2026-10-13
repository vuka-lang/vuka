// Package islandui is a JSX target for the island golden cases. It implements
// the JSX contract by delegating the primitives to github.com/vuka-lang/ui and
// adds the island API (island.go) that ui.MountIsland lowering calls, so the
// cases transpile and run without perturbing the other goldens' ui version.
package islandui

import (
	"context"

	ui "github.com/vuka-lang/ui"
)

// VukaJSX marks this package as a JSX target implementing version 1.
const VukaJSX = 1

type (
	Node = ui.Node
	Attr = ui.Attr
)

func El(tag string, attrs []Attr, children ...Node) Node { return ui.El(tag, attrs, children...) }
func Text(v any) Node                                    { return ui.Text(v) }
func Child(v any) Node                                   { return ui.Child(v) }
func Fragment(children ...Node) Node                     { return ui.Fragment(children...) }
func Nodes(build func(add func(Node))) Node              { return ui.Nodes(build) }
func Try(n Node, err error) Node                         { return ui.Try(n, err) }
func F(fp uint64, shape string, kids ...Node) Node       { return ui.F(fp, shape, kids...) }

func String(ctx context.Context, n Node) (string, error) { return ui.String(ctx, n) }
