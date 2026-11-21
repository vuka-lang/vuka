package vuka

import (
	"reflect"
	"slices"
	"testing"
)

type mtInner struct{ n int }

func (i *mtInner) Bump(by int) int { i.n += by; return i.n }
func (i mtInner) Count() int       { return i.n }
func (i mtInner) Shadowed() string { return "inner" }

type mtOuter struct {
	mtInner
	Name string
}

func (o *mtOuter) Rename(name string) { o.Name = name }
func (o *mtOuter) Decorated() string  { return "d" }
func (o mtOuter) Shadowed() string    { return "outer" }
func (o mtOuter) Join(sep string, parts ...string) string {
	return o.Name + sep + parts[0]
}

// mtPlain has no table: Methods finds its methods by reflection.
type mtPlain struct{ mtInner }

func (mtPlain) Hello() string { return "hi" }

func init() {
	RegisterMethods[mtInner]((*mtInner).Bump, mtInner.Count, mtInner.Shadowed)
	RegisterMethods[mtOuter]((*mtOuter).Rename, mtOuter.Shadowed, mtOuter.Join)
	RegisterFunc((*mtOuter).Decorated) // in the table too, as generated code leaves it to RegisterFunc
}

// TestMethodsTable: a registered type's method set is reflect's, promoted
// methods included, value and pointer receivers each in their set.
func TestMethodsTable(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[mtOuter](), reflect.TypeFor[*mtOuter](), reflect.TypeFor[*mtInner](), reflect.TypeFor[mtInner]()} {
		var got, want []string
		for _, m := range Methods(typ) {
			got = append(got, m.Name+" "+m.Type.String())
		}
		for i := range typ.NumMethod() {
			m := typ.Method(i)
			want = append(want, m.Name+" "+m.Type.String())
		}
		if !slices.Equal(got, want) {
			t.Errorf("Methods(%s) = %q, want %q", typ, got, want)
		}
	}
	o := &mtOuter{Name: "a"}
	m, ok := MethodByName(reflect.TypeOf(o), "Bump")
	if !ok || m.Promoted == nil {
		t.Fatalf("MethodByName(*mtOuter, Bump) = %+v, %v; want a promoted method", m, ok)
	}
	if n := m.Func.Call([]reflect.Value{reflect.ValueOf(o), reflect.ValueOf(2)})[0].Int(); n != 2 || o.n != 2 {
		t.Errorf("promoted Bump = %d, field %d; want 2, 2", n, o.n)
	}
	if s := mustMethod(t, o, "Shadowed").Bind(reflect.ValueOf(o)).Call(nil)[0].String(); s != "outer" {
		t.Errorf("Shadowed = %q, want the outer's", s)
	}
	join := mustMethod(t, o, "Join").Bind(reflect.ValueOf(o))
	if s := join.Interface().(func(string, ...string) string)("-", "x"); s != "a-x" {
		t.Errorf("Join = %q", s)
	}
	m = mustMethod(t, o, "Shadowed")
	if m.Promoted != nil || m.Decl.Type().In(0) != reflect.TypeFor[mtOuter]() {
		t.Errorf("Shadowed of *mtOuter: Promoted %v, Decl %s; want the value method as declared", m.Promoted, m.Decl.Type())
	}
}

// TestMethodsReflect: a type without a table has its methods by reflection
// (but for the vuka_release tag).
func TestMethodsReflect(t *testing.T) {
	ms := Methods(reflect.TypeFor[*mtPlain]())
	var names []string
	for _, m := range ms {
		names = append(names, m.Name)
	}
	if want := []string{"Bump", "Count", "Hello", "Shadowed"}; !slices.Equal(names, want) {
		t.Fatalf("Methods(*mtPlain) = %q, want %q", names, want)
	}
	if ms[2].Promoted != nil || ms[0].Promoted == nil {
		t.Errorf("Promoted: Hello %v, Bump %v", ms[2].Promoted, ms[0].Promoted)
	}
}

func mustMethod(t *testing.T, x any, name string) Method {
	t.Helper()
	m, ok := MethodByName(reflect.TypeOf(x), name)
	if !ok {
		t.Fatalf("no method %s of %T", name, x)
	}
	return m
}
