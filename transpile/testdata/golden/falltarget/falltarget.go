// Package falltarget is opttarget with ui.Fallthrough, for the cases of a
// component's tag passing on what no prop takes (forward_test.go): a
// minimal JSX target, independent of a version of the ui library.
package falltarget

import (
	"context"
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

// Attr is an attribute a component passes on to an element.
type Attr struct {
	Name  string
	Value any
}

func Sel(css string) Target { return Target(css) }

const This Target = ""

type Condition struct{}

type ValueCondition struct{}

func (Target) Value() ValueCondition { return ValueCondition{} }
func (Target) Checked() Condition    { return Condition{} }
func (Target) Attr(string) Condition { return Condition{} }

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

func (c Commands) OnError(a any) Commands   { return c.hook("on_error", a) }
func (c Commands) OnSuccess(a any) Commands { return c.hook("on_ok", a) }
func (c Commands) Before(a any) Commands    { return c.hook("before", a) }
func (c Commands) Success(a any) Commands   { return c.hook("on_ok", a) }
func (c Commands) Error(a any) Commands     { return c.hook("on_error", a) }
func (c Commands) After(a any) Commands     { return c.hook("after", a) }

// hook is the chain with a hook of the last push: a func's name, as a
// push names it, or a chain's steps.
func (c Commands) hook(name string, a any) Commands {
	if ch, ok := a.(JSChain); ok {
		return c.step(name + " [" + ch.jsChain().String() + "]")
	}
	return c.step(name + " " + stepName(a))
}

// JSChain is a chain of commands.
type JSChain interface{ jsChain() Commands }

func (c Commands) jsChain() Commands { return c }

func (c Commands) step(s string) Commands { return Commands{append(c.steps, s)} }

func (c Commands) Key(keys ...string) Commands       { return c.step("key " + strings.Join(keys, " ")) }
func (c Commands) Prevent() Commands                 { return c.step("prevent") }
func (c Commands) Stop() Commands                    { return c.step("stop") }
func (c Commands) Once() Commands                    { return c.step("once") }
func (c Commands) Optimistic() Commands              { return c.step("optimistic") }
func (c Commands) Debounce(d time.Duration) Commands { return c.step("debounce " + d.String()) }
func (c Commands) Throttle(d time.Duration) Commands { return c.step("throttle " + d.String()) }
func (c Commands) Then(ch JSChain) Commands          { return Commands{append(c.steps, ch.jsChain().steps...)} }
func (c Commands) Show(t Target) Commands            { return c.step("show " + string(t)) }
func (c Commands) Reload(region ...string) Commands {
	return c.step(strings.TrimSpace("reload " + strings.Join(region, " ")))
}
func (c Commands) Remove(t Target) Commands { return c.step("remove " + string(t)) }

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

// Action is what an event does: a chain, a method or a closure.
type Action interface{}

// ActionOf is what an event does with a T: a method or a closure taking one.
type ActionOf[T any] interface{}

// CallOption is how a component a Call opens shows.
type CallOption func()

// Call opens a component, or calls a script's function by name: the step
// names the component's type, and whether a RegisterCall registered it.
func (c Commands) Call(target any, args ...any) Commands {
	if name, ok := target.(string); ok {
		return c.step("call " + name)
	}
	t := reflect.TypeOf(target)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.String()
	if !calls[t] {
		name += " (not registered)"
	}
	return c.step("open " + name)
}

func (c Commands) Closed(a any) Commands { return c.hook("closed", a) }
func (c Commands) Close() Commands       { return c.step("close") }

var calls = map[reflect.Type]bool{}

// RegisterCall records a component's type, as ui's does.
func RegisterCall(component any) {
	t := reflect.TypeOf(component)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	calls[t] = true
}

// OmitZero is v, or nil when v is zero.
func OmitZero[T any](v T) any {
	if reflect.ValueOf(&v).Elem().IsZero() {
		return nil
	}
	return v
}

// Fallthrough is what a component's tag passes on to its forward element.
type Fallthrough []Attr

// Merge is the forward element's class and the caller's, joined.
func (f Fallthrough) Merge(own ...any) any {
	var b strings.Builder
	for _, p := range own {
		b.WriteString(fmt.Sprint(p))
	}
	return MergeClasses(b.String(), f.Class())
}

// Class is the caller's classes.
func (f Fallthrough) Class() string {
	var cs []string
	for _, a := range f {
		if a.Name == "class" && a.Value != nil {
			cs = append(cs, fmt.Sprint(a.Value))
		}
	}
	return strings.Join(cs, " ")
}

// Attrs are the caller's attributes but its class.
func (f Fallthrough) Attrs() []Attr {
	var out []Attr
	for _, a := range f {
		if set, ok := a.Value.([]Attr); ok {
			out = append(out, set...)
		} else if a.Name != "class" {
			out = append(out, a)
		}
	}
	return out
}

// All is Attrs and the class.
func (f Fallthrough) All() []Attr { return append(f.Attrs(), Attr{Name: "class", Value: f.Class()}) }

// Or is v, or nil when the caller sets name.
func (f Fallthrough) Or(name string, v any) any {
	for _, a := range f.Attrs() {
		if a.Name == name {
			return nil
		}
	}
	return v
}

// MergeClasses joins class lists.
func MergeClasses(classes ...string) string {
	return strings.Join(strings.Fields(strings.Join(classes, " ")), " ")
}

// BindValue is an element's two-way binding of a field.
func BindValue(owner, field any, pace ...JSChain) []Attr {
	return []Attr{{Name: "value", Value: reflect.ValueOf(field).Elem().Interface()}, {Name: "data-vk-bind", Value: "value"}}
}

// AutoID is a component's id prop.
type AutoID string

// NewAutoID is the id of the tag calling it; here always "auto".
func NewAutoID(ctx context.Context) AutoID { return "auto" }

// Ctx is a node built in the context it renders in.
func Ctx(build func(ctx context.Context) Node) Node { return build(context.Background()) }

// SlotOr is n, or fallback when n is nil.
func SlotOr(n, fallback Node) Node {
	if n == nil {
		return fallback
	}
	return n
}

// ClassMerge is a caller's classes, merged into a class's text before it.
type ClassMerge string

// Classes is the caller's classes for the forward element's written class.
func (f Fallthrough) Classes() ClassMerge { return ClassMerge(f.Class()) }
