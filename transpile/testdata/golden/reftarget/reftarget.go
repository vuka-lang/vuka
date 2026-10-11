// Package reftarget is a minimal JSX target declaring Ref, for the golden
// cases checking refs without depending on a version of the ui library.
package reftarget

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

// Target is what Ref returns.
type Target string

func Ref(names ...string) Target { return Target(names[0]) }

// GetAttr is the elements with an attribute, or one of its values.
func GetAttr(name string, values ...string) Target { return Target(name) }

// Prefix is a value matcher, never checked.
func Prefix(s string) string { return s }

// Hide is a command taking a target, as a chain's would.
func Hide(t Target) func() { return func() {} }
