package vuka

import (
	"reflect"
	"testing"
)

func declShow(id int, name string) string { return name }

func TestDeclOf(t *testing.T) {
	d := DeclOf(declShow, Param{Name: "id", Attrs: []any{perm("path")}}, Param{Name: "name"})
	if d.Name != "vuka.declShow" || d.Pkg != "github.com/vuka-lang/vuka" || d.File != "decl_test.go" || d.Line != 8 {
		t.Errorf("DeclOf: %s in %s at %s:%d", d.Name, d.Pkg, d.File, d.Line)
	}
	if len(d.Params) != 2 || d.Params[0].Name != "id" || d.Params[0].Type != reflect.TypeFor[int]() || d.Params[1].Name != "name" {
		t.Errorf("params: %+v", d.Params)
	}
	var p perm
	if !d.Params[0].Attr(&p) || p != "path" || d.Recv != nil || len(d.Results) != 1 {
		t.Errorf("param attrs %v, recv %v, results %v", d.Params[0].Attrs, d.Recv, d.Results)
	}

	m := DeclOf((*regBoard).Clear)
	if m.Name != "regBoard.Clear" || m.Recv != reflect.TypeFor[*regBoard]() || len(m.Params) != 0 {
		t.Errorf("a method: %s, recv %v, params %v", m.Name, m.Recv, m.Params)
	}
	if !m.Attr(&p) || p != "board.delete" {
		t.Errorf("a method's registered attributes: %v", m.Attrs)
	}
	if v := DeclOf(regBoard.Count); v.Name != "regBoard.Count" || v.Recv != reflect.TypeFor[regBoard]() {
		t.Errorf("a value method: %s, recv %v", v.Name, v.Recv)
	}
}

func TestTypeFor(t *testing.T) {
	ctor := func(n int) *regBoard { return &regBoard{n} }
	ty := TypeFor[regBoard](ctor, perm("a"), Compose(perm("b"), Decorator(func(*Call) {})))
	if ty.Name != "vuka.regBoard" || ty.Reflect != reflect.TypeFor[regBoard]() || ty.New == nil || len(ty.Fields) != 1 {
		t.Errorf("TypeFor: %+v", ty)
	}
	if len(ty.Attrs) != 2 || ty.Attrs[0] != perm("a") || ty.Attrs[1] != perm("b") {
		t.Errorf("attrs, a bundle's flattened: %v", ty.Attrs)
	}
	if n := TypeFor[perm](nil); n.New != nil || n.Fields != nil {
		t.Errorf("a type that isn't a struct: %+v", n)
	}
}
