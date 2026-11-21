package transpile

import (
	"slices"
	"testing"
)

// TestMethodTables: a type the runtime renders or registers has its
// exported methods, any other type those named by a convention, each as
// declared; generic types, unexported methods and types the sources don't
// declare have none.
func TestMethodTables(t *testing.T) {
	got := MethodTables([]byte(`package p

import "github.com/vuka-lang/vuka/v2"

type Page struct{}

func (p *Page) Render() any       { return nil }
func (p *Page) Add(n int) error   { return nil }
func (p Page) Title() string      { return "" }
func (p *Page) hidden()           {}

type Form struct{}

func (f *Form) CleanEmail() error   { return nil }
func (f Form) RoleChoices() []string { return nil }
func (f *Form) Init() error         { return nil }
func (f *Form) Submit() error       { return nil }

type Model struct{}

func (Model) LegacyTableName() string { return "" }
func (*Model) UpdateOrder(o int)      {}

type Box[T any] struct{}

func (b *Box[T]) Render() any { return nil }

type Island struct{}

type Card struct{}

func (c Card) View() any                    { return nil }
func (c Card) Render(ctx any, w any) error  { return nil }

type Stats struct{}

func (s *Stats) Load() (int, error) { return 0, nil }
func (s *Stats) Other()             {}

func init() { vuka.RegisterFunc((*Stats).Load) }

type Session struct{}

func (s *Session) Render() (string, error) { return "", nil }
func (s *Session) Close()                  {}

func init() { vuka.RegisterType[Island]() }
`), []byte(`package p

func (i *Island) Pick(n int) {}

func (x *Elsewhere) Render() any { return nil }
`))
	want := []string{
		"vuka.RegisterMethods[Card](Card.Render, Card.View)",
		"vuka.RegisterMethods[Form]((*Form).CleanEmail, (*Form).Init, Form.RoleChoices)",
		"vuka.RegisterMethods[Island]((*Island).Pick)",
		"vuka.RegisterMethods[Model](Model.LegacyTableName, (*Model).UpdateOrder)",
		"vuka.RegisterMethods[Page]((*Page).Add, (*Page).Render, Page.Title)",
		"vuka.RegisterMethods[Stats]((*Stats).Load, (*Stats).Other)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("MethodTables =\n%q\nwant\n%q", got, want)
	}
}
