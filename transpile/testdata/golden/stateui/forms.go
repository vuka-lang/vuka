package stateui

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// The forms API in the shape the compiler lowers model and for to: each
// renders the path of the field its pointers name, found as the real one
// finds it — among the values holding the field, by address.

type FormState struct{}

type ModelSpec struct {
	Tag      string
	Type     string
	Value    any
	HasValue bool
	Mods     string
	Own      string
}

// path is the field's name in the first value holding it.
func path(field any, outer []any) string {
	p := reflect.ValueOf(field).Pointer()
	for _, o := range outer {
		v := reflect.ValueOf(o)
		for v.Kind() == reflect.Pointer && v.Elem().Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
			continue
		}
		st := v.Elem()
		for i := 0; i < st.NumField(); i++ {
			if st.Field(i).Addr().Pointer() == p && st.Type().Field(i).Type.Size() > 0 {
				return st.Type().Name() + "." + st.Type().Field(i).Name
			}
		}
	}
	return "?"
}

func Model(spec ModelSpec, field any, outer ...any) []Attr {
	s := fmt.Sprintf("%s tag=%s", path(field, outer), spec.Tag)
	if spec.Type != "" {
		s += " type=" + spec.Type
	}
	if spec.HasValue {
		s += fmt.Sprintf(" value=%v", spec.Value)
	}
	if spec.Mods != "" {
		s += " mods=" + spec.Mods
	}
	if spec.Own != "" {
		s += " own=" + strings.ReplaceAll(spec.Own, " ", "+")
	}
	return []Attr{{Name: "data-model", Value: s}}
}

func ModelID(field any, outer ...any) any { return "id:" + path(field, outer) }

func ModelText(field any, outer ...any) string {
	return fmt.Sprint(reflect.ValueOf(field).Elem().Interface())
}

func ModelOptions(field any, outer ...any) Node {
	return Fragment(El("option", []Attr{{Name: "value", Value: "a"}}, Child("choices of "+path(field, outer))))
}

func ModelSelected(v any, field any, outer ...any) bool {
	return fmt.Sprint(reflect.ValueOf(field).Elem().Interface()) == fmt.Sprint(v)
}

type FieldRef struct{ Path string }

func For(field any, outer ...any) FieldRef {
	if len(outer) == 0 {
		return FieldRef{"form " + reflect.TypeOf(field).Elem().Name()}
	}
	return FieldRef{path(field, outer)}
}

// Field is a form component taking a reference.
type Field struct {
	For   any
	Label string
}

func (f Field) View() Node {
	return El("p", nil, Child(fmt.Sprintf("field %v %s", f.For, f.Label)))
}

func (f Field) Render(ctx context.Context, w io.Writer) error { return f.View().Render(ctx, w) }

// Form takes a key, as a form's identity.
type Form struct {
	For      any
	Key      any
	Children Node
}

func (f Form) View() Node {
	return El("form", []Attr{{Name: "data-for", Value: fmt.Sprint(f.For)}, {Name: "data-key", Value: fmt.Sprint(f.Key)}}, f.Children)
}

func (f Form) Render(ctx context.Context, w io.Writer) error { return f.View().Render(ctx, w) }
