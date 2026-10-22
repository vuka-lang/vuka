// Package web is a stand-in for github.com/vuka-lang/web's registry: its
// decorators and the registry methods they name, which print what they
// register.
package web

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/vuka-lang/vuka"
)

// RouteOption configures a registration: anything carrying attributes.
type RouteOption interface{ Attrs() []any }

// Registry records registrations; Routes is the package's.
type Registry struct{ lines []string }

func (*Registry) vukaRegistry() {}

var Routes = &Registry{}

func (r *Registry) add(verb, path string, decl any, opts []RouteOption) *Registry {
	var parts []string
	for _, o := range opts {
		for _, a := range o.Attrs() {
			parts = append(parts, describe(a))
		}
	}
	if ref, err := vuka.FuncOf(decl); err == nil && ref.Params != nil {
		parts = append(parts, "params("+strings.Join(ref.Params, ",")+")")
	}
	r.lines = append(r.lines, strings.TrimSpace(fmt.Sprintf("%s %s %s %s", verb, path, describe(decl), strings.Join(parts, " "))))
	return r
}

func (r *Registry) Get(path string, handler any, opts ...RouteOption) *Registry {
	return r.add("GET", path, handler, opts)
}

func (r *Registry) Live(path string, page any, opts ...RouteOption) *Registry {
	return r.add("LIVE", path, page, opts)
}

func (r *Registry) Controller(prefix string, ctor any, opts ...RouteOption) *Registry {
	return r.add("CONTROLLER", prefix, ctor, opts)
}

// Print prints what was registered, in order.
func (r *Registry) Print() {
	for _, l := range r.lines {
		fmt.Println(l)
	}
}

func describe(v any) string {
	switch v.(type) {
	case Uses:
		return "use"
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Func {
		if f := runtime.FuncForPC(rv.Pointer()); f != nil {
			name := f.Name()
			return name[strings.LastIndexByte(name, '/')+1:]
		}
	}
	return fmt.Sprintf("%T", v)
}

// Uses is per-route middleware.
type Uses func(*vuka.Decl)

func (u Uses) Attrs() []any { return []any{u} }

func Use(mw ...func(string) string) Uses { return func(*vuka.Decl) {} }

// The decorators, as older generated code calls them.

func Get(path string) vuka.Bundle                    { return vuka.Compose() }
func Live(path string, opts ...any) func(*vuka.Type) { return func(*vuka.Type) {} }
func Controller(prefix string) func(*vuka.Type)      { return func(*vuka.Type) {} }
