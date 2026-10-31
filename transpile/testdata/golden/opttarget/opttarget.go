// Package opttarget is a minimal JSX target declaring the Push and Options
// commands, for the golden cases registering the methods they name without
// depending on a version of the ui library. A chain renders as the methods
// it registered pushing to, so a case can print them.
package opttarget

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"
)

const VukaJSX = 3

type Node interface{}

func HTML(src string, args ...any) Node { return fmt.Sprint(args...) }
func Child(v any) Node                  { return v }
func Fragment(kids ...Node) Node        { return kids }
func Try(n Node, err error) Node        { return n }
func On(fn any) any                     { return fn }

type Option struct{ Value, Label string }

type OptionGroup struct {
	Label   string
	Options []Option
}

type Target string

func Sel(css string) Target { return Target(css) }

const This Target = ""

type Condition struct{}

type ValueCondition struct{}

func (Target) Value() ValueCondition { return ValueCondition{} }
func (Target) Checked() Condition    { return Condition{} }

type Commands struct{ steps []string }

func (c Commands) String() string { return strings.Join(c.steps, "; ") }

var JS Commands

func (c Commands) Options(target Target, handler any, args ...any) Commands {
	name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	name = strings.TrimSuffix(name[strings.LastIndexByte(name, '/')+1:], "-fm")
	if !registered[name] {
		name += " (not registered)"
	}
	return Commands{append(c.steps, fmt.Sprintf("options %s %d", name, len(args)))}
}

func stepName(handler any) string {
	name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	name = strings.TrimSuffix(name[strings.LastIndexByte(name, '/')+1:], "-fm")
	if !registered[name] {
		name += " (not registered)"
	}
	return name
}

type PushOption func()

func Read() PushOption                 { return func() {} }
func Cache(d time.Duration) PushOption { return func() {} }
func Value(v any) PushOption           { return func() {} }

type IntoOption func()

func Append() IntoOption  { return func() {} }
func Prepend() IntoOption { return func() {} }

func (c Commands) Push(handler any, args ...any) Commands {
	return Commands{append(c.steps, fmt.Sprintf("push %s %d", stepName(handler), len(args)))}
}

func (c Commands) Into(t Target, opts ...IntoOption) Commands {
	return Commands{append(c.steps, "into "+string(t))}
}

func (c Commands) OnError(ch Commands) Commands { return c }

// Stateful is what a live page's pointer implements.
type Stateful interface{ live() }

// Live makes a page live.
type Live struct{}

func (*Live) live() {}

func (c Commands) On(t Target) CommandsOn { return CommandsOn{c} }

type CommandsOn struct{ c Commands }

func (o CommandsOn) String() string { return o.c.String() }

func (o CommandsOn) Push(handler any, args ...any) CommandsOn {
	return CommandsOn{o.c.Push(handler, args...)}
}

func (o CommandsOn) Into(t Target, opts ...IntoOption) CommandsOn {
	return CommandsOn{o.c.Into(t, opts...)}
}

func (o CommandsOn) Options(handler any, args ...any) CommandsOn {
	return CommandsOn{o.c.Options("", handler, args...)}
}

var registered = map[string]bool{}

// RegisterPush records a method expression's name, as ui's does.
func RegisterPush(m any) { RegisterOptions(m) }

// RegisterOptions records a method expression's name, as ui's does.
func RegisterOptions(m any) {
	name := runtime.FuncForPC(reflect.ValueOf(m).Pointer()).Name()
	registered[name[strings.LastIndexByte(name, '/')+1:]] = true
}
