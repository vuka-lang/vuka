// Package streamui is a stand-in for github.com/vuka-lang/ui/v2's Stream in
// the golden cases: the methods the compiler knows a stream by.
package streamui

import "github.com/vuka-lang/ui/v2"

// Stream is a list a live page sends and forgets.
type Stream[T any] struct{ rows []T }

func (s *Stream[T]) AssignDirty() *bool          { return new(bool) }
func (s *Stream[T]) Insert(rows ...T)            { s.rows = append(s.rows, rows...) }
func (s *Stream[T]) Prepend(rows ...T)           { s.rows = append(rows, s.rows...) }
func (s *Stream[T]) InsertAt(i int, rows ...T)   {}
func (s *Stream[T]) Delete(keys ...any)          {}
func (s *Stream[T]) Reset(rows ...T)             { s.rows = rows }
func (s *Stream[T]) Limit(n int)                 {}
func (s *Stream[T]) Rows() []T                   { return s.rows }
func (s *Stream[T]) Block(rows []ui.Node) *Block { return &Block{ui.Fragment(rows...)} }

// Block is a stream's rows in one render.
type Block struct{ ui.Node }

func (b *Block) Attrs() []ui.Attr { return nil }
