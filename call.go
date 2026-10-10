package vuka

import (
	"context"
	"fmt"
	"reflect"
)

// Decorator wraps a call of any function: it runs code around c.Next(), may
// change c.Args before it, read or replace c.Results after it, call it again
// (retries) or not at all (caches). Written once, it decorates any function or
// method:
//
//	func logged(c *vuka.Call) {
//		fmt.Println("calling", c.Name, c.Args)
//		c.Next()
//	}
//
// A decorator with parameters is a function returning one; it runs once per
// decorated function, so state it keeps isn't shared between functions.
type Decorator func(c *Call)

// Func describes a decorated function. Vuka generates one per function.
type Func struct {
	Name  string // "main.charge", "Store.Save"
	Attrs []any  // the declaration's typed attributes
	// ParamAttrs are each parameter's typed attributes, receiver excluded:
	// func Show(@Path("id") id int) has [][]any{{Path("id")}}. Nil when no
	// parameter has any.
	ParamAttrs [][]any
	ErrIndex   int // the trailing error result's index, or -1
	CtxIndex   int // the context.Context argument's index, or -1
	Zero       func() []any
}

// Call is one call of a decorated function, passed down its decorators.
type Call struct {
	Name     string
	Receiver any   // the receiver, for a method
	Args     []any // the arguments; a variadic parameter is one slice
	Results  []any // the results, once Next has run (or Return was called)

	fn     *Func
	chain  []Decorator
	pos    int
	invoke func(c *Call) []any
}

// NewCall starts a call through chain, ending in invoke. Generated code calls it.
func NewCall(fn *Func, chain []Decorator, recv any, args []any, invoke func(c *Call) []any) *Call {
	return &Call{Name: fn.Name, Receiver: recv, Args: args, fn: fn, chain: chain, invoke: invoke}
}

// Run runs the chain and checks what it left in Results. Generated code calls it.
func (c *Call) Run() {
	c.Next()
	if want := len(c.fn.Zero()); len(c.Results) != want {
		panic(fmt.Sprintf("vuka: a decorator of %s left %d results; it returns %d", c.Name, len(c.Results), want))
	}
}

// Next runs the rest of the chain: the next decorator, or the function itself.
// It can be called more than once.
func (c *Call) Next() {
	i := c.pos
	if i == len(c.chain) {
		c.Results = c.invoke(c)
		return
	}
	c.pos = i + 1
	c.chain[i](c)
	c.pos = i
}

// Return sets the results without running the function; the rest of the chain
// is skipped unless Next is called.
func (c *Call) Return(results ...any) { c.Results = results }

// Err is the function's error result, or nil.
func (c *Call) Err() error {
	if i := c.fn.ErrIndex; i >= 0 && i < len(c.Results) {
		err, _ := c.Results[i].(error)
		return err
	}
	return nil
}

// SetErr replaces the error result. Before Next, it makes the call fail with
// zero values and err.
func (c *Call) SetErr(err error) {
	i := c.fn.ErrIndex
	if i < 0 {
		panic(fmt.Sprintf("vuka: %s returns no error", c.Name))
	}
	if len(c.Results) == 0 {
		c.Results = c.fn.Zero()
	}
	c.Results[i] = err
}

// Context is the call's context.Context argument, or context.Background().
func (c *Call) Context() context.Context {
	if i := c.fn.CtxIndex; i >= 0 {
		if ctx, ok := c.Args[i].(context.Context); ok {
			return ctx
		}
	}
	return context.Background()
}

// Attr fills ptr with the declaration's typed attribute of ptr's element type,
// reporting whether there is one: var r Route; c.Attr(&r).
func (c *Call) Attr(ptr any) bool { return fillAttr(c.fn.Attrs, ptr) }

// ParamAttr fills ptr with parameter i's typed attribute of ptr's element
// type, reporting whether there is one. i indexes c.Args:
//
//	func Create(@Valid in NewPet) error
//
//	for i := range c.Args {
//		var v Valid
//		if c.ParamAttr(i, &v) { … }
//	}
func (c *Call) ParamAttr(i int, ptr any) bool {
	if i < 0 || i >= len(c.fn.ParamAttrs) {
		fillAttr(nil, ptr)
		return false
	}
	return fillAttr(c.fn.ParamAttrs[i], ptr)
}

// Arg is argument i as a T.
func Arg[T any](c *Call, i int) T { return As[T](c.Args[i]) }

// AttrOf is the declaration's typed attribute of type T.
func AttrOf[T any](c *Call) (T, bool) {
	var v T
	ok := c.Attr(&v)
	return v, ok
}

// As converts a value held in an any back to T; nil is T's zero value. It
// panics, naming both types, when a decorator put something else there.
func As[T any](v any) T {
	if v == nil {
		var zero T
		return zero
	}
	t, ok := v.(T)
	if !ok {
		var zero T
		panic(fmt.Sprintf("vuka: a decorator replaced a %T with a %T", zero, v))
	}
	return t
}

func fillAttr(attrs []any, ptr any) bool {
	dst := reflect.ValueOf(ptr)
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		panic("vuka: Attr needs a non-nil pointer")
	}
	want := dst.Elem().Type()
	for _, a := range attrs {
		if v := reflect.ValueOf(a); v.IsValid() && v.Type() == want {
			dst.Elem().Set(v)
			return true
		}
	}
	return false
}

// Type describes a decorated type, for a type decorator:
//
//	func Model(t *vuka.Type) { registry[t.Name] = t.Reflect }
//
// For a struct, New is a constructor taking each dependency field as a
// parameter (or the type's own static New, when it declares one), which is the
// shape dependency-injection containers take:
//
//	func Component(t *vuka.Type) { providers = append(providers, t.New) }
type Type struct {
	Name    string // "main.User"
	Reflect reflect.Type
	Attrs   []any   // the declaration's typed attributes
	New     any     // the constructor, for a struct; nil otherwise
	Fields  []Field // the struct's fields
}

// Field is a field of a struct.
type Field struct {
	Name     string
	Type     reflect.Type
	Tag      reflect.StructTag
	Attrs    []any // the field's typed attributes
	Injected bool  // a parameter of New: not blank, not tagged inject:"-"
}

// Attr fills ptr with the type's typed attribute of ptr's element type.
func (t *Type) Attr(ptr any) bool { return fillAttr(t.Attrs, ptr) }

// TypeOf describes T for its decorators. Generated code calls it.
func TypeOf[T any](name string, attrs ...any) *Type {
	return &Type{Name: name, Reflect: reflect.TypeOf((*T)(nil)).Elem(), Attrs: attrs}
}

// TypeWith describes a struct T with its constructor. Generated code calls it.
func TypeWith[T any](name string, ctor any, attrs ...any) *Type {
	t := TypeOf[T](name, attrs...)
	t.New = ctor
	t.Fields = fieldsOf(t.Reflect)
	return t
}
