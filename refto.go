package vuka

import (
	"cmp"
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

// RefTo is a reference to the field whose address field returns, the way a
// field reference is written in Go, Post.Title in Vuka source:
//
//	title := vuka.StringRefTo(func(p *Post) *string { return &p.Title })
//	city := vuka.StringRefTo(func(p *Post) *string { return &p.Home.City })
//	name := vuka.StringRefTo(func(p *Post) *string { return &p.Author.Related().Name })
//
// The path goes through embedded structs, pointers and Related values. The
// compiler checks the fields; field runs once per instantiation, on a value
// of T made for it (nil pointers filled in), and must only select fields.
// OrderedRefTo, StringRefTo, CompareRefTo and NullableRefTo make the other
// kinds (see Ref).
func RefTo[T, V any](field func(*T) *V) Ref[T, V] { return Ref[T, V]{indexOf(field)} }

func OrderedRefTo[T any, V cmp.Ordered](field func(*T) *V) OrderedRef[T, V] {
	return OrderedRef[T, V]{RefTo(field)}
}

func StringRefTo[T any, V ~string](field func(*T) *V) StringRef[T, V] {
	return StringRef[T, V]{OrderedRefTo(field)}
}

func CompareRefTo[T any, V interface{ Compare(V) int }](field func(*T) *V) CompareRef[T, V] {
	return CompareRef[T, V]{RefTo(field)}
}

func NullableRefTo[T, V any](field func(*T) *V) NullableRef[T, V] {
	return NullableRef[T, V]{RefTo(field)}
}

var refPaths sync.Map // refKey → [][]int

type refKey struct {
	pc   uintptr
	root reflect.Type
}

// indexOf is the index path of the field field selects: field runs on a
// probe of T, every nil pointer on the way filled in, and the address it
// returns is found among the probe's fields.
func indexOf[T, V any](field func(*T) *V) [][]int {
	root := reflect.TypeFor[T]()
	key := refKey{reflect.ValueOf(field).Pointer(), root}
	if p, ok := refPaths.Load(key); ok {
		return p.([][]int)
	}
	if root.Kind() != reflect.Struct {
		panic(fmt.Sprintf("vuka: RefTo of a %s, not a struct", root))
	}
	probe := reflect.New(root)
	fill(probe.Elem(), map[reflect.Type]int{})
	var addr *V
	func() {
		defer func() {
			if r := recover(); r != nil {
				panic(fmt.Sprintf("vuka: RefTo of %s: the function must return the address of a field of its argument: %v", root, r))
			}
		}()
		addr = field(probe.Interface().(*T))
	}()
	path, ok := locate(probe.Elem(), uintptr(unsafe.Pointer(addr)), reflect.TypeFor[V](), nil, nil)
	if !ok {
		panic(fmt.Sprintf("vuka: RefTo of %s: the function returns the address of no field of its argument", root))
	}
	refPaths.Store(key, path)
	return path
}

// fill points v's nil pointers, exported or not, at new values filled in
// turn, so a field function can go through them; a type is filled at most
// twice on the way down.
func fill(v reflect.Value, seen map[reflect.Type]int) {
	if v.Kind() != reflect.Struct || seen[v.Type()] >= 2 {
		return
	}
	seen[v.Type()]++
	defer func() { seen[v.Type()]-- }()
	for i := range v.NumField() {
		f := v.Field(i)
		f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
		switch {
		case f.Kind() == reflect.Pointer && f.IsNil() && f.Type().Elem().Kind() == reflect.Struct:
			p := reflect.New(f.Type().Elem())
			fill(p.Elem(), seen)
			f.Set(p)
		case f.Kind() == reflect.Struct:
			fill(f, seen)
		}
	}
}

// locate finds the field of type want at addr in v (addressable): the
// steps of its path, the current step being step.
func locate(v reflect.Value, addr uintptr, want reflect.Type, steps [][]int, step []int) ([][]int, bool) {
	base := v.UnsafeAddr()
	for i := range v.NumField() {
		sf := v.Type().Field(i)
		f := v.Field(i)
		at := base + sf.Offset
		idx := append(append([]int(nil), step...), i)
		if at == addr && sf.Type == want {
			return append(steps, idx), true
		}
		inside := addr >= at && addr < at+sf.Type.Size()
		switch {
		case inside && sf.Anonymous && sf.Type.Kind() == reflect.Struct:
			if p, ok := locate(f, addr, want, steps, idx); ok {
				return p, true
			}
		case inside && sf.Type.Kind() == reflect.Struct:
			if p, ok := locate(f, addr, want, append(steps, idx), nil); ok {
				return p, true
			}
		}
		target, ok := pointee(f)
		if !ok || target.UnsafeAddr() == base {
			continue
		}
		next, nextStep := append(steps, idx), []int(nil)
		if sf.Anonymous && sf.Type.Kind() == reflect.Pointer {
			next, nextStep = steps, idx
		}
		if p, ok := locate(target, addr, want, next, nextStep); ok {
			return p, true
		}
	}
	return nil, false
}

// pointee is the struct a field leads to past itself: its Related target or
// what it points to.
func pointee(f reflect.Value) (reflect.Value, bool) {
	f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
	if _, ok := relatedMethod(f.Type()); ok {
		r := f
		if r.Kind() != reflect.Pointer {
			r = r.Addr()
		} else if r.IsNil() {
			return reflect.Value{}, false
		}
		t := r.MethodByName("Related").Call(nil)[0]
		if t.IsNil() || t.Elem().Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		return t.Elem(), true
	}
	if f.Kind() == reflect.Pointer && !f.IsNil() && f.Type().Elem().Kind() == reflect.Struct {
		return f.Elem(), true
	}
	return reflect.Value{}, false
}
