// Package orm is a small stand-in for github.com/vuka-lang/orm/v2 in the golden
// cases: Base's markers, Schema, Objects, typed fields and Transaction, in
// memory.
package orm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Base is a model's embedded base.
type Base[Self any] struct{}

func (Base[Self]) vukaSchema(interface{ Schema(*Schema) }) {}
func (Base[Self]) vukaFields()                             {}
func (Base[Self]) vukaPlain(Model[Self])                   {}

// Model is what plain Go embeds in Base's place, with orm's Objects method:
// plain Go's User{}.Objects().
type Model[Self any] struct{ Base[Self] }

// Objects is the model's manager.
func (Model[Self]) Objects(names ...string) *Manager[Self] { return Objects[Self](names...) }

// Save stores the row.
func (b *Base[Self]) Save(self *Self, ctx context.Context) error {
	m := Objects[Self]()
	m.rows = append(m.rows, *self)
	return nil
}

type ModelAttr interface{ ApplyModel(*string) }
type FieldAttr interface{ Apply(*[]string) }

type Meta struct{ Table string }

func (m Meta) ApplyModel(t *string) { *t = m.Table }

type (
	PK     struct{}
	Char   struct{ Max int }
	Column string
)

func (PK) Apply(o *[]string)       { *o = append(*o, "pk") }
func (c Char) Apply(o *[]string)   { *o = append(*o, fmt.Sprintf("char(%d)", c.Max)) }
func (c Column) Apply(o *[]string) { *o = append(*o, "column "+string(c)) }

// Schema collects a model's attributes, its fields named by address.
type Schema struct {
	root   reflect.Value
	table  string
	fields map[string][]string
}

func (s *Schema) Model(attrs ...ModelAttr) {
	for _, a := range attrs {
		a.ApplyModel(&s.table)
	}
}

func (s *Schema) Field(ptr any, attrs ...FieldAttr) {
	p := reflect.ValueOf(ptr).Pointer()
	t := s.root.Elem().Type()
	for i := range t.NumField() {
		if s.root.Pointer()+t.Field(i).Offset == p && t.Field(i).Type == reflect.TypeOf(ptr).Elem() {
			var o []string
			for _, a := range attrs {
				a.Apply(&o)
			}
			s.fields[t.Field(i).Name] = append(s.fields[t.Field(i).Name], o...)
			return
		}
	}
	panic("not a field")
}

// NewSchema is the Schema of the model x points to, for describe.
func NewSchema(x any) *Schema {
	return &Schema{root: reflect.ValueOf(x), fields: map[string][]string{}}
}

// Table and Attrs are what the Schema method declared.
func (s *Schema) Table() string              { return s.table }
func (s *Schema) Attrs() map[string][]string { return s.fields }

// Cond is a condition on rows.
type Cond struct {
	path string
	v    any
}

// Field is a typed field.
type Field[V any] struct{ Path string }

func (f Field[V]) Eq(v V) Cond  { return Cond{f.Path, v} }
func (f Field[V]) Desc() string { return "-" + f.Path }

// Manager is a model's rows.
type Manager[T any] struct{ rows []T }

type table interface {
	size() int
	cut(n int)
}

func (m *Manager[T]) size() int { return len(m.rows) }
func (m *Manager[T]) cut(n int) { m.rows = m.rows[:n] }

var managers = map[reflect.Type]table{}

// Objects is the model's manager.
func Objects[T any](names ...string) *Manager[T] {
	t := reflect.TypeFor[T]()
	if m, ok := managers[t]; ok {
		return m.(*Manager[T])
	}
	m := &Manager[T]{}
	managers[t] = m
	return m
}

var ErrNotFound = errors.New("not found")

// Get is the one row every condition holds for.
func (m *Manager[T]) Get(ctx context.Context, conds ...Cond) (T, error) {
	for _, r := range m.rows {
		if match(r, conds) {
			return r, nil
		}
	}
	var zero T
	return zero, ErrNotFound
}

func (m *Manager[T]) Count() int { return len(m.rows) }

// CountWhere counts the rows keep holds for.
func (m *Manager[T]) CountWhere(keep func(T) bool) int {
	n := 0
	for _, r := range m.rows {
		if keep(r) {
			n++
		}
	}
	return n
}

func match(row any, conds []Cond) bool {
	for _, c := range conds {
		v := reflect.ValueOf(row)
		for _, n := range strings.Split(c.path, "__") {
			for v.Kind() == reflect.Pointer {
				v = v.Elem()
			}
			v = v.FieldByNameFunc(func(s string) bool { return strings.EqualFold(s, n) })
		}
		if !v.IsValid() || v.Interface() != c.v {
			return false
		}
	}
	return true
}

// TxOption tunes a transaction.
type TxOption func(*tx)

type tx struct{ db string }

func TxOn(db string) TxOption { return func(t *tx) { t.db = db } }

// Transaction runs fn; its error rolls the rows back.
func Transaction(ctx context.Context, fn func(ctx context.Context) error, opts ...TxOption) error {
	var t tx
	for _, o := range opts {
		o(&t)
	}
	saved := map[reflect.Type]int{}
	for k, m := range managers {
		saved[k] = m.size()
	}
	err := fn(ctx)
	if err != nil {
		for k, m := range managers {
			m.cut(saved[k])
		}
	}
	fmt.Printf("tx on %q: %v\n", t.db, err)
	return err
}

// FK is a foreign key to a T: its key, and the row once loaded.
type FK[T any] struct {
	key any
	row *T
}

func (FK[T]) relTarget() reflect.Type { return reflect.TypeFor[T]() }
func (FK[T]) relMany() bool           { return false }
func (f FK[T]) Related() *T           { return f.row }
func (f FK[T]) Loaded() bool          { return f.row != nil }
func (f FK[T]) IsSet() bool           { return f.key != nil || f.row != nil }

// FKOf is an FK to a row.
func FKOf[T any](row *T) FK[T] { return FK[T]{row: row} }

// FKID is an FK to a key: its row "loads" as a zero T.
func FKID[T any](key any) FK[T] { return FK[T]{key: key} }

// M2M is a many-to-many to T.
type M2M[T any] struct {
	rows   []T
	loaded bool
}

func (M2M[T]) relTarget() reflect.Type { return reflect.TypeFor[T]() }
func (M2M[T]) relMany() bool           { return true }
func (m M2M[T]) Rows() []T             { return m.rows }
func (m M2M[T]) Loaded() bool          { return m.loaded }

// LoadRelation loads relation into rows: an FK set by key gets a zero row,
// an M2M is marked loaded. It prints what it loads.
func LoadRelation[T any](ctx context.Context, rows []*T, relation string) error {
	fmt.Printf("load %s of %d %s\n", relation, len(rows), reflect.TypeFor[T]().Name())
	for _, r := range rows {
		f := reflect.ValueOf(r).Elem().FieldByName(relation)
		switch x := f.Addr().Interface().(type) {
		case interface{ load() }:
			x.load()
		}
	}
	return nil
}

func (f *FK[T]) load() {
	if f.key != nil && f.row == nil {
		f.row = new(T)
	}
}

func (m *M2M[T]) load() { m.loaded = true }
