// Package opttarget is a minimal JSX target declaring the Options command,
// for the golden cases registering the methods it names without depending
// on a version of the ui library. A chain renders as the methods it
// registered loading for it, so a case can print them.
package opttarget

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
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

func (c Commands) On(t Target) CommandsOn { return CommandsOn{c} }

type CommandsOn struct{ c Commands }

func (o CommandsOn) String() string { return o.c.String() }

func (o CommandsOn) Options(handler any, args ...any) CommandsOn {
	return CommandsOn{o.c.Options("", handler, args...)}
}

var registered = map[string]bool{}

// RegisterOptions records a method expression's name, as ui's does.
func RegisterOptions(m any) {
	name := runtime.FuncForPC(reflect.ValueOf(m).Pointer()).Name()
	registered[name[strings.LastIndexByte(name, '/')+1:]] = true
}
