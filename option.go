package vuka

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// Option is a value that may be absent. The zero Option is None.
type Option[T any] struct {
	value T
	ok    bool
}

// Some is a present value.
func Some[T any](v T) Option[T] { return Option[T]{value: v, ok: true} }

// None is an absent value. Vuka code writes plain None; the transpiler fills
// in T from where the value goes.
func None[T any]() Option[T] { return Option[T]{} }

// FromPtr is None for a nil pointer and Some(*p) otherwise.
func FromPtr[T any](p *T) Option[T] {
	if p == nil {
		return Option[T]{}
	}
	return Option[T]{value: *p, ok: true}
}

func (o Option[T]) IsSome() bool { return o.ok }
func (o Option[T]) IsNone() bool { return !o.ok }

// Value is the value, or T's zero value when o is None.
func (o Option[T]) Value() T { return o.value }

// Get is Go's comma-ok form: v, ok := o.Get().
func (o Option[T]) Get() (T, bool) { return o.value, o.ok }

// Unwrap is the value; it panics when o is None.
func (o Option[T]) Unwrap() T {
	if !o.ok {
		panic("vuka: Unwrap of None")
	}
	return o.value
}

// UnwrapOr is the value, or def when o is None.
func (o Option[T]) UnwrapOr(def T) T {
	if !o.ok {
		return def
	}
	return o.value
}

func (o Option[T]) String() string {
	if !o.ok {
		return "None"
	}
	return fmt.Sprintf("Some(%v)", o.value)
}

// MarshalJSON writes the value, or null for None.
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if !o.ok {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON reads a value as Some, and null as None.
func (o *Option[T]) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*o = Option[T]{}
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*o = Option[T]{value: v, ok: true}
	return nil
}

// IsZero reports whether o is None: a field tagged json:",omitzero" leaves
// a None out.
func (o Option[T]) IsZero() bool { return !o.ok }

// MapOption applies f to a present value.
func MapOption[T, U any](o Option[T], f func(T) U) Option[U] {
	if !o.ok {
		return Option[U]{}
	}
	return Option[U]{value: f(o.value), ok: true}
}

// MarshalText is the text of the value: T's MarshalText, else a string, a
// bool or a number as strconv writes it; a T with neither is an error. None
// has no text, and is written as the empty text: a caller that must tell it
// from Some("") asks IsNone.
func (o Option[T]) MarshalText() ([]byte, error) {
	if !o.ok {
		return nil, nil
	}
	if m, ok := any(o.value).(encoding.TextMarshaler); ok {
		return m.MarshalText()
	}
	if m, ok := any(&o.value).(encoding.TextMarshaler); ok {
		return m.MarshalText()
	}
	v := reflect.ValueOf(o.value)
	switch v.Kind() {
	case reflect.String:
		return []byte(v.String()), nil
	case reflect.Bool:
		return strconv.AppendBool(nil, v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(nil, v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.AppendUint(nil, v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.AppendFloat(nil, v.Float(), 'g', -1, v.Type().Bits()), nil
	}
	return nil, fmt.Errorf("vuka: Option[%T] has no text form", o.value)
}

// UnmarshalText reads text as a T, as MarshalText writes it, and sets Some:
// the empty text is parsed like any other, so it is Some("") for a string
// and an error for a number. A parameter that is absent is not text at all,
// and leaves a None as it is.
func (o *Option[T]) UnmarshalText(b []byte) error {
	var v T
	if u, ok := any(&v).(encoding.TextUnmarshaler); ok {
		if err := u.UnmarshalText(b); err != nil {
			return err
		}
		*o = Option[T]{value: v, ok: true}
		return nil
	}
	rv := reflect.ValueOf(&v).Elem()
	s := string(b)
	switch rv.Kind() {
	case reflect.String:
		rv.SetString(s)
	case reflect.Bool:
		x, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		rv.SetBool(x)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		x, err := strconv.ParseInt(s, 10, rv.Type().Bits())
		if err != nil {
			return err
		}
		rv.SetInt(x)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		x, err := strconv.ParseUint(s, 10, rv.Type().Bits())
		if err != nil {
			return err
		}
		rv.SetUint(x)
	case reflect.Float32, reflect.Float64:
		x, err := strconv.ParseFloat(s, rv.Type().Bits())
		if err != nil {
			return err
		}
		rv.SetFloat(x)
	default:
		return fmt.Errorf("vuka: Option[%T] has no text form", v)
	}
	*o = Option[T]{value: v, ok: true}
	return nil
}
