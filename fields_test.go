package vuka_test

import (
	"reflect"
	"testing"

	"github.com/vuka-lang/vuka"
)

type maxLen int

// schemaBase stands for a library's embedded base type declaring its
// owner's field attributes, as orm.Model does from a Schema method.
type schemaBase[T any] struct{}

func (*schemaBase[T]) DeclaredFieldAttrs(t reflect.Type, registered map[string][]any) map[string][]any {
	if t != reflect.TypeFor[T]() {
		return registered
	}
	out := map[string][]any{"Title": {maxLen(200)}}
	for k, v := range registered {
		out[k] = append(append([]any(nil), v...), out[k]...)
	}
	return out
}

type article struct {
	schemaBase[article]
	Title string
	Slug  string
}

type featured struct {
	article
	Rank int
}

func init() {
	vuka.RegisterFields[article](map[string][]any{"Slug": {maxLen(50)}, "Title": {"required"}})
}

func TestFieldAttrsOfDeclarer(t *testing.T) {
	got := vuka.FieldAttrs[article]()
	want := map[string][]any{"Title": {"required", maxLen(200)}, "Slug": {maxLen(50)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FieldAttrs[article]() = %v, want %v", got, want)
	}
	var m maxLen
	if f := vuka.FieldsOf[article](); !f[1].Attr(&m) || m != 200 {
		t.Errorf("FieldsOf[article]()[1].Attr = %v", m)
	}
	if got := vuka.FieldAttrs[featured](); got != nil {
		t.Errorf("a struct embedding the declarer has %v, want what it registered (none)", got)
	}
}
