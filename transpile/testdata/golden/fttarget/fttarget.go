// Package fttarget is a JSX target declaring FT as well as F, so the golden
// cases exercise the lazy-frame lowering (frameFT) without depending on a
// version of the ui library that has it.
package fttarget

const VukaJSX = 1

type Node interface{}

type Attr struct {
	Name  string
	Value any
}

func El(tag string, attrs []Attr, kids ...Node) Node { return nil }
func Text(v any) Node                                { return nil }
func Child(v any) Node                               { return nil }
func Fragment(kids ...Node) Node                     { return nil }
func Nodes(f func(func(Node))) Node                  { return nil }
func Try(n Node, err error) Node                     { return nil }
func On(fn any) any                                  { return fn }

// F is the eager frame; FT is the lazy one the build closure rebuilds.
func F(fp uint64, shape string, kids ...Node) Node                         { return nil }
func FT(fp uint64, shape string, build func([]any) Node, dyns ...any) Node { return nil }
