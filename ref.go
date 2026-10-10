package vuka

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unsafe"
)

// Related is implemented by a field type that refers to a T held elsewhere,
// such as a foreign key. A field reference continues through it into T's
// fields: with Author of a type implementing Related[User], Post.Author.Name
// is a Ref[Post, string]. Related returns nil when there is no T.
type Related[T any] interface{ Related() *T }

// FieldPath is where a field reference leads from its root struct: one step
// per field written (Post.Author.Name has two), each step an index path that
// passes through embedded structs. Between steps it goes through a pointer or
// a Related value.
type FieldPath struct {
	root  reflect.Type
	index [][]int
}

// Root is the struct type the path starts from.
func (p FieldPath) Root() reflect.Type { return p.root }

// Index is the reflect index path of each step.
func (p FieldPath) Index() [][]int { return p.index }

// Fields are the struct fields of each step.
func (p FieldPath) Fields() []reflect.StructField {
	out := make([]reflect.StructField, len(p.index))
	t := p.root
	for i, idx := range p.index {
		if i > 0 {
			t = stepInto(out[i-1].Type)
		}
		out[i] = t.FieldByIndex(idx)
	}
	return out
}

// Names are the fields as written: ["Author", "Name"].
func (p FieldPath) Names() []string {
	var names []string
	for _, f := range p.Fields() {
		names = append(names, f.Name)
	}
	return names
}

func (p FieldPath) String() string { return strings.Join(p.Names(), ".") }

// Attrs are the typed attributes of the last field.
func (p FieldPath) Attrs() []any {
	if len(p.index) == 0 {
		return nil
	}
	t := p.root
	fields := p.Fields()
	if n := len(p.index); n > 1 {
		t = stepInto(fields[n-2].Type)
	}
	idx := p.index[len(p.index)-1]
	if len(idx) > 1 {
		t = deref(t.FieldByIndex(idx[:len(idx)-1]).Type)
	}
	return FieldAttrsOf(t)[fields[len(fields)-1].Name]
}

// Attr fills ptr with the last field's typed attribute of ptr's element type.
func (p FieldPath) Attr(ptr any) bool { return fillAttr(p.Attrs(), ptr) }

// Value follows the path in root, a value or pointer of the root type. It
// reports false when a nil pointer or relation is on the way.
func (p FieldPath) Value(root any) (any, bool) {
	v := reflect.ValueOf(root)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	} else {
		c := reflect.New(v.Type()).Elem()
		c.Set(v)
		v = c
	}
	v, ok := p.value(v)
	if !ok {
		return nil, false
	}
	return v.Interface(), true
}

// value follows the path from an addressable root.
func (p FieldPath) value(v reflect.Value) (reflect.Value, bool) {
	for i, idx := range p.index {
		if i > 0 {
			var ok bool
			if v, ok = stepValue(v); !ok {
				return v, false
			}
		}
		f, err := v.FieldByIndexErr(idx)
		if err != nil {
			return f, false
		}
		v = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem() // readable, unexported or not
	}
	return v, true
}

// stepInto is the struct a step continues into from a field of type t: a
// Related type's target, or the struct t is or points to.
func stepInto(t reflect.Type) reflect.Type {
	if m, ok := relatedMethod(t); ok {
		return m.Type.Out(0).Elem()
	}
	return deref(t)
}

func relatedMethod(t reflect.Type) (reflect.Method, bool) {
	pt := t
	if t.Kind() != reflect.Pointer {
		pt = reflect.PointerTo(t)
	}
	m, ok := pt.MethodByName("Related")
	if !ok || m.Type.NumIn() != 1 || m.Type.NumOut() != 1 || m.Type.Out(0).Kind() != reflect.Pointer {
		return m, false
	}
	return m, true
}

func stepValue(v reflect.Value) (reflect.Value, bool) {
	if _, ok := relatedMethod(v.Type()); ok {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return v, false
			}
		} else {
			v = v.Addr()
		}
		v = v.MethodByName("Related").Call(nil)[0]
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return v, false
		}
		v = v.Elem()
	}
	return v, true
}

func deref(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}

// Ref is a field reference: Post.Title in Vuka source, a typed handle on the
// Title field of Post, of type V. Through pointer, struct and Related fields it
// reaches further: Post.Author.Name. Its methods build predicates and orderings
// over T, or read the field.
//
// Vuka picks the most capable kind for the field's type: StringRef for
// strings, OrderedRef for numbers, CompareRef for types with a Compare
// method (time.Time), NullableRef for pointers, slices, maps, interfaces and
// Options, and Ref for the rest.
type Ref[T, V any] struct{ index [][]int }

