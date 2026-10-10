package vuka

import (
	"reflect"
	"sync"
)

var fieldAttrs sync.Map // reflect.Type of a struct → map[string][]any

// RegisterFields records the typed attributes written on T's fields, by field
// name. Generated code calls it at init, for every struct with field
// attributes.
func RegisterFields[T any](attrs map[string][]any) { fieldAttrs.Store(reflect.TypeFor[T](), attrs) }

// FieldAttrs is the typed attributes of T's fields, by field name: for
//
//	type Post struct {
//		Title string @Char{Max: 200}
//	}
//
// FieldAttrs[Post]()["Title"] is []any{Char{Max: 200}}. It is nil for a type
// whose fields carry none.
func FieldAttrs[T any]() map[string][]any { return FieldAttrsOf(reflect.TypeFor[T]()) }

// FieldAttrsOf is FieldAttrs for a reflect.Type.
func FieldAttrsOf(t reflect.Type) map[string][]any {
	m, _ := fieldAttrs.Load(t)
	attrs, _ := m.(map[string][]any)
	return attrs
}

// FieldsOf describes the fields of the struct T, with their tags and
// attributes. It is nil when T isn't a struct.
func FieldsOf[T any]() []Field { return fieldsOf(reflect.TypeFor[T]()) }

func fieldsOf(t reflect.Type) []Field {
	if t.Kind() != reflect.Struct {
		return nil
	}
	attrs := FieldAttrsOf(t)
	fields := make([]Field, t.NumField())
	for i := range fields {
		sf := t.Field(i)
		fields[i] = Field{Name: sf.Name, Type: sf.Type, Tag: sf.Tag, Attrs: attrs[sf.Name],
			Injected: sf.Name != "_" && sf.Tag.Get("inject") != "-"}
	}
	return fields
}

// Attr fills ptr with the field's typed attribute of ptr's element type,
// reporting whether there is one: var c Char; f.Attr(&c).
func (f Field) Attr(ptr any) bool { return fillAttr(f.Attrs, ptr) }
