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
	title := StringRefTo(func(d *doc) *string { return &d.Title })
	version := OrderedRefTo(func(d *doc) *int { return &d.Version })
	ownerName := StringRefTo(func(d *doc) *string { return &d.Owner.Related().Name })
	bossName := StringRefTo(func(d *doc) *string { return &d.Boss.Name })
	nick := NullableRefTo(func(d *doc) *Option[string] { return &d.Nick })
	for _, c := range []struct {
		got  FieldPath
		want [][]int
	}{
		{title.FieldPath(), [][]int{{1}}}, {version.FieldPath(), [][]int{{0, 0}}}, {ownerName.FieldPath(), [][]int{{2}, {0}}},
		{bossName.FieldPath(), [][]int{{3}, {0}}}, {nick.FieldPath(), [][]int{{5}}},
		{StringRefOf[doc, string]([][]int{{2}, {0}}, nil).FieldPath(), [][]int{{2}, {0}}},
	} {
		if !slices.EqualFunc(c.got.Index(), c.want, slices.Equal) {
			t.Errorf("%s: %v, want %v", c.got, c.got.Index(), c.want)
		}
	}

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

type box[T any] struct {
	N   int
	V   T
	sub struct{ inner [2]int }
}

func TestRefToGeneric(t *testing.T) {
	ref := func() OrderedRef[box[string], int] { return OrderedRefTo(func(b *box[string]) *int { return &b.N }) }
	if got := ref().FieldPath().Index(); !slices.EqualFunc(got, [][]int{{0}}, slices.Equal) {
		t.Errorf("N: %v", got)
	}
	if got := RefTo(func(b *box[int]) *int { return &b.V }).FieldPath().Index(); !slices.EqualFunc(got, [][]int{{1}}, slices.Equal) {
		t.Errorf("V: %v", got)
	}
	if got := RefTo(func(b *box[int]) *[2]int { return &b.sub.inner }).FieldPath().Index(); !slices.EqualFunc(got, [][]int{{2}, {0}}, slices.Equal) {
		t.Errorf("sub.inner: %v", got)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("a function returning no field's address")
		}
	}()
	n := 0
	RefTo(func(b *box[int]) *int { return &n })
}

func BenchmarkRefTo(b *testing.B) {
	for i := 0; i < b.N; i++ {
		StringRefTo(func(d *doc) *string { return &d.Owner.Related().Name })
	}
}