// OrderedRef is a reference to a number field (or any cmp.Ordered type).
type OrderedRef[T any, V cmp.Ordered] struct{ Ref[T, V] }

// StringRef is a reference to a string field.
type StringRef[T any, V ~string] struct{ OrderedRef[T, V] }

// CompareRef is a reference to a field whose type orders itself, such as
// time.Time.
type CompareRef[T any, V interface{ Compare(V) int }] struct{ Ref[T, V] }

// NullableRef is a reference to a field that can be nil or None.
type NullableRef[T, V any] struct{ Ref[T, V] }

// RefOf, OrderedRefOf, StringRefOf, CompareRefOf and NullableRefOf make
// references; Vuka generates the calls. The function is never called: it is
// there so the compiler and the editor see the fields the reference names.
func RefOf[T, V any](index [][]int, _ func(*T)) Ref[T, V] { return Ref[T, V]{index} }

func OrderedRefOf[T any, V cmp.Ordered](index [][]int, _ func(*T)) OrderedRef[T, V] {
	return OrderedRef[T, V]{Ref[T, V]{index}}
}

func StringRefOf[T any, V ~string](index [][]int, _ func(*T)) StringRef[T, V] {
	return StringRef[T, V]{OrderedRef[T, V]{Ref[T, V]{index}}}
}

func CompareRefOf[T any, V interface{ Compare(V) int }](index [][]int, _ func(*T)) CompareRef[T, V] {
	return CompareRef[T, V]{Ref[T, V]{index}}
}

func NullableRefOf[T, V any](index [][]int, _ func(*T)) NullableRef[T, V] {
	return NullableRef[T, V]{Ref[T, V]{index}}
}

// FieldPath is where the reference leads.
func (r Ref[T, V]) FieldPath() FieldPath { return FieldPath{reflect.TypeFor[T](), r.index} }

// Name is the path as written, dotted: "Author.Name".
func (r Ref[T, V]) Name() string { return r.FieldPath().String() }

// Path is the fields as written: ["Author", "Name"].
func (r Ref[T, V]) Path() []string { return r.FieldPath().Names() }

// Attrs are the typed attributes of the field referred to.
func (r Ref[T, V]) Attrs() []any { return r.FieldPath().Attrs() }

// Attr fills ptr with the field's typed attribute of ptr's element type.
func (r Ref[T, V]) Attr(ptr any) bool { return fillAttr(r.Attrs(), ptr) }

// Lookup is the field's value in t; false when a nil pointer or relation is on
// the way.
func (r Ref[T, V]) Lookup(t T) (V, bool) {
	v, ok := r.FieldPath().value(reflect.ValueOf(&t).Elem())
	if !ok {
		var zero V
		return zero, false
	}
	x, _ := v.Interface().(V) // a nil interface is V's zero value
	return x, true
}

// Get is the field's value in t, or V's zero value when a nil pointer or
// relation is on the way.
func (r Ref[T, V]) Get(t T) V {
	v, _ := r.Lookup(t)
	return v
}

func (r Ref[T, V]) pred(op Op, value any, test func(V) bool) Pred[T] {
	return Pred[T]{Op: op, Path: r.FieldPath(), Value: value, test: func(v any) bool { x, _ := v.(V); return test(x) }}
}

// Eq is the predicate field == v.
func (r Ref[T, V]) Eq(v V) Pred[T] { return r.pred(OpEq, v, func(x V) bool { return equal(x, v) }) }

// Ne is the predicate field != v.
func (r Ref[T, V]) Ne(v V) Pred[T] { return r.pred(OpNe, v, func(x V) bool { return !equal(x, v) }) }

// In is the predicate that the field equals one of vs.
func (r Ref[T, V]) In(vs ...V) Pred[T] {
	return r.pred(OpIn, vs, func(x V) bool { return slices.ContainsFunc(vs, func(v V) bool { return equal(x, v) }) })
}

// Asc orders by the field, smallest first.
func (r Ref[T, V]) Asc() Order[T] { return Order[T]{Path: r.FieldPath(), cmp: kindCompare} }

// Desc orders by the field, largest first.
func (r Ref[T, V]) Desc() Order[T] {
	return Order[T]{Path: r.FieldPath(), Desc: true, cmp: kindCompare}
}

func equal[V any](a, b V) bool { return reflect.DeepEqual(a, b) }

// Lt is the predicate field < v.
func (r OrderedRef[T, V]) Lt(v V) Pred[T] { return r.pred(OpLt, v, func(x V) bool { return x < v }) }

// Le is the predicate field <= v.
func (r OrderedRef[T, V]) Le(v V) Pred[T] { return r.pred(OpLe, v, func(x V) bool { return x <= v }) }

