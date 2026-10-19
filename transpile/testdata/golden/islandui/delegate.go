// Package islandui is a JSX target for the island golden cases. It implements
// the JSX contract by delegating the primitives to github.com/vuka-lang/ui and
// adds the island API (island.go) that an island tag lowers to, so the cases
// transpile and run without perturbing the other goldens' ui version.
package islandui

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"

	ui "github.com/vuka-lang/ui"
)

// VukaJSX marks this package as a JSX target implementing version 2.
const VukaJSX = 2

type (
	Node = ui.Node
	Attr = ui.Attr
)

func El(tag string, attrs []Attr, children ...Node) Node { return ui.El(tag, attrs, children...) }
func Child(v any) Node                                   { return ui.Child(v) }
func Fragment(children ...Node) Node                     { return ui.Fragment(children...) }
func Try(n Node, err error) Node                         { return ui.Try(n, err) }
func Each[T any](items []T, fn func(T) Node) Node        { return ui.Each(items, fn) }
func If(cond bool, n Node) Node                          { return ui.If(cond, n) }

func String(ctx context.Context, n Node) (string, error) { return ui.String(ctx, n) }

// Markup is ui's template, an island in a hole hosted with the hole's
// place as its site: <file>:<line>#<hole>.
type Markup struct {
	m    *ui.Markup
	site string
}

func Template(src string) *Markup {
	_, file, line, _ := runtime.Caller(1)
	return &Markup{ui.Template(src), filepath.Base(file) + ":" + strconv.Itoa(line)}
}

func (t *Markup) Render(args ...any) Node {
	for i, a := range args {
		site := t.site + "#" + strconv.Itoa(i)
		switch a := a.(type) {
		case *mount:
			args[i] = mountIsland(site, a.props, a.opts...)
		default:
			if tp := reflect.TypeOf(a); tp != nil && lookupIsland(tp) != nil {
				args[i] = mountIsland(site, a)
			}
		}
	}
	return t.m.Render(args...)
}
