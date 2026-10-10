package vuka

import (
	"reflect"
	"sync"
	"unsafe"
)

// Assign is a stateful component's state value whose changes a live session
// tracks: Set and Update mark it changed. A component whose state fields
// (its unexported ones, except those tagged `vuka:"-"`) are all Assigns
// renders again only when one of them changed or its props did; otherwise
// the session keeps its last render without calling Render.
//
//	type Row struct {
//		vuka.Live
//		Name string            // a prop
//		qty  vuka.Assign[int]  // state
//	}
//
//	func (r *Row) Inc() { r.qty.Update(func(n int) int { return n + 1 }) }
//
// Render must then read nothing but props and Assigns (and fields tagged
// `vuka:"-"`, which must not change what it renders); in tests (or with
// VUKA_LIVE_VERIFY=1) the session renders skipped components anyway and
// fails when the result differs.
type Assign[T any] struct {
	v     T
	dirty bool
}

// Get is the value.
func (a *Assign[T]) Get() T { return a.v }

// Set sets the value and marks it changed.
func (a *Assign[T]) Set(v T) { a.v, a.dirty = v, true }

// Update sets the value to f of it and marks it changed.
func (a *Assign[T]) Update(f func(T) T) { a.v, a.dirty = f(a.v), true }

func (a *Assign[T]) assignDirty() *bool { return &a.dirty }

type assigner interface{ assignDirty() *bool }

// stateFields is a component type's Assign fields, or tracked false when a
// state field isn't one.
type stateFields struct {
	tracked bool
	fields  []int
}

var stateCache sync.Map // reflect.Type (the struct) → stateFields

var assignerType = reflect.TypeFor[assigner]()

func stateOf(t reflect.Type) stateFields {
	if v, ok := stateCache.Load(t); ok {
		return v.(stateFields)
	}
	sf := stateFields{tracked: true}
	live := reflect.TypeFor[Live]()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		switch {
		case f.IsExported() || f.Type == live || f.Tag.Get("vuka") == "-":
		case reflect.PointerTo(f.Type).Implements(assignerType):
			sf.fields = append(sf.fields, i)
		default:
			sf.tracked = false
		}
	}
	stateCache.Store(t, sf)
	return sf
}

func assignsOf(c Stateful, each func(dirty *bool)) bool {
	v := reflect.ValueOf(c).Elem()
	sf := stateOf(v.Type())
	if !sf.tracked {
		return false
	}
	for _, i := range sf.fields {
		f := v.Field(i)
		each(reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Interface().(assigner).assignDirty())
	}
	return true
}

// TrackedState reports whether c's state is all Assigns (tracked), and
// whether one of them changed since SettleState. Package live calls it.
func TrackedState(c Stateful) (tracked, changed bool) {
	tracked = assignsOf(c, func(d *bool) { changed = changed || *d })
	return tracked, changed
}

// SettleState marks c's Assigns unchanged, after a render.
func SettleState(c Stateful) { assignsOf(c, func(d *bool) { *d = false }) }
