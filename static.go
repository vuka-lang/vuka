package vuka

import (
	"reflect"
	"sync"
)

// Static is the storage of a static field of a generic type: one per
// instantiation, so Model[User] and Model[Order] each have their own.
type Static[V any] struct {
	V V
}

// Statics holds one generic static field's instantiations.
type Statics struct {
	m  sync.Map // reflect.Type of the instantiated type → *Static[V]
	mu sync.Mutex
}

// StaticOf returns the static field of the instantiated type K, built by init
// on first use. Generated code calls it.
func StaticOf[K, V any](s *Statics, init func() V) *Static[V] {
	key := reflect.TypeOf((*K)(nil)).Elem()
	if v, ok := s.m.Load(key); ok {
		return v.(*Static[V])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m.Load(key); ok {
		return v.(*Static[V])
	}
	v := &Static[V]{V: init()}
	s.m.Store(key, v)
	return v
}
