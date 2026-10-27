// Package graphql stands in for github.com/vuka-lang/web/v2/graphql: the
// API the code generated for @graphql.Query and @graphql.Mutation calls,
// Register printing what it is given.
package graphql

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/vuka-lang/vuka/v2"
)

type Query string
type Mutation string
type Subscription string
type Type string
type Input string
type Field string
type Value string
type Deprecated string

type ArgName struct{ Param, Name string }

func Arg(param, name string) ArgName { return ArgName{param, name} }

type IDParams []string
type IntParams []string

func AsID(params ...string) vuka.Bundle  { return vuka.Compose(IDParams(params)) }
func AsInt(params ...string) vuka.Bundle { return vuka.Compose(IntParams(params)) }

type TypeDef interface{ String() string }

type named struct{ name string }

func (n named) String() string { return n.name }

type NonNull struct{ Of TypeDef }

func (n *NonNull) String() string { return n.Of.String() + "!" }

type List struct{ Of TypeDef }

func (l *List) String() string { return "[" + l.Of.String() + "]" }

var (
	Int, Float, String, Boolean, ID, DateTime TypeDef = named{"Int"}, named{"Float"}, named{"String"}, named{"Boolean"}, named{"ID"}, named{"DateTime"}
)

type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

func IntOf[T Integer]() TypeDef               { return Int }
func FloatOf[T ~float32 | ~float64]() TypeDef { return Float }
func StringOf[T ~string]() TypeDef            { return String }
func BooleanOf[T ~bool]() TypeDef             { return Boolean }
func IDOf[T ~string]() TypeDef                { return ID }
func IntIDOf[T Integer]() TypeDef             { return ID }
func TextScalar[T any]() TypeDef              { return named{reflect.TypeFor[T]().Name()} }
func ObjectOf[T any]() TypeDef                { return named{reflect.TypeFor[T]().Name()} }
func InputOf[T any]() TypeDef                 { return named{reflect.TypeFor[T]().Name() + " (input)"} }
func EnumOf[T comparable]() TypeDef           { return named{reflect.TypeFor[T]().Name() + " (enum)"} }
func ListOf[E any](of TypeDef) *List          { return &List{of} }

type Param struct {
	Name string
	Type TypeDef
	Dep  reflect.Type
}

type Call struct{ args []any }

func Get[T any](c *Call, i int) T     { return As[T](c.args[i]) }
func GetPtr[E any](c *Call, i int) *E { return Ptr[E](c.args[i]) }

func As[T any](v any) T {
	x, _ := v.(T)
	return x
}

func Ptr[E any](v any) *E {
	if x, ok := v.(E); ok {
		return &x
	}
	return nil
}

func Deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func OmitZero[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

func OmitEmpty[E any](xs []E) any {
	if len(xs) == 0 {
		return nil
	}
	return xs
}

type Operation struct {
	Go       string
	Mutation bool
	Func     any
	Doc      string
	Params   []Param
	Type     TypeDef
	Resolve  func(ctx context.Context, c *Call) (any, error)
}

type Binding interface{ describe() []string }

type Struct[T any] struct {
	Doc    string
	Fields []Member[T]
}

type Member[T any] struct {
	Go, Name, Doc string
	Type          TypeDef
	Get           func(src *T) any
	Params        []Param
	Resolve       func(ctx context.Context, src *T, c *Call) (any, error)
}

func (s Struct[T]) describe() []string {
	out := []string{"type " + reflect.TypeFor[T]().Name()}
	for _, f := range s.Fields {
		out = append(out, "  "+f.Go+params(f.Params)+": "+f.Type.String())
	}
	return out
}

type FormInput[T any] struct {
	Doc    string
	Fields []InputMember[T]
}

type InputMember[T any] struct {
	Go, Name, Doc string
	Type          TypeDef
	Set           func(dst *T, v any)
}

func (s FormInput[T]) describe() []string {
	out := []string{"input " + reflect.TypeFor[T]().Name()}
	for _, f := range s.Fields {
		out = append(out, "  "+f.Go+": "+f.Type.String())
	}
	return out
}

type Consts[T comparable] struct {
	Doc    string
	Values []Const[T]
}

type Const[T comparable] struct {
	Go    string
	Doc   string
	Value T
}

func (s Consts[T]) describe() []string {
	out := []string{"enum " + reflect.TypeFor[T]().Name()}
	for _, c := range s.Values {
		out = append(out, fmt.Sprintf("  %s = %v", c.Go, c.Value))
	}
	return out
}

type Package struct {
	Path       string
	Operations []Operation
	Types      []Binding
}

var pkgs []Package

// Register prints what it is given.
func Register(p Package) {
	pkgs = append(pkgs, p)
	Describe()
}

func RegisterValue[T comparable](v T, attrs ...any) {
	fmt.Printf("value %v: %v\n", v, attrs)
}

func params(ps []Param) string {
	var out []string
	for _, p := range ps {
		switch {
		case p.Dep != nil:
			out = append(out, p.Name+" <- "+p.Dep.String())
		case p.Type != nil:
			out = append(out, p.Name+": "+p.Type.String())
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "(" + strings.Join(out, ", ") + ")"
}

// Describe prints what is registered.
func Describe() {
	for _, p := range pkgs {
		fmt.Println("package", p.Path)
		for _, op := range p.Operations {
			kind := "query"
			if op.Mutation {
				kind = "mutation"
			}
			fmt.Printf("%s %s%s: %s %q\n", kind, op.Go, params(op.Params), op.Type, op.Doc)
		}
		for _, t := range p.Types {
			for _, l := range t.describe() {
				fmt.Println(l)
			}
		}
	}
}
