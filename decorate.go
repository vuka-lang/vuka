package vuka

import (
	"reflect"
	"sync"
)

// Decorated holds a decorated function, built from its decorators on first
// use. Vuka generates one per decorated function or method.
type Decorated[F any] struct {
	once sync.Once
	f    F
}

// Get returns the decorated function, building it the first time.
func (d *Decorated[F]) Get(build func() F) F {
	d.once.Do(func() { d.f = build() })
	return d.f
}

// Instances holds a generic decorated function's instantiations, each built
// from its decorators on first use.
type Instances struct {
	m  sync.Map // reflect.Type of F → F
	mu sync.Mutex
}

// Instance returns the instantiation of a generic decorated function whose
// type is F, building it the first time.
func Instance[F any](s *Instances, build func() F) F {
	key := reflect.TypeOf((*F)(nil)).Elem()
	if v, ok := s.m.Load(key); ok {
		return v.(F)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m.Load(key); ok {
		return v.(F)
	}
	f := build()
	s.m.Store(key, f)
	return f
}
