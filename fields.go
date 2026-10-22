package vuka

import (
	"reflect"
	"sync"
)

var fieldAttrs sync.Map // reflect.Type of a struct → map[string][]any

// RegisterFields records the typed attributes of T's fields, by field name,
// for FieldAttrs to read: Vuka's field attributes on a struct with no
// schema method to hold them (orm.Base's Schema, ui.Live's LiveSchema),
// written at init.
//
//	func init() {
//		vuka.RegisterFields[Address](map[string][]any{"City": {Char{Max: 50}}})
//	}
func RegisterFields[T any](attrs map[string][]any) { fieldAttrs.Store(reflect.TypeFor[T](), attrs) }

// FieldAttrs is the typed attributes of T's fields, by field name: for
//
//	type Post struct {
//		Title string @Char{Max: 200}
//	}
//
// FieldAttrs[Post]()["Title"] is []any{Char{Max: 200}}. It is nil for a type
// whose fields carry none. It holds the attributes RegisterFields recorded
// and those a FieldAttrsDeclarer declares: a model's Schema method's
// (s.Field(&p.Title, orm.Char{Max: 200})), with its model attributes on
// its embedded orm.Model field, where a Vuka model writes them.
func FieldAttrs[T any]() map[string][]any { return FieldAttrsOf(reflect.TypeFor[T]()) }

// FieldAttrsOf is FieldAttrs for a reflect.Type.
func FieldAttrsOf(t reflect.Type) map[string][]any {
	m, _ := fieldAttrs.Load(t)
	attrs, _ := m.(map[string][]any)
	if t.Kind() == reflect.Struct && reflect.PointerTo(t).Implements(declarerType) {
		return reflect.New(t).Interface().(FieldAttrsDeclarer).DeclaredFieldAttrs(t, attrs)
	}
	return attrs
}

// FieldAttrsDeclarer is a struct whose fields' attributes are declared
// another way than RegisterFields: by a library's schema method, as a
// model's are in its Schema method (orm.Model and orm.Base implement it).
// FieldAttrsOf asks it for t's — t being the struct or one embedding it —
// given what RegisterFields recorded for t, which it returns with the
// attributes it declares after them, or as they are when t isn't its own.
// It is asked of a zero t: a method of a pointer receiver it doesn't read
// is safe behind a nil embedded pointer.
type FieldAttrsDeclarer interface {
	DeclaredFieldAttrs(t reflect.Type, registered map[string][]any) map[string][]any
}

var declarerType = reflect.TypeFor[FieldAttrsDeclarer]()

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
			Injected: sf.Name != "_" && (sf.Type.Kind() == reflect.Pointer || sf.Type.Kind() == reflect.Interface)}
	}
	return fields
}

// Attr fills ptr with the field's typed attribute of ptr's element type,
// reporting whether there is one: var c Char; f.Attr(&c).
func (f Field) Attr(ptr any) bool { return fillAttr(f.Attrs, ptr) }
