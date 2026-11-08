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

func ModelLabel(field any, outer ...any) Node { return Child("label of " + path(field, outer)) }

func FieldsetAttrs(field any, outer ...any) []Attr {
	return []Attr{{Name: "data-fieldset", Value: path(field, outer)}}
}

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

// Form takes a key, as a form's identity, and its handlers.
type Form struct {
	For          any
	Key          any
	OnSubmit     any
	OnChange     any
	OnSuccess    any
	OnError      any
	OnInvalid    any
	OnReset      any
	NoSummary    bool
	Bound        []string
	ID           string
	Class        string
	Validate     string
	Keep         bool
	Eager        bool
	ConfirmLeave bool
	Attrs        []Attr
	Children     Node
}

// FormElement is what a <form for={…}> is: Form, its class the page's.
type FormElement Form

func (f FormElement) View() Node {
	attrs := []Attr{{Name: "data-element", Value: true}, {Name: "class", Value: f.Class}, {Name: "data-validate", Value: f.Validate}, {Name: "data-keep", Value: f.Keep}}
	attrs = append(attrs, f.Attrs...)
	return El("div", attrs, Form(f).View())
}

func (f FormElement) Render(ctx context.Context, w io.Writer) error { return f.View().Render(ctx, w) }

func (f Form) View() Node {
	attrs := []Attr{{Name: "data-for", Value: fmt.Sprint(f.For)}, {Name: "data-key", Value: fmt.Sprint(f.Key)}}
	if f.Bound != nil {
		attrs = append(attrs, Attr{Name: "data-bound", Value: strings.Join(f.Bound, ",")})
	}
	return El("form", attrs, f.Children)
}

// FormFor is a form of a T, its values in Value.
type FormFor[T any] struct {
	FormState
	Value T
}

func (FormFor[T]) vukaFormFor() {}

func (f *FormFor[T]) Load(v T)                       { f.Value = v }
func (f *FormFor[T]) Save(ctx context.Context) error { return nil }

// The form field attributes.
type (
	Label string
	Max   float64
)

func (f Form) Render(ctx context.Context, w io.Writer) error { return f.View().Render(ctx, w) }

// ModelForm and FormOf are a form over a model's named fields.
type ModelForm[T any] struct {
	FormState
	Value T
}

func FormOf[T any](fields ...string) *ModelForm[T] { return &ModelForm[T]{} }