// Gt is the predicate field > v.
func (r OrderedRef[T, V]) Gt(v V) Pred[T] { return r.pred(OpGt, v, func(x V) bool { return x > v }) }

// Ge is the predicate field >= v.
func (r OrderedRef[T, V]) Ge(v V) Pred[T] { return r.pred(OpGe, v, func(x V) bool { return x >= v }) }

func (r OrderedRef[T, V]) Asc() Order[T] { return Order[T]{Path: r.FieldPath(), cmp: compareAs[V]} }

func (r OrderedRef[T, V]) Desc() Order[T] {
	return Order[T]{Path: r.FieldPath(), Desc: true, cmp: compareAs[V]}
}

func compareAs[V cmp.Ordered](a, b any) int { return cmp.Compare(a.(V), b.(V)) }

// Contains is the predicate that the field contains s.
func (r StringRef[T, V]) Contains(s V) Pred[T] {
	return r.pred(OpContains, s, func(x V) bool { return strings.Contains(string(x), string(s)) })
}

// StartsWith is the predicate that the field starts with s.
func (r StringRef[T, V]) StartsWith(s V) Pred[T] {
	return r.pred(OpStartsWith, s, func(x V) bool { return strings.HasPrefix(string(x), string(s)) })
}

// EndsWith is the predicate that the field ends with s.
func (r StringRef[T, V]) EndsWith(s V) Pred[T] {
	return r.pred(OpEndsWith, s, func(x V) bool { return strings.HasSuffix(string(x), string(s)) })
}

// Lt is the predicate field < v, by V's Compare.
func (r CompareRef[T, V]) Lt(v V) Pred[T] {
	return r.pred(OpLt, v, func(x V) bool { return x.Compare(v) < 0 })
}

// Le is the predicate field <= v, by V's Compare.
func (r CompareRef[T, V]) Le(v V) Pred[T] {
	return r.pred(OpLe, v, func(x V) bool { return x.Compare(v) <= 0 })
}

// Gt is the predicate field > v, by V's Compare.
func (r CompareRef[T, V]) Gt(v V) Pred[T] {
	return r.pred(OpGt, v, func(x V) bool { return x.Compare(v) > 0 })
}

// Ge is the predicate field >= v, by V's Compare.
func (r CompareRef[T, V]) Ge(v V) Pred[T] {
	return r.pred(OpGe, v, func(x V) bool { return x.Compare(v) >= 0 })
}

func (r CompareRef[T, V]) Asc() Order[T] { return Order[T]{Path: r.FieldPath(), cmp: compareMethod[V]} }

func (r CompareRef[T, V]) Desc() Order[T] {
	return Order[T]{Path: r.FieldPath(), Desc: true, cmp: compareMethod[V]}
}

func compareMethod[V interface{ Compare(V) int }](a, b any) int { return a.(V).Compare(b.(V)) }

// IsNil is the predicate that the field is nil or None (or that a nil pointer
// or relation is on the way).
func (r NullableRef[T, V]) IsNil() Pred[T] { return Pred[T]{Op: OpIsNil, Path: r.FieldPath()} }

// NotNil is the predicate that the field is neither nil nor None.
func (r NullableRef[T, V]) NotNil() Pred[T] { return Pred[T]{Op: OpNotNil, Path: r.FieldPath()} }

// Op is what a predicate tests.
type Op string

const (
	OpEq         Op = "eq"
	OpNe         Op = "ne"
	OpLt         Op = "lt"
	OpLe         Op = "le"
	OpGt         Op = "gt"
	OpGe         Op = "ge"
	OpIn         Op = "in"
	OpContains   Op = "contains"
	OpStartsWith Op = "startswith"
	OpEndsWith   Op = "endswith"
	OpIsNil      Op = "isnil"
	OpNotNil     Op = "notnil"
	OpAnd        Op = "and"
	OpOr         Op = "or"
	OpNot        Op = "not"
)

// Pred is a condition on a T, built from field references —
// Post.Views.Gt(100), vuka.Or(…) — and kept as data, so a query builder can
// translate it (Op, the field's Path and attributes, the Value) or Match can
// test it in memory. Being typed by T, a Pred[User] can't be passed where a
// Pred[Post] is wanted.
type Pred[T any] struct {
	Op    Op
	Path  FieldPath // the field tested; zero for and, or and not
	Value any       // the operand: a V, a []V for in; nil for isnil, notnil and the combinations
	Preds []Pred[T] // the operands of and, or and not

	test func(any) bool
}

// And holds when every p does.
func And[T any](ps ...Pred[T]) Pred[T] { return Pred[T]{Op: OpAnd, Preds: ps} }

