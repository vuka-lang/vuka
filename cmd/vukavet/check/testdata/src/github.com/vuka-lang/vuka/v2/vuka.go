package vuka

type Result[T any] struct{ v T }

func (r Result[T]) Get() (T, error) { return r.v, nil }

func RegisterParams(fn any, names ...string) {}

func RegisterFunc(fn any, attrs ...any) {}
