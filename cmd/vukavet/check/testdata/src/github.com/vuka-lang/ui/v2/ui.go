package ui

import (
	"context"

	"github.com/a-h/templ"
)

type Node = templ.Component

type Live struct{}

func (l *Live) vukaLive() *Live            { return l }
func (l *Live) Subscribe(topics ...string) {}

type Stateful interface {
	Render() Node
	vukaLive() *Live
}

type Commands []string

type JSChain interface{ jsChain() Commands }

type AnyState interface{ stateOf() int }

type State[T any] struct{ v T }

func (s *State[T]) stateOf() int { return 0 }

type Attr struct {
	Name  string
	Value any
}

type Class map[string]bool

type Style map[string]string

type EventHandler struct{ Fn any }

func On(fn any) EventHandler { return EventHandler{fn} }

type Binding struct{}

type TextNode string

type ComponentNode struct{}

type Changed struct{}

type islandOpt struct{}

func (islandOpt) Attrs() []any { return nil }

func Island(args ...any) islandOpt { return islandOpt{} }

func HTML(src string, args ...any) Node { return nil }

func IslandPage(x any) Node { return nil }

type FieldOptions struct{}

type FieldAttr interface{ Apply(*FieldOptions) }

type (
	Bind  struct{ Debounce int }
	URL   string
	Defer string
)

func (Bind) Apply(*FieldOptions) {}
func (URL) Apply(*FieldOptions)  {}

type URLDefault struct {
	Name  string
	Value any
}

func (URLDefault) Apply(*FieldOptions) {}
func (u URL) Default(v any) URLDefault { return URLDefault{string(u), v} }

func URLKey(key string, c any) Node { return nil }
func Key(key any, c Stateful) Node  { return nil }
func (Defer) Apply(*FieldOptions)   {}

type LiveSchema struct{}

func (l *LiveSchema) Field(ptr any, attrs ...FieldAttr)                      {}
func (l *LiveSchema) Mount(fn func(ctx context.Context) error)               {}
func (l *LiveSchema) OnChange(fn func(ctx context.Context, c Changed) error) {}
func (l *LiveSchema) Loads(group string, fn func(ctx context.Context) error) {}

func PageTitle(ctx context.Context) string { return "" }

type Crumb struct{ Label, Href string }

func PageCrumbs(ctx context.Context) []Crumb { return nil }

type FormState struct{ st *int }

func (s *FormState) Valid() bool { return true }

type Option struct{ Value, Label string }

type PartFallback struct{ Pending, Error Node }

func Part[R, V any](recv R, m func(R, context.Context) (V, error), fallback Node, render func(V) Node) Node {
	return nil
}
