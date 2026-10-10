package vuka

import (
	"fmt"
	"reflect"
	"slices"
)

// Bundle is a composed decorator: decorators and attributes applied as one,
// as Spring's @RestController stands for @Controller and @ResponseBody.
//
//	decorator ApiRoute(path string) = @web.Get(path) @web.Use(auth) @timed
//
// declares a function returning a Bundle; @ApiRoute("/pets") on a function
// applies each of them, in order, as if written there. Plain Go builds one
// with Compose.
type Bundle struct {
	calls []Decorator
	decls []func(*Decl)
	types []func(*Type)
	attrs []any
}

var (
	callType = reflect.TypeFor[func(*Call)]()
	declType = reflect.TypeFor[func(*Decl)]()
	typeType = reflect.TypeFor[func(*Type)]()
)

// Compose bundles decorators and attributes: each item is a call decorator
// (func(*Call)), a declarer (func(*Decl)), a type decorator (func(*Type)),
// another Bundle (its items, in place), or an attribute (any other value).
func Compose(items ...any) Bundle {
	var b Bundle
	for _, it := range items {
		switch v := it.(type) {
		case Bundle:
			b.calls = append(b.calls, v.calls...)
			b.decls = append(b.decls, v.decls...)
			b.types = append(b.types, v.types...)
			b.attrs = append(b.attrs, v.attrs...)
			continue
		case Decorator:
			b.calls = append(b.calls, v)
			continue
		}
		rv := reflect.ValueOf(it)
		switch t := rv.Type(); {
		case rv.Kind() != reflect.Func:
			b.attrs = append(b.attrs, it)
		case t.ConvertibleTo(callType):
			b.calls = append(b.calls, Decorator(rv.Convert(callType).Interface().(func(*Call))))
		case t.ConvertibleTo(declType):
			b.decls = append(b.decls, rv.Convert(declType).Interface().(func(*Decl)))
		case t.ConvertibleTo(typeType):
			b.types = append(b.types, rv.Convert(typeType).Interface().(func(*Type)))
		default:
			b.attrs = append(b.attrs, it)
		}
	}
	return b
}

// Calls are the bundle's call decorators, outermost first.
func (b Bundle) Calls() []Decorator { return slices.Clip(b.calls) }

// Decls are the bundle's declarers, in order.
func (b Bundle) Decls() []func(*Decl) { return slices.Clip(b.decls) }

// Types are the bundle's type decorators, in order.
func (b Bundle) Types() []func(*Type) { return slices.Clip(b.types) }

// Attrs are the bundle's typed attributes, in order.
func (b Bundle) Attrs() []any { return slices.Clip(b.attrs) }

// DeclareWith runs a function's bundle's declarers with d. Generated code
// calls it.
func DeclareWith(b Bundle, d *Decl) {
	if len(b.types) > 0 {
		panic(fmt.Sprintf("vuka: a decorator of %s bundles a type decorator, which takes a type", d.Name))
	}
	for _, f := range b.decls {
		f(d)
	}
}

// DecorateType runs a type's bundle's type decorators with t. Generated code
// calls it.
func DecorateType(b Bundle, t *Type) {
	if len(b.decls) > 0 {
		panic(fmt.Sprintf("vuka: a decorator of %s bundles a declarer, which takes a function", t.Name))
	}
	for _, f := range b.types {
		f(t)
	}
}
