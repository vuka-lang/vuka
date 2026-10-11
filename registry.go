package vuka

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

// The registry of declarations' attributes: what Vuka generates for each
// function, method and type carrying typed attributes (or a bundle, whose
// attributes count), filled at init, so that a library may read a
// declaration's attributes where it holds only a value — a web framework
// dispatching an event to a method value reads the gate written on the
// method, before calling it.

// Attrs are a declaration's typed attributes, as the lookups return them.
type Attrs []any

// Attr fills ptr with the attribute of ptr's element type, reporting
// whether there is one, as Call.Attr and Decl.Attr do: var g Gate;
// attrs.Attr(&g).
func (a Attrs) Attr(ptr any) bool { return fillAttr(a, ptr) }

var registry = struct {
	sync.RWMutex
	funcs map[string]Attrs        // "example.com/app.Board.Clear", "example.com/app.charge"
	types map[string]reflect.Type // "example.com/app.Board"
	attrs map[reflect.Type]Attrs
	pcs   sync.Map // uintptr → FuncRef: FuncOf's answers
}{funcs: map[string]Attrs{}, types: map[string]reflect.Type{}, attrs: map[reflect.Type]Attrs{}}

// RegisterFunc records the typed attributes of a function, or of a method
// given as its method expression ((*Board).Clear). Generated code calls it
// at init; Go code may too, for a declaration it writes by hand.
func RegisterFunc(fn any, attrs ...any) {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func || v.IsNil() {
		panic(fmt.Sprintf("vuka: RegisterFunc of %T, not a function", fn))
	}
	f := runtime.FuncForPC(v.Pointer())
	if f == nil {
		return
	}
	key, _, _, lit := parseFuncName(f.Name())
	if lit {
		panic("vuka: RegisterFunc of a function literal " + f.Name())
	}
	registry.Lock()
	registry.funcs[key] = append(registry.funcs[key], attrs...)
	registry.Unlock()
	registry.pcs.Clear()
}

// RegisterType records the typed attributes of type T. Generated code calls
// it at init.
func RegisterType[T any](attrs ...any) {
	t := reflect.TypeFor[T]()
	registry.Lock()
	registry.attrs[t] = append(registry.attrs[t], attrs...)
	registry.types[typeKey(t)] = t
	registry.Unlock()
	registry.pcs.Clear()
}

// TypeAttrs are the typed attributes declared on t (or on the type t points
// to); none for a type Vuka registered nothing for.
func TypeAttrs(t reflect.Type) Attrs {
	t = named(t)
	if t == nil {
		return nil
	}
	registry.RLock()
	defer registry.RUnlock()
	return registry.attrs[t]
}

// MethodAttrs are the typed attributes declared on method name of t (or of
// the type t points to), its bundles' included; not its type's, which
// TypeAttrs has. None for a method Vuka registered nothing for.
func MethodAttrs(t reflect.Type, name string) Attrs {
	t = named(t)
	if t == nil {
		return nil
	}
	registry.RLock()
	defer registry.RUnlock()
	return registry.funcs[typeKey(t)+"."+name]
}

// FuncRef is the declaration a function value's code belongs to, as FuncOf
// finds it.
type FuncRef struct {
	Name string // the runtime's name for the code: "example.com/app.(*Board).Clear-fm"
	// Recv is a method's receiver type as declared (Board for *Board) when
	// Vuka registered the type; Method is the method's name.
	Recv   reflect.Type
	Method string
	// Attrs are the declaration's typed attributes: a method's own (its
	// type's are TypeAttrs(Recv)).
	Attrs Attrs
	// Literal says fn is a function literal ("…(*Board).Render.func1"): it
	// is no declaration and has no attributes, and what it calls is found
	// only when it runs.
	Literal bool
}

// ErrNotFunc is FuncOf's error for a value that is no function, or nil.
var ErrNotFunc = errors.New("vuka: not a function")

// FuncOf finds the declaration fn's code belongs to: a function, a method
// expression ((*Board).Clear) or a method value (b.Clear). A function
// literal is reported as one (Literal); a function or method nothing
// registered has no Attrs. A method value made by reflection
// (reflect.Value.Method) names no declaration either: look it up with
// MethodAttrs. Answers are cached per function, so a lookup costs a map
// read after the first.
func FuncOf(fn any) (FuncRef, error) {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func || v.IsNil() {
		return FuncRef{}, ErrNotFunc
	}
	pc := v.Pointer()
	if r, ok := registry.pcs.Load(pc); ok {
		return r.(FuncRef), nil
	}
	f := runtime.FuncForPC(pc)
	if f == nil {
		return FuncRef{}, ErrNotFunc
	}
	ref := FuncRef{Name: f.Name()}
	key, recv, method, lit := parseFuncName(ref.Name)
	ref.Literal = lit
	if !lit {
		registry.RLock()
		ref.Attrs = registry.funcs[key]
		if recv != "" {
			ref.Recv, ref.Method = registry.types[recv], method
		}
		registry.RUnlock()
	}
	registry.pcs.Store(pc, ref)
	return ref, nil
}

func named(t reflect.Type) reflect.Type {
	if t != nil && t.Kind() == reflect.Pointer && t.Name() == "" {
		t = t.Elem()
	}
	if t == nil || t.Name() == "" {
		return nil
	}
	return t
}

// typeKey is a named type as the runtime names its code's package: the
// import path, its last element's dots escaped, then the type's name.
func typeKey(t reflect.Type) string {
	return escapePath(t.PkgPath()) + "." + t.Name()
}

func escapePath(p string) string {
	i := strings.LastIndexByte(p, '/') + 1
	return p[:i] + strings.ReplaceAll(p[i:], ".", "%2e")
}

// parseFuncName reads a function's runtime name: key is the registry's
// ("pkg.T.M" for a method, whatever its receiver; "pkg.F" for a
// function), recv and method a method's ("pkg.T", "M"), lit whether it is
// a function literal.
func parseFuncName(name string) (key, recv, method string, lit bool) {
	name = strings.TrimSuffix(name, "-fm")
	slash := strings.LastIndexByte(name, '/') + 1
	dot := strings.IndexByte(name[slash:], '.')
	if dot < 0 {
		return name, "", "", false
	}
	pkg, rest := name[:slash+dot], name[slash+dot+1:]
	// A generic's type arguments, "Box[...]": the registry holds no generic.
	if i := strings.IndexByte(rest, '['); i >= 0 {
		if j := strings.IndexByte(rest[i:], ']'); j >= 0 {
			rest = rest[:i] + rest[i+j+1:]
		}
	}
	rest = strings.Replace(strings.Replace(rest, "(*", "", 1), ")", "", 1)
	parts := strings.Split(rest, ".")
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "func") || strings.HasPrefix(p, "gowrap") || strings.Contains(p, "-") || p == "deferwrap1" {
			return "", "", "", true
		}
	}
	switch len(parts) {
	case 1:
		return pkg + "." + parts[0], "", "", false
	case 2:
		return pkg + "." + parts[0] + "." + parts[1], pkg + "." + parts[0], parts[1], false
	}
	return "", "", "", true
}
