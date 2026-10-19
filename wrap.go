package vuka

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// FuncInfo describes a function for its call decorators: the name they see
// (c.Name), whether its first parameter is a receiver (a method
// expression, (*Store).save), and the typed attributes c.Attr and
// c.ParamAttr read, the receiver's excluded.
type FuncInfo struct {
	Name       string
	Method     bool
	Attrs      []any
	ParamAttrs [][]any
}

// Wrap is fn with chain around each of its calls, outermost first: what a
// call decorator on a function is, written by hand.
//
//	var save = vuka.Wrap(saveOrder, vuka.FuncInfo{Name: "orders.save"}, logged, retry(3))
//
// A method keeps its name and calls the wrapped method expression, built on
// first use (a package-level var can't refer to itself):
//
//	var storeSave vuka.Decorated[func(*Store, context.Context, Order) error]
//
//	func (s *Store) Save(ctx context.Context, o Order) error {
//		return storeSave.Get(func() func(*Store, context.Context, Order) error {
//			return vuka.Wrap((*Store).save, vuka.FuncInfo{Name: "Store.Save", Method: true}, logged)
//		})(s, ctx, o)
//	}
//
// The call goes through reflection unless an adapter for F is registered
// (RegisterAdapter), which the vuka command generates for a package's
// decorated functions. With no decorators Wrap is fn.
func Wrap[F any](fn F, info FuncInfo, chain ...Decorator) F {
	if len(chain) == 0 {
		return fn
	}
	ft := reflect.TypeFor[F]()
	if ft.Kind() != reflect.Func {
		panic(fmt.Sprintf("vuka: Wrap of a %s, not a function", ft))
	}
	skip := 0
	if info.Method {
		if ft.NumIn() == 0 {
			panic("vuka: Wrap of a method with no receiver: " + info.Name)
		}
		skip = 1
	}
	d := describe(ft, skip, info)
	call := caller(d, chain, info.Method)
	if a, ok := adapters.Load(ft); ok {
		return a.(func(F, Caller) F)(fn, call)
	}
	fv := reflect.ValueOf(fn)
	return reflect.MakeFunc(ft, func(in []reflect.Value) []reflect.Value {
		args := make([]any, len(in))
		for i, v := range in {
			args[i] = v.Interface()
		}
		res := call(args, func(args []any) []any {
			vs := make([]reflect.Value, len(args))
			for i, a := range args {
				vs[i] = valueAs(a, ft.In(i))
			}
			var out []reflect.Value
			if ft.IsVariadic() {
				out = fv.CallSlice(vs)
			} else {
				out = fv.Call(vs)
			}
			rs := make([]any, len(out))
			for i, v := range out {
				rs[i] = v.Interface()
			}
			return rs
		})
		out := make([]reflect.Value, len(res))
		for i, r := range res {
			out[i] = valueAs(r, ft.Out(i))
		}
		return out
	}).Interface().(F)
}

// Caller runs a call through its decorators: args are the call's, the
// receiver first for a method, and invoke calls the function with the
// arguments the decorators leave; it returns the results they leave.
type Caller func(args []any, invoke func(args []any) []any) []any

var adapters sync.Map // reflect.Type of F → func(F, Caller) F

// RegisterAdapter has Wrap call functions of type F through adapt instead of
// reflection: adapt packs a call's arguments for call and unpacks its
// results. The vuka command writes one per signature a package decorates,
// in vuka_calls_gen.go:
//
//	vuka.RegisterAdapter(func(next func(int, int) int, call vuka.Caller) func(int, int) int {
//		invoke := func(a []any) []any { return []any{next(vuka.As[int](a[0]), vuka.As[int](a[1]))} }
//		return func(a0, a1 int) int {
//			r := call([]any{a0, a1}, invoke)
//			return vuka.As[int](r[0])
//		}
//	})
//
// The first adapter registered for F is kept.
func RegisterAdapter[F any](adapt func(next F, call Caller) F) {
	adapters.LoadOrStore(reflect.TypeFor[F](), adapt)
}

var (
	ctxType = reflect.TypeFor[context.Context]()
	errType = reflect.TypeFor[error]()
)

// describe is the Func a call of ft is described by.
func describe(ft reflect.Type, skip int, info FuncInfo) *Func {
	d := &Func{Name: info.Name, Attrs: info.Attrs, ParamAttrs: info.ParamAttrs, ErrIndex: -1, CtxIndex: -1}
	for i := skip; i < ft.NumIn(); i++ {
		if ft.In(i) == ctxType {
			d.CtxIndex = i - skip
			break
		}
	}
	n := ft.NumOut()
	if n > 0 && ft.Out(n-1) == errType {
		d.ErrIndex = n - 1
	}
	d.Zero = func() []any {
		zs := make([]any, n)
		for i := range zs {
			zs[i] = reflect.Zero(ft.Out(i)).Interface()
		}
		return zs
	}
	return d
}

func caller(d *Func, chain []Decorator, method bool) Caller {
	return func(args []any, invoke func([]any) []any) []any {
		c := &Call{Name: d.Name, Args: args, fn: d, chain: chain, call: invoke, method: method}
		if method {
			c.Receiver, c.Args = args[0], args[1:]
		}
		c.Run()
		return c.Results
	}
}

// valueAs is v as a value of t: As, by reflection.
func valueAs(v any, t reflect.Type) reflect.Value {
	x := reflect.New(t).Elem()
	if v == nil {
		return x
	}
	rv := reflect.ValueOf(v)
	if !rv.Type().AssignableTo(t) {
		panic(fmt.Sprintf("vuka: a decorator replaced a %s with a %T", t, v))
	}
	x.Set(rv)
	return x
}
