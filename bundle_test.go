package vuka

import "testing"

func TestDual(t *testing.T) {
	var got []string
	b := Compose(Dual(func(d *Decl) { got = append(got, "decl "+d.Name) }, func(t *Type) { got = append(got, "type "+t.Name) }), "attr")
	DeclareWith(b, &Decl{Name: "f"})
	DecorateType(b, &Type{Name: "T"})
	if len(got) != 2 || got[0] != "decl f" || got[1] != "type T" {
		t.Fatalf("%v", got)
	}
	if len(b.Attrs()) != 1 {
		t.Fatalf("attrs %v", b.Attrs())
	}
}
