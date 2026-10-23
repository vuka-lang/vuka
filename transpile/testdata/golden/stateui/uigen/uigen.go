// Package uigen is the golden cases' stand-in for github.com/vuka-lang/ui/v2/uigen:
// what the compiler's client state and tracked server state lower to, over
// stateui's states.
package uigen

import (
	"reflect"
	"unsafe"

	"vukatest.local/stateui"
)

// Assign is tracked server state: Get, Set, Update; Writes counts the
// changes, as the real one marks itself changed.
type Assign[T any] struct {
	v T
	n int
}

func (a *Assign[T]) Get() T             { return a.v }
func (a *Assign[T]) Set(v T)            { a.v, a.n = v, a.n+1 }
func (a *Assign[T]) Update(f func(T) T) { a.v, a.n = f(a.v), a.n+1 }
func (a *Assign[T]) Writes() int        { return a.n }

func UseState[T any](initial T) stateui.State[T] { return stateui.GenState("", false, initial) }
func UsePageState[T any](initial T) stateui.State[T] {
	return stateui.GenState("", true, initial)
}
func State[T any](site string, initial T) stateui.State[T] {
	return stateui.GenState(site, false, initial)
}
func PageState[T any](site string, initial T) stateui.State[T] {
	return stateui.GenState(site, true, initial)
}

// States gives the zero stateui.State fields of the struct p points to a
// state each, named after the field.
func States(p any) {
	v := reflect.ValueOf(p).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
		if s, ok := f.Interface().(interface{ Zero() bool }); ok && s.Zero() {
			f.Set(reflect.ValueOf(f.Interface().(interface{ Named(string) any }).Named(v.Type().Field(i).Name)))
		}
	}
}

func Derived[T any](v T) stateui.State[T] { return stateui.GenDerived(v) }
func At[T, S any](s stateui.State[S], path string) stateui.State[T] {
	return stateui.GenAt[T](s, path)
}

func Not[T any](s stateui.State[T]) stateui.State[bool]                 { return s.GenNot() }
func And[T any](s stateui.State[T], o any) stateui.State[bool]          { return s.GenAnd(o) }
func Or[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenOr(o) }
func Eq[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenEq(o) }
func Ne[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenNe(o) }
func Lt[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenLt(o) }
func Le[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenLe(o) }
func Gt[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenGt(o) }
func Ge[T any](s stateui.State[T], o any) stateui.State[bool]           { return s.GenGe(o) }
func Plus[T any](s stateui.State[T], o any) stateui.State[T]            { return s.GenPlus(o) }
func Minus[T any](s stateui.State[T], o any) stateui.State[T]           { return s.GenMinus(o) }
func Neg[T any](s stateui.State[T]) stateui.State[T]                    { return s.GenNeg() }
func Len[T any](s stateui.State[T]) stateui.State[int]                  { return s.GenLen() }
func Toggle(c stateui.Commands, s stateui.State[bool]) stateui.Commands { return s.GenToggle(c) }

type number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

func Inc[T number](c stateui.Commands, s stateui.State[T]) stateui.Commands { return s.GenInc(c) }
func Dec[T number](c stateui.Commands, s stateui.State[T]) stateui.Commands { return s.GenDec(c) }