// Or holds when any p does.
func Or[T any](ps ...Pred[T]) Pred[T] { return Pred[T]{Op: OpOr, Preds: ps} }

// Not holds when p doesn't.
func Not[T any](p Pred[T]) Pred[T] { return Pred[T]{Op: OpNot, Preds: []Pred[T]{p}} }

// Match tests p on t. A comparison fails when a nil pointer or relation is on
// the way to its field; IsNil holds.
func (p Pred[T]) Match(t T) bool {
	switch p.Op {
	case OpAnd:
		for _, q := range p.Preds {
			if !q.Match(t) {
				return false
			}
		}
		return true
	case OpOr:
		for _, q := range p.Preds {
			if q.Match(t) {
				return true
			}
		}
		return false
	case OpNot:
		return !p.Preds[0].Match(t)
	}
	v, ok := p.Path.value(reflect.ValueOf(&t).Elem())
	switch p.Op {
	case OpIsNil:
		return !ok || isNil(v)
	case OpNotNil:
		return ok && !isNil(v)
	}
	return ok && p.test != nil && p.test(v.Interface())
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan, reflect.Func:
		return v.IsNil()
	}
	if o, ok := v.Interface().(interface{ IsNone() bool }); ok {
		return o.IsNone()
	}
	return false
}

var opText = map[Op]string{OpEq: "=", OpNe: "!=", OpLt: "<", OpLe: "<=", OpGt: ">", OpGe: ">=",
	OpContains: "contains", OpStartsWith: "starts with", OpEndsWith: "ends with"}

// String writes p readably: Author.Name = "ada", (Views > 100 OR Title contains "go").
func (p Pred[T]) String() string {
	switch p.Op {
	case OpAnd, OpOr:
		parts := make([]string, len(p.Preds))
		for i, q := range p.Preds {
			parts[i] = q.String()
		}
		return "(" + strings.Join(parts, " "+strings.ToUpper(string(p.Op))+" ") + ")"
	case OpNot:
		return "NOT " + p.Preds[0].String()
	case OpIsNil:
		return p.Path.String() + " IS NIL"
	case OpNotNil:
		return p.Path.String() + " IS NOT NIL"
	case OpIn:
		v := reflect.ValueOf(p.Value)
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = operand(v.Index(i).Interface())
		}
		return p.Path.String() + " IN (" + strings.Join(parts, ", ") + ")"
	}
	return p.Path.String() + " " + opText[p.Op] + " " + operand(p.Value)
}

func operand(v any) string {
	if reflect.ValueOf(v).Kind() == reflect.String {
		return fmt.Sprintf("%q", v)
	}
	return fmt.Sprint(v)
}

// Order is an ordering of Ts by a field: Post.Views.Desc().
type Order[T any] struct {
	Path FieldPath
	Desc bool

	cmp func(a, b any) int
}

func (o Order[T]) String() string {
	if o.Desc {
		return o.Path.String() + " DESC"
	}
	return o.Path.String() + " ASC"
}

// Compare orders a and b by o: negative when a comes first. A value whose
// field can't be reached (a nil pointer or relation on the way) comes first.
func (o Order[T]) Compare(a, b T) int {
	va, oka := o.Path.value(reflect.ValueOf(&a).Elem())
	vb, okb := o.Path.value(reflect.ValueOf(&b).Elem())
	c := 0
	switch {
	case !oka || !okb:
		c = cmp.Compare(btoi(oka), btoi(okb))
	default:
		c = o.cmp(va.Interface(), vb.Interface())
	}
	if o.Desc {
		return -c
	}
	return c
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SortBy sorts xs by the orders, the first deciding, the next breaking ties;
// the sort is stable.
func SortBy[T any](xs []T, orders ...Order[T]) {
	slices.SortStableFunc(xs, func(a, b T) int {
		for _, o := range orders {
			if c := o.Compare(a, b); c != 0 {
				return c
			}
		}
		return 0
	})
}

// kindCompare orders the values of a field by their kind, for a reference
// that isn't of an ordered type: booleans (false first), numbers and strings.
func kindCompare(a, b any) int {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	switch va.Kind() {
	case reflect.Bool:
		return cmp.Compare(btoi(va.Bool()), btoi(vb.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmp.Compare(va.Int(), vb.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cmp.Compare(va.Uint(), vb.Uint())
	case reflect.Float32, reflect.Float64:
		return cmp.Compare(va.Float(), vb.Float())
	case reflect.String:
		return cmp.Compare(va.String(), vb.String())
	}
	panic(fmt.Sprintf("vuka: can't order by a %s field", va.Type()))
}
