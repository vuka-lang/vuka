package vuka

import (
	"slices"
	"testing"
)

type tag struct{ Name string }

type stamps struct{ Version int }

type owner struct{ Name string }

type link[T any] struct{ to *T }

func (l *link[T]) Related() *T { return l.to }

type doc struct {
	*stamps
	Title string
	Owner link[owner]
	Boss  *owner
	Tags  []string
	Nick  Option[string]
}

func TestRefs(t *testing.T) {
	RegisterFields[doc](map[string][]any{"Title": {tag{"title"}}})
	RegisterFields[stamps](map[string][]any{"Version": {tag{"v"}}})
	title := StringRefOf[doc, string]([][]int{{1}}, nil)
	version := OrderedRefOf[doc, int]([][]int{{0, 0}}, nil)
	ownerName := StringRefOf[doc, string]([][]int{{2}, {0}}, nil)
	bossName := StringRefOf[doc, string]([][]int{{3}, {0}}, nil)
	nick := NullableRefOf[doc, Option[string]]([][]int{{5}}, nil)

	d := doc{Title: "go", Owner: link[owner]{&owner{"ada"}}}
	if v, ok := version.Lookup(d); ok || v != 0 {
		t.Errorf("through a nil embedded pointer: %v %v", v, ok)
	}
	if got := ownerName.Get(d); got != "ada" {
		t.Errorf("through a relation: %q", got)
	}
	if _, ok := bossName.Lookup(d); ok {
		t.Error("through a nil pointer: found")
	}
	var a tag
	if !title.Attr(&a) || a.Name != "title" || version.Attrs()[0] != (tag{"v"}) {
		t.Errorf("attributes: %v %v", a, version.Attrs())
	}
	if got := ownerName.Path(); !slices.Equal(got, []string{"Owner", "Name"}) {
		t.Errorf("path: %v", got)
	}
	if v, ok := ownerName.FieldPath().Value(&d); !ok || v != "ada" {
		t.Errorf("Value: %v %v", v, ok)
	}

	for _, c := range []struct {
		p    Pred[doc]
		want bool
		text string
	}{
		{title.Eq("go"), true, `Title = "go"`},
		{Not(title.StartsWith("r")), true, `NOT Title starts with "r"`},
		{bossName.Ne("x"), false, `Boss.Name != "x"`},
		{nick.IsNil(), true, `Nick IS NIL`},
		{Or(version.Ge(1), ownerName.In("ada", "bob")), true, `(Version >= 1 OR Owner.Name IN ("ada", "bob"))`},
	} {
		if got := c.p.Match(d); got != c.want || c.p.String() != c.text {
			t.Errorf("%s: %v, want %s: %v", c.p, got, c.text, c.want)
		}
	}

	docs := []doc{{Title: "b"}, {Title: "a", Owner: link[owner]{&owner{"z"}}}, {Title: "c", Owner: link[owner]{&owner{"m"}}}}
	SortBy(docs, ownerName.Desc(), title.Asc())
	var got []string
	for _, x := range docs {
		got = append(got, x.Title)
	}
	if !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Errorf("sorted: %v", got)
	}
}
