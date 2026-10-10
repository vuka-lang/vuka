package vuka

import (
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func outer(c *Call) { c.Next() }
func inner(c *Call) { c.Next() }

func double(fn *Func, chain []Decorator, n int) int {
	c := NewCall(fn, chain, nil, []any{n}, func(c *Call) []any { return []any{c.Args[0].(int) * 2} })
	c.Run()
	return c.Results[0].(int)
}

func TestObserveCalls(t *testing.T) {
	fn := &Func{Name: "main.double", ErrIndex: -1, CtxIndex: -1, Zero: func() []any { return []any{0} }}
	chain := []Decorator{outer, inner}
	var log []string
	name := func(d Decorator) string {
		if d == nil {
			return "func"
		}
		n := runtime.FuncForPC(reflect.ValueOf(d).Pointer()).Name()
		return n[strings.LastIndexByte(n, '.')+1:]
	}
	stop := ObserveCalls(func(c *Call, d Decorator) func() {
		log = append(log, c.Name+" "+name(d))
		return func() { log = append(log, "end "+name(d)) }
	})
	if got := double(fn, chain, 2); got != 4 {
		t.Fatalf("observed: %d", got)
	}
	want := []string{"main.double outer", "main.double inner", "main.double func", "end func", "end inner", "end outer"}
	if !slices.Equal(log, want) {
		t.Errorf("steps %q, want %q", log, want)
	}
	stop()
	log = nil
	if double(fn, chain, 3) != 6 || log != nil {
		t.Errorf("after stop: %q", log)
	}
	if callObservers.Load() != nil {
		t.Error("observers left after the last stopped")
	}
}

func BenchmarkCallUnobserved(b *testing.B) {
	fn := &Func{Name: "main.double", ErrIndex: -1, CtxIndex: -1, Zero: func() []any { return []any{0} }}
	chain := []Decorator{outer, inner}
	for i := 0; i < b.N; i++ {
		double(fn, chain, 2)
	}
}
