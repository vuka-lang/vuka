// Package stateui is a JSX target for the client-state golden cases. It
// implements the JSX contract by delegating to github.com/vuka-lang/ui and
// adds ui's state API in the shape the compiler lowers to; a binding renders
// as its initial value plus data-vk-b-<name>="<expression> <deps>", enough to
// see what the compiler wrote.
package stateui

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	ui "github.com/vuka-lang/ui"
)

// VukaJSX marks this package as a JSX target implementing version 1.
const VukaJSX = 1

type (
	Node = ui.Node
	Attr = ui.Attr
)

func El(tag string, attrs []Attr, children ...Node) Node {
	out := make([]Attr, 0, len(attrs))
	for _, a := range attrs {
		if b, ok := a.Value.(Binding); ok {
			out = append(out, Attr{Name: a.Name, Value: classText(b.Init)}, Attr{Name: "data-vk-b-" + strings.ToLower(a.Name), Value: b.String()})
			continue
		}
		a.Value = classText(a.Value)
		if c, ok := a.Value.(Commands); ok {
			a = Attr{Name: "data-vk-js-" + strings.ToLower(strings.TrimPrefix(a.Name, "on")), Value: c.String()}
		}
		out = append(out, a)
	}
	return ui.El(tag, out, children...)
}
func Text(v any) Node { return ui.Text(v) }
func Child(v any) Node {
	if b, ok := v.(Binding); ok {
		return ui.El("vk-t", []Attr{{Name: "data-vk-b", Value: b.String()}}, ui.Text(b.Init))
	}
	return ui.Child(v)
}
func Fragment(children ...Node) Node               { return ui.Fragment(children...) }
func Nodes(build func(add func(Node))) Node        { return ui.Nodes(build) }
func Try(n Node, err error) Node                   { return ui.Try(n, err) }
func F(fp uint64, shape string, kids ...Node) Node { return ui.F(fp, shape, kids...) }
func On(fn any) any {
	if c, ok := fn.(Commands); ok {
		return c
	}
	return ui.On(fn)
}

func String(ctx context.Context, n Node) (string, error) { return ui.String(ctx, n) }

type cell struct{ site string }

type State[T any] struct {
	c    *cell
	path []string
	v    T
	expr *Binding
}

func NewState[T any](site string, initial T) State[T] { return State[T]{c: &cell{site}, v: initial} }
func PageState[T any](site string, initial T) State[T] {
	return State[T]{c: &cell{"g:" + site}, v: initial}
}
func Derived[T any](value T) State[T] { return NewDerived(value, `{"$":0}`, value) }
func NewDerived[T any](value T, expr string, deps ...any) State[T] {
	return State[T]{v: value, expr: &Binding{value, expr, deps}}
}
func StateField[S, T any](s State[S], name string, sample T) State[T] {
	return State[T]{c: s.c, path: append(s.path[:len(s.path):len(s.path)], name), v: sample}
}

func (s State[T]) Get() T           { return s.v }
func (s State[T]) Set(v T) Commands { return JS.Set(s, v) }
func (s State[T]) Toggle() Commands { return JS.add("toggle " + s.String()) }
func (s State[T]) Inc() Commands    { return JS.add("inc " + s.String()) }
func (s State[T]) Dec() Commands    { return JS.add("dec " + s.String()) }
func (s State[T]) Reset() Commands  { return JS.Set(s, s.v) }
func (s State[T]) anyState() string { return s.String() }
func (s State[T]) String() string {
	if s.expr != nil {
		return "(" + s.expr.String() + ")"
	}
	return s.c.site + strings.Join(append([]string{""}, s.path...), ".") + "=" + fmt.Sprint(s.v)
}

type AnyState interface{ anyState() string }

type Commands struct{ ops []string }

var JS Commands

func (c Commands) add(op string) Commands { return Commands{append(c.ops[:len(c.ops):len(c.ops)], op)} }
func (c Commands) Set(s AnyState, v any) Commands {
	return c.add("set " + s.anyState() + " " + fmt.Sprint(v))
}
func (c Commands) String() string { return strings.Join(c.ops, "; ") }

type Binding struct {
	Init any
	Expr string
	Deps []any
}

func Bind(init any, expr string, deps ...any) Binding { return Binding{init, expr, deps} }

func (b Binding) String() string {
	var ds []string
	for _, d := range b.Deps {
		if reflect.TypeOf(d) != nil && reflect.TypeOf(d).Kind() == reflect.Map {
			ds = append(ds, fmt.Sprintf("%v", d))
			continue
		}
		ds = append(ds, fmt.Sprint(d))
	}
	return b.Expr + " " + strings.Join(ds, " ")
}

type Class map[string]bool

type ShowProps struct {
	When     any
	Children Node
}

func Show(p ShowProps) Node {
	return ui.El("vk-show", []Attr{{Name: "data-vk-show", Value: fmt.Sprint(p.When)}}, p.Children)
}

// classText is a Class as its attribute's text; other values as they are.
func classText(v any) any {
	c, ok := v.(Class)
	if !ok {
		return v
	}
	var on []string
	for k, v := range c {
		if v {
			on = append(on, k)
		}
	}
	sort.Strings(on)
	return strings.Join(on, " ")
}
