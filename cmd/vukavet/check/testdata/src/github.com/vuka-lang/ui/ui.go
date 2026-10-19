package ui

import "github.com/a-h/templ"

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

type Markup struct{}

func Template(src string) *Markup { return &Markup{} }

func (m *Markup) Render(args ...any) Node { return nil }
