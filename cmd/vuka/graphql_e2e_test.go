package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const graphqlMain = `package main

import (
	"example.com/app/pets"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/graphql"
)

func main() {
	app := web.New(web.GraphQL("/graphql", graphql.Naming{Fields: graphql.SnakeCase}))
	app.Service(pets.NewStore)
	app.Run("")
}
`

const graphqlTypes = `package pets

// Pet is an animal for sale.
type Pet struct {
	ID        int64 ` + "`graphql:\",id\"`" + `
	Name      string
	BirthYear int
}

type Store struct{ pets []Pet }

func NewStore() *Store { return &Store{} }
`

const graphqlOps = `package pets

import (
	"context"

	"github.com/vuka-lang/web/v2/graphql"
)

// Pets lists the pets.
@graphql.Query
func Pets(ctx context.Context, first *int, store *Store) ([]Pet, error) { return store.pets, nil }
`

// TestGraphQLSchema: vuka graphql schema prints the schema the app builds —
// names decided by its options — writes it, and checks a changed one,
// failing on a breaking change only.
func TestGraphQLSchema(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "GraphQL") {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no GraphQL; set VUKA_WEB (and VUKA_UI)", web)
	}
	root := localModule(t, web, map[string]string{
		"main.go":       graphqlMain,
		"pets/types.go": graphqlTypes,
		"pets/ops.vuka": graphqlOps,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	for _, k := range []string{graphqlSchemaEnv, graphqlCheckEnv, "VUKA_TAILWIND", "VUKA_FRONTEND"} {
		t.Setenv(k, "")
	}
	t.Chdir(root)
	schema := func(args ...string) (string, error) {
		t.Helper()
		os.Unsetenv(graphqlSchemaEnv)
		os.Unsetenv(graphqlCheckEnv)
		var stdout, stderr bytes.Buffer
		err := graphqlCmd(append([]string{"schema"}, args...), &stdout, &stderr)
		if err != nil && !exitedWith(err) {
			t.Fatalf("vuka graphql schema %s: %v\n%s", strings.Join(args, " "), err, stderr.Bytes())
		}
		return stdout.String(), err
	}
	sdl, err := schema()
	want := `type Query {
  """Pets lists the pets."""
  pets(first: Int): [Pet!]!
}

"""Pet is an animal for sale."""
type Pet {
  id: ID!
  name: String!
  birth_year: Int!
}
`
	if err != nil || sdl != want {
		t.Fatalf("schema: %v\n%s\nwant:\n%s", err, sdl, want)
	}
	if _, err := schema("-o", "schema.graphql"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile("schema.graphql"); string(b) != want {
		t.Fatalf("schema.graphql:\n%s", b)
	}

	// Adding a field breaks nothing; making one nullable breaks clients.
	writeFile(t, "pets/types.go", strings.Replace(graphqlTypes, "BirthYear int", "BirthYear int\n\tNick *string", 1))
	if out, err := schema("-check", "schema.graphql"); err != nil || !strings.Contains(out, "Field Pet.nick was added") {
		t.Errorf("an added field: %v\n%s", err, out)
	}
	writeFile(t, "pets/types.go", strings.Replace(graphqlTypes, "Name      string", "Name      *string", 1))
	out, err := schema("-check", "schema.graphql")
	if !exitedWith(err) || !strings.Contains(out, "BREAKING Field Pet.name changed type from String! to String.") {
		t.Errorf("a breaking change: %v\n%s", err, out)
	}
}

// isExit reports whether err is the app's exit with a status.
func exitedWith(err error) bool {
	var exit *exec.ExitError
	var st exitStatus
	return errors.As(err, &exit) || errors.As(err, &st)
}

const shopMain = `package main

import (
	"example.com/app/shop"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/graphql"
)

func main() {
	app := web.New(web.GraphQL("/graphql", graphql.Introspection(true)))
	app.Service(shop.NewStore)
	app.Run("")
}
`

const shopTypes = `package shop

type Product struct {
	ID    int64
	Name  string
	Codes *[]*int // [Int]: null, or codes some of which are null
}

type Store struct{ byCategory map[int]*[]*Product }

func intp(n int) *int { return &n }

func NewStore() *Store {
	kettle := &Product{ID: 1, Name: "kettle", Codes: &[]*int{intp(10), nil, intp(2)}}
	return &Store{byCategory: map[int]*[]*Product{1: {kettle, nil}, 2: {}}}
}
`

const shopOps = `package shop

import (
	"context"
	"time"

	"github.com/vuka-lang/web/v2/graphql"
)

// Details are what the API tells of a product, each maybe unknown.
type Details struct {
	SKU      Option[string]
	Released Option[time.Time]
	Scores   []Option[int]
}

// ProductDetails are a product's details, null for an unknown product.
@graphql.Query
func ProductDetails(ctx context.Context, @graphql.AsInt productId Option[int]) (Option[Details], error) {
	if id, ok := productId.Get(); ok && id == 1 {
		return Some(Details{SKU: Some("K-1"), Released: Some(time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)), Scores: []Option[int]{Some(1), None, Some(3)}}), nil
	}
	return None, nil
}

// Products are every product, or null while the shop is closed.
@graphql.Query
func Products(ctx context.Context, open *bool, store *Store) (*[]*Product, error) {
	if open != nil && !*open {
		return nil, nil
	}
	return &[]*Product{{ID: 2, Name: "mug"}}, nil
}

// ProductsByCategory are a category's products: null for a category the
// shop doesn't have, a null for each product it no longer sells.
@graphql.Query
func ProductsByCategory(ctx context.Context, @graphql.AsInt categoryId int, store *Store) (*[]*Product, error) {
	return store.byCategory[categoryId], nil
}

// Count counts the codes given, nulls apart.
@graphql.Query
func Count(ctx context.Context, @graphql.Arg("of") codes []*int, first *int) (int, error) {
	n := 0
	for _, c := range codes {
		if c != nil {
			n++
		}
	}
	return n, nil
}
`

// TestGraphQLNullability: the compiler's schema for nullable list results
// (*[]*T is [T]), lists of nullable scalars ([]*int is [Int]), arguments
// required by their Go type (int is Int!, *int is Int) and Options (None is
// null), and the app answering with null lists, null items, None and Some,
// and validation errors.
func TestGraphQLNullability(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if bind, _ := os.ReadFile(filepath.Join(web, "graphql", "bind.go")); !bytes.Contains(bind, []byte("func ListOfPtr[")) {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no graphql.ListOfPtr; set VUKA_WEB (and VUKA_UI)", web)
	}
	root := localModule(t, web, map[string]string{
		"main.go":       shopMain,
		"shop/types.go": shopTypes,
		"shop/ops.vuka": shopOps,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	for _, k := range []string{graphqlSchemaEnv, graphqlCheckEnv, "VUKA_TAILWIND", "VUKA_FRONTEND"} {
		t.Setenv(k, "")
	}
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if err := graphqlCmd([]string{"schema"}, &stdout, &stderr); err != nil {
		t.Fatalf("vuka graphql schema: %v\n%s", err, stderr.Bytes())
	}
	want := `type Query {
  """ProductDetails are a product's details, null for an unknown product."""
  productDetails(productId: Int): Details
  """Products are every product, or null while the shop is closed."""
  products(open: Boolean): [Product]
  """
  ProductsByCategory are a category's products: null for a category the
  shop doesn't have, a null for each product it no longer sells.
  """
  productsByCategory(categoryId: Int!): [Product]
  """Count counts the codes given, nulls apart."""
  count(of: [Int]!, first: Int): Int!
}

"""A date and time, in RFC 3339: 2026-10-26T09:30:00Z."""
scalar DateTime @specifiedBy(url: "https://scalars.graphql.org/andimarek/date-time")

"""Details are what the API tells of a product, each maybe unknown."""
type Details {
  sku: String
  released: DateTime
  scores: [Int]!
}

type Product {
  id: ID!
  name: String!
  codes: [Int]
}
`
	if got := stdout.String(); got != want {
		t.Fatalf("schema:\n%s\nwant:\n%s", got, want)
	}

	if err := gen([]string{"-inplace", "./..."}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	app := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command("go", "build", "-o", app, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	os.Unsetenv(graphqlSchemaEnv) // set, the app prints its schema and exits
	os.Unsetenv(graphqlCheckEnv)
	base, logs := runApp(t, app, root, "VUKA_DEV=", "VUKA_ENV=")
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("app log:\n%s", logs)
		}
	})
	post := func(query string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"query": query})
		res, err := http.Post(base+"/graphql", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	for _, c := range []struct{ query, want string }{
		{`{ products(open: false) { name } }`, `{"data":{"products":null}}`},
		{`{ products { name codes } }`, `{"data":{"products":[{"name":"mug","codes":null}]}}`},
		{`{ productsByCategory(categoryId: 1) { id codes } }`, `{"data":{"productsByCategory":[{"id":"1","codes":[10,null,2]},null]}}`},
		{`{ productsByCategory(categoryId: 2) { id } }`, `{"data":{"productsByCategory":[]}}`},
		{`{ productsByCategory(categoryId: 3) { id } }`, `{"data":{"productsByCategory":null}}`},
		{`{ count(of: [1, null, 3]) }`, `{"data":{"count":2}}`},
		{`{ productDetails { sku } }`, `{"data":{"productDetails":null}}`},
		{`{ productDetails(productId: 1) { sku released scores } }`, `{"data":{"productDetails":{"sku":"K-1","released":"2026-10-30T09:30:00Z","scores":[1,null,3]}}}`},
	} {
		if got := post(c.query); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.query, got, c.want)
		}
	}
	for _, q := range []string{`{ productsByCategory { id } }`, `{ productsByCategory(categoryId: null) { id } }`, `{ count }`} {
		if got := post(q); !strings.Contains(got, "GRAPHQL_VALIDATION_FAILED") || strings.Contains(got, `"data"`) {
			t.Errorf("%s: %s", q, got)
		}
	}
}
