package main

import (
	"bytes"
	"strings"
	"testing"
)

const fixGraphQLOps = `package shop

import (
	"context"

	"github.com/vuka-lang/web/v2/graphql"
)

type Product struct{ Name string }

type Store struct{}

func NewStore() *Store { return &Store{} }

// Products are a category's products, after a cursor.
@graphql.Query
@graphql.AsInt("categoryId")
@graphql.Arg("after", "cursor")
func Products(ctx context.Context, categoryId int, after *string, first int, store *Store) ([]Product, error) {
	return nil, nil
}
`

const fixGraphQLMain = `package main

import (
	"example.com/app/shop"
	"github.com/vuka-lang/web/v2"
)

func main() {
	app := web.New(web.GraphQL("/graphql"))
	app.Service(shop.NewStore)
	app.Run("")
}
`

// TestFixGraphQL: vuka fix graphql-params writes the attributes above an
// operation before the parameters they name, and a second run has nothing
// to do; vuka fix graphql-args lists the required arguments, changing
// nothing.
func TestFixGraphQL(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	dir := refactorModule(t, map[string]string{"main.go": fixGraphQLMain, "shop/ops.vuka": fixGraphQLOps})
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out bytes.Buffer
	if err := fix([]string{"graphql-params"}, &out); err != nil {
		t.Fatalf("vuka fix: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "shop/ops.vuka: 2 attribute(s) written before their parameters") {
		t.Errorf("reported:\n%s", out.String())
	}
	want := strings.Replace(fixGraphQLOps, `@graphql.Query
@graphql.AsInt("categoryId")
@graphql.Arg("after", "cursor")
func Products(ctx context.Context, categoryId int, after *string,`, `@graphql.Query
func Products(ctx context.Context, @graphql.AsInt categoryId int, @graphql.Arg("cursor") after *string,`, 1)
	if got := readFile(t, "shop/ops.vuka"); got != want {
		t.Errorf("shop/ops.vuka:\n%s\nwant:\n%s", got, want)
	}
	out.Reset()
	if err := fix([]string{"graphql-params"}, &out); err != nil || !strings.Contains(out.String(), "nothing to fix") {
		t.Errorf("again: %v\n%s", err, out.String())
	}

	out.Reset()
	if err := fix([]string{"graphql-args"}, &out); err != nil {
		t.Fatalf("vuka fix graphql-args: %v\n%s", err, out.String())
	}
	for _, line := range []string{
		"graphql-args: shop/ops.vuka:17: Products's argument categoryId (int) is required: a request must give it; make it *int or Option[int] if it may be left out",
		"graphql-args: shop/ops.vuka:17: Products's argument first (int) is required",
		"graphql-args: 2 argument(s) listed; nothing changed",
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("no %q in:\n%s", line, out.String())
		}
	}
	if got := readFile(t, "shop/ops.vuka"); got != want {
		t.Errorf("graphql-args changed shop/ops.vuka:\n%s", got)
	}
	vets(t, dir)
}
