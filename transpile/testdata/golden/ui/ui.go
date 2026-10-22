// Package ui is a small stand-in for github.com/vuka-lang/ui/v2's LiveSchema in
// the golden cases.
package ui

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"time"
)

// Live makes a struct a live page.
type Live struct{}

func (Live) vukaSchema(interface{ LiveSchema(*LiveSchema) }) {}

type FieldOptions struct {
	Bind     bool
	Debounce time.Duration
	URL      string
	Defer    string
	Validate []string
}

type FieldAttr interface{ Apply(*FieldOptions) }

type (
	Bind  struct{ Debounce time.Duration }
	URL   string
	Defer string
)

func (b Bind) Apply(o *FieldOptions)  { o.Bind, o.Debounce = true, b.Debounce }
func (u URL) Apply(o *FieldOptions)   { o.URL = string(u) }
func (d Defer) Apply(o *FieldOptions) { o.Defer = string(d) }

// Changed names the fields a set changed.
type Changed struct{ Names []string }

// LiveSchema collects a page's field attributes and hooks.
type LiveSchema struct {
	root   reflect.Value
	fields map[string]FieldOptions
	hooks  []string
	mount  func(context.Context) error
}

func (l *LiveSchema) Field(ptr any, attrs ...FieldAttr) {
	p := reflect.ValueOf(ptr).Pointer()
	t := l.root.Elem().Type()
	for i := range t.NumField() {
		if l.root.Pointer()+t.Field(i).Offset == p && t.Field(i).Type == reflect.TypeOf(ptr).Elem() {
			o := l.fields[t.Field(i).Name]
			for _, a := range attrs {
				a.Apply(&o)
			}
			l.fields[t.Field(i).Name] = o
			return
		}
	}
	panic("not a field")
}

func (l *LiveSchema) Mount(fn func(ctx context.Context) error) {
	l.mount = fn
	l.hook("Mount", fn)
}
func (l *LiveSchema) OnChange(fn func(ctx context.Context, c Changed) error) { l.hook("OnChange", fn) }
func (l *LiveSchema) Loads(group string, fn func(ctx context.Context) error) {
	l.hook("Loads("+group+")", fn)
}

func (l *LiveSchema) hook(what string, fn any) {
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	name = strings.TrimSuffix(name[strings.LastIndexByte(name, '.')+1:], "-fm")
	l.hooks = append(l.hooks, what+"="+name)
}

// NewLiveSchema is the LiveSchema of the page x, for describe.
func NewLiveSchema(x any) *LiveSchema {
	return &LiveSchema{root: reflect.ValueOf(x), fields: map[string]FieldOptions{}}
}

// Fields, Hooks and MountHook are what the LiveSchema method declared.
func (l *LiveSchema) Fields() map[string]FieldOptions        { return l.fields }
func (l *LiveSchema) Hooks() []string                        { return l.hooks }
func (l *LiveSchema) MountHook() func(context.Context) error { return l.mount }
