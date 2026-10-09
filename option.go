package vuka

import "fmt"

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

// MapOption applies f to a present value.
func MapOption[T, U any](o Option[T], f func(T) U) Option[U] {
	if !o.ok {
		return Option[U]{}
	}
	return Option[U]{value: f(o.value), ok: true}
}
