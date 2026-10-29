package orm

import "time"

type Cond interface{ cond() }

type Expr interface{ expr() }

type q struct{}

func (q) cond() {}
func (q) expr() {}

type Manager[T any] struct{ QuerySet[T] }

type QuerySet[T any] struct{}

func Objects[T any]() *Manager[T] { return &Manager[T]{} }

func (qs QuerySet[T]) Join(j Joinable, kind ...JoinKind) QuerySet[T] { return qs }
func (qs QuerySet[T]) Filter(conds ...Cond) QuerySet[T]              { return qs }
func (qs QuerySet[T]) OrderBy(fields ...any) QuerySet[T]             { return qs }
func (qs QuerySet[T]) All(ctx any) ([]T, error)                      { return nil, nil }
func (qs QuerySet[T]) Values[R any](cols ...any) Values[T, R]        { return Values[T, R]{} }
func (vq Values[T, R]) All(ctx any) ([]R, error)                     { return nil, nil }
func (vq Values[T, R]) OrderBy(fields ...any) Values[T, R]           { return vq }

type Values[T, R any] struct{}

type JoinKind int

const (
	Inner JoinKind = iota
	Left
)

type Joinable interface{ joinable() }

type JoinOn struct{}

func (JoinOn) joinable() {}

type Source struct{}

type RelationPath struct{}

func (RelationPath) joinable()               {}
func (RelationPath) On(conds ...Cond) JoinOn { return JoinOn{} }
func (RelationPath) Eq(v any) Cond           { return q{} }
func (RelationPath) Source() *Source         { return nil }
func (*Source) As(alias string) *Source      { return nil }

type ColumnOf[V any] interface{ column(V) }

type Field[V any] struct{}

func (Field[V]) column(V)                 {}
func (Field[V]) expr()                    {}
func (Field[V]) Eq(v V) Cond              { return q{} }
func (Field[V]) EqCol(c ColumnOf[V]) Cond { return q{} }
func (Field[V]) In(vs ...V) Cond          { return q{} }
func (Field[V]) Desc() string             { return "" }

type TextField[V ~string] struct{ Field[V] }
type NumberField[V any] struct{ Field[V] }
type TimeField struct{ Field[time.Time] }

func (TimeField) Gte(v any) Cond { return q{} }

type AggOption int

const Distinct AggOption = 1

type Agg struct{ q }

func Count(field any, opts ...AggOption) Agg { return Agg{} }
func Sum(field any, opts ...AggOption) Agg   { return Agg{} }
func Avg(field any, opts ...AggOption) Agg   { return Agg{} }

type SQLExpr[T any] struct{ q }

type DialectName string

const (
	MySQL    DialectName = "mysql"
	Postgres DialectName = "postgres"
	SQLite   DialectName = "sqlite"
)

type Dialects map[DialectName]string

func SQLOf[T any](template string, args ...any) SQLExpr[T] { return SQLExpr[T]{} }
func SQLFor[T any](per Dialects, args ...any) SQLExpr[T]   { return SQLExpr[T]{} }

func Today() q { return q{} }
