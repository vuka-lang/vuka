package vuka

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
)

// Live, embedded by value in a struct whose pointer has a Render() Node method,
// makes the struct a stateful component:
//
//	type Counter struct {
//		vuka.Live
//		Start int // exported fields are props, set from the tag's attributes
//		n     int // unexported fields are state
//	}
//
// Rendered without a live session (vuka.String, Handler, …) a stateful
// component mounts and renders once, as plain HTML. Under a session (package
// live) it is an instance that keeps its state between renders and answers
// the events of the elements it renders.
type Live struct{ inst LiveInstance }

// LiveInstance is a session's side of a component instance. Package live
// implements it.
type LiveInstance interface {
	ID() string
	Subscribe(topics ...string)
}

// ID is the instance's id in its session, the value of its root element's
// data-vk-id; "" when rendered without one.
func (l *Live) ID() string {
	if l.inst == nil {
		return ""
	}
	return l.inst.ID()
}

// Subscribe asks the session for the messages published on topics, delivered
// to the component's Update methods. Without a session it does nothing.
func (l *Live) Subscribe(topics ...string) {
	if l.inst != nil {
		l.inst.Subscribe(topics...)
	}
}

func (l *Live) vukaLive() *Live { return l }

// Stateful is a stateful component: a pointer to a struct embedding Live, with
// a Render method.
type Stateful interface {
	Render() Node
	vukaLive() *Live
}

// AttachLive binds a component to its session instance. Package live calls it.
func AttachLive(c Stateful, inst LiveInstance) { c.vukaLive().inst = inst }

// ComponentNode is a stateful component's tag, <Counter Start={5} />: Props is
// a fresh value carrying the attributes, never mounted itself. Site (the tag's
// place in the source), Key and the instance rendering it identify the
// instance a session keeps.
type ComponentNode struct {
	Site  string
	Key   any
	Props Stateful
}

// Component is a stateful component's tag.
func Component(site string, key any, props Stateful) Node {
	return &ComponentNode{Site: site, Key: key, Props: props}
}

func (n *ComponentNode) Render(ctx context.Context, w io.Writer) error {
	if h := liveHost(ctx); h != nil {
		return h.Component(ctx, n, w)
	}
	c, err := n.static(ctx)
	if err != nil || c == nil {
		return err
	}
	if r := c.Render(); r != nil {
		return r.Render(ctx, w)
	}
	return nil
}

// static is the instance a render without a session uses: a copy of Props,
// mounted.
func (n *ComponentNode) static(ctx context.Context) (Stateful, error) {
	if n.Props == nil {
		return nil, nil
	}
	c := CopyLive(n.Props)
	return c, MountLive(ctx, c)
}

// CopyLive is a shallow copy of c.
func CopyLive(c Stateful) Stateful {
	v := reflect.ValueOf(c)
	cp := reflect.New(v.Type().Elem())
	cp.Elem().Set(v.Elem())
	return cp.Interface().(Stateful)
}

// MountLive runs c's Mount method, if it has one: Mount(), Mount() error,
// Mount(context.Context) or Mount(context.Context) error.
func MountLive(ctx context.Context, c Stateful) error {
	switch m := c.(type) {
	case interface{ Mount(context.Context) error }:
		return m.Mount(ctx)
	case interface{ Mount(context.Context) }:
		m.Mount(ctx)
	case interface{ Mount() error }:
		return m.Mount()
	case interface{ Mount() }:
		m.Mount()
	}
	return nil
}

// EventHandler is an on… attribute's expression on an element: onClick={c.Inc}.
// Under a live session it renders as data-vk-on-<event>="<handler id>" and the
// session calls Fn when the browser reports the event; without one it renders
// nothing. A templ.ComponentScript renders as templ renders it.
type EventHandler struct{ Fn any }

// On is an on… attribute's expression.
func On(fn any) EventHandler { return EventHandler{fn} }

// EventName is the DOM event an on… attribute names: onClick is click,
// onKeyDown keydown.
func EventName(attr string) string { return strings.ToLower(strings.TrimPrefix(attr, "on")) }

// Event is what the browser reports of an event, and what a handler taking a
// vuka.Event receives. Target is the handler's id; Type the DOM event; Value
// the target element's value (for a checkbox, "true" or "false"); Key the key
// of a keyboard event; Form the fields of the form a submit event comes from.
type Event struct {
	Target string     `json:"target"`
	Type   string     `json:"event,omitempty"`
	Value  string     `json:"value,omitempty"`
	Key    string     `json:"key,omitempty"`
	Form   url.Values `json:"form,omitempty"`
}

// LiveHost renders stateful components and registers event handlers for a
// live session. Package live implements it; ctx carries it.
type LiveHost interface {
	Component(ctx context.Context, n *ComponentNode, w io.Writer) error
	Handler(ctx context.Context, fn any) string
	Keyed(ctx context.Context, key any) context.Context
}

type hostKey struct{}

// WithLiveHost is ctx carrying h.
func WithLiveHost(ctx context.Context, h LiveHost) context.Context {
	return context.WithValue(ctx, hostKey{}, h)
}

func liveHost(ctx context.Context) LiveHost {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(hostKey{}).(LiveHost)
	return h
}

// elementKey is an element's key attribute, which scopes the instances of the
// stateful components inside it.
func elementKey(e *Element) (any, bool) {
	for _, a := range e.Attrs {
		if a.Name == "key" {
			return a.Value, true
		}
	}
	return nil, false
}

// handlerAttr writes an event handler's attribute, or nothing without a session.
func (h *HTMLRenderer) handlerAttr(name string, eh EventHandler) error {
	host := liveHost(h.ctx)
	if host == nil || eh.Fn == nil {
		return nil
	}
	if reflect.TypeOf(eh.Fn).Kind() != reflect.Func {
		return fmt.Errorf("vuka: %s takes a function, not %T", name, eh.Fn)
	}
	h.write(` data-vk-on-` + EventName(name) + `="`)
	h.write(host.Handler(h.ctx, eh.Fn))
	h.write(`"`)
	return nil
}
