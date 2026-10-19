package vuka

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type wperm string

type store struct{ prefix string }

func (s *store) save(ctx context.Context, name string, tags ...string) (string, error) {
	return s.prefix + name + strings.Join(tags, ","), nil
}

type saveFunc = func(*store, context.Context, string, ...string) (string, error)

func adaptSave(next saveFunc, call Caller) saveFunc {
	invoke := func(a []any) []any {
		r0, r1 := next(As[*store](a[0]), As[context.Context](a[1]), As[string](a[2]), As[[]string](a[3])...)
		return []any{r0, r1}
	}
	return func(a0 *store, a1 context.Context, a2 string, a3 ...string) (string, error) {
		r := call([]any{a0, a1, a2, a3}, invoke)
		return As[string](r[0]), As[error](r[1])
	}
}

// TestWrap runs a method through decorators that read the call and change
// it, by reflection and through an adapter.
func TestWrap(t *testing.T) {
	var seen []string
	look := func(c *Call) {
		var p wperm
		c.Attr(&p)
		var q wperm
		c.ParamAttr(1, &q)
		seen = append(seen, fmt.Sprintf("%s %v %s %s %v", c.Name, c.Receiver.(*store).prefix, p, q, c.Context() != nil))
		c.Args[1] = Arg[string](c, 1) + "!"
		c.Next()
	}
	deny := func(c *Call) {
		if Arg[string](c, 1) == "x!" {
			c.SetErr(errors.New("denied"))
			return
		}
		c.Next()
		c.Results[0] = strings.ToUpper(c.Results[0].(string))
	}
	info := FuncInfo{Name: "store.Save", Method: true, Attrs: []any{wperm("write")}, ParamAttrs: [][]any{nil, {wperm("name")}}}
	for _, typed := range []bool{false, true} {
		adapters.Delete(reflect.TypeFor[saveFunc]())
		if typed {
			RegisterAdapter(adaptSave)
		}
		seen = nil
		f := Wrap((*store).save, info, look, deny)
		s := &store{"s:"}
		got, err := f(s, context.Background(), "a", "t1", "t2")
		if got != "S:A!T1,T2" || err != nil {
			t.Errorf("typed %v: %q, %v", typed, got, err)
		}
		if got, err := f(s, context.Background(), "x"); got != "" || err == nil || err.Error() != "denied" {
			t.Errorf("typed %v, denied: %q, %v", typed, got, err)
		}
		want := []string{"store.Save s: write name true", "store.Save s: write name true"}
		if !slices.Equal(seen, want) {
			t.Errorf("typed %v: decorators saw %q", typed, seen)
		}
	}
	adapters.Delete(reflect.TypeFor[saveFunc]())
}

// TestWrapFunc checks Wrap of a function: Return skipping it, no results,
// no decorators, and a decorator leaving the wrong type.
func TestWrapFunc(t *testing.T) {
	calls := 0
	cached := Wrap(func(n int) int { calls++; return n * 2 }, FuncInfo{Name: "main.double"}, func(c *Call) {
		if Arg[int](c, 0) == 0 {
			c.Return(-1)
			return
		}
		c.Next()
	})
	if cached(0) != -1 || cached(3) != 6 || calls != 1 {
		t.Errorf("Return: calls %d", calls)
	}
	ran := false
	Wrap(func() { ran = true }, FuncInfo{Name: "main.run"}, func(c *Call) { c.Next() })()
	if !ran {
		t.Error("a function with no results didn't run")
	}
	f := func(n int) int { return n }
	if g := Wrap(f, FuncInfo{}); reflect.ValueOf(g).Pointer() != reflect.ValueOf(f).Pointer() {
		t.Error("Wrap with no decorators isn't the function")
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "replaced a int with a string") {
			t.Errorf("a result of the wrong type: %v", r)
		}
	}()
	Wrap(f, FuncInfo{Name: "main.f"}, func(c *Call) { c.Return("no") })(1)
}

func add(a, b int) int { return a + b }

func adaptAdd(next func(int, int) int, call Caller) func(int, int) int {
	invoke := func(a []any) []any { return []any{next(As[int](a[0]), As[int](a[1]))} }
	return func(a0, a1 int) int {
		r := call([]any{a0, a1}, invoke)
		return As[int](r[0])
	}
}

// The cost of a call through two decorators: by reflection, through a
// generated adapter, and through the adapter older vuka commands wrote.
func BenchmarkWrapReflect(b *testing.B) {
	adapters.Delete(reflect.TypeFor[func(int, int) int]())
	f := Wrap(add, FuncInfo{Name: "main.add"}, outer, inner)
	for i := 0; i < b.N; i++ {
		f(i, 2)
	}
}

func BenchmarkWrapAdapter(b *testing.B) {
	adapters.Delete(reflect.TypeFor[func(int, int) int]())
	RegisterAdapter(adaptAdd)
	defer adapters.Delete(reflect.TypeFor[func(int, int) int]())
	f := Wrap(add, FuncInfo{Name: "main.add"}, outer, inner)
	for i := 0; i < b.N; i++ {
		f(i, 2)
	}
}

func BenchmarkWrapNewCall(b *testing.B) {
	fn := &Func{Name: "main.add", ErrIndex: -1, CtxIndex: -1, Zero: func() []any { return []any{0} }}
	chain := []Decorator{outer, inner}
	f := func(a, b int) int {
		c := NewCall(fn, chain, nil, []any{a, b}, func(c *Call) []any { return []any{add(As[int](c.Args[0]), As[int](c.Args[1]))} })
		c.Run()
		return As[int](c.Results[0])
	}
	for i := 0; i < b.N; i++ {
		f(i, 2)
	}
}
