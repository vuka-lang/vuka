// Package vuka is the runtime of Vuka programs: Result and Option.
//
// Vuka code writes Result[User], Ok(u), Err(e), Some(x) and None unqualified; the
// transpiler points them here. Go code uses the package like any other.
package vuka

import "fmt"

// Result is a value or an error. It is Ok when it holds no error; the zero
// Result is Ok with T's zero value.
type Result[T any] struct {
	value T
	err   error
}

// Ok is a successful Result.
func Ok[T any](v T) Result[T] { return Result[T]{value: v} }

// Err is a failed Result. err must not be nil.
func Err[T any](err error) Result[T] {
	if err == nil {
		panic("vuka: Err(nil)")
	}
	return Result[T]{err: err}
}

// Of turns Go's (T, error) into a Result: vuka.Of(os.ReadFile(name)).
func Of[T any](v T, err error) Result[T] {
	if err != nil {
		return Result[T]{err: err}
	}
	return Result[T]{value: v}
}

func (r Result[T]) IsOk() bool  { return r.err == nil }
func (r Result[T]) IsErr() bool { return r.err != nil }

// Value is the value, or T's zero value when r failed.
func (r Result[T]) Value() T { return r.value }

// Err is the error, or nil when r succeeded.
func (r Result[T]) Err() error { return r.err }

// Get turns r back into Go's (T, error).
func (r Result[T]) Get() (T, error) { return r.value, r.err }

// Unwrap is the value; it panics when r failed.
func (r Result[T]) Unwrap() T {
	if r.err != nil {
		panic(fmt.Sprintf("vuka: Unwrap of Err(%v)", r.err))
	}
	return r.value
}

// UnwrapOr is the value, or def when r failed.
func (r Result[T]) UnwrapOr(def T) T {
	if r.err != nil {
		return def
	}
	return r.value
}

func (r Result[T]) String() string {
	if r.err != nil {
		return fmt.Sprintf("Err(%v)", r.err)
	}
	return fmt.Sprintf("Ok(%v)", r.value)
}

// Map applies f to an Ok value.
func Map[T, U any](r Result[T], f func(T) U) Result[U] {
	if r.err != nil {
		return Result[U]{err: r.err}
	}
	return Result[U]{value: f(r.value)}
}

// AndThen chains a step that can fail.
func AndThen[T, U any](r Result[T], f func(T) Result[U]) Result[U] {
	if r.err != nil {
		return Result[U]{err: r.err}
	}
	return f(r.value)
}
