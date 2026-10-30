package transpile_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestGraphQLVet: the warnings vuka vet prints for a schema — a field
// method of a type a list reaches querying the orm itself (W0801), and an
// operation or field method returning a list without a limit (W0802).
func TestGraphQLVet(t *testing.T) {
	src, err := os.ReadFile("testdata/golden/graphql_loaders.vuka")
	if err != nil {
		t.Fatal(err)
	}
	res, err := transpile.Package([]transpile.File{{Name: "graphql_loaders.vuka", Src: src}},
		transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range res.Files {
		for _, w := range o.Warnings {
			got = append(got, string(w.Code)+" "+w.Error())
		}
	}
	want := []string{
		"W0802 graphql_loaders.vuka:58:20: Customer.Orders returns a list without a limit",
		"W0801 graphql_loaders.vuka:63:20: Customer.Recent queries the database once for each Customer of a list (an N+1)",
		"W0802 graphql_loaders.vuka:63:20: Customer.Recent returns a list without a limit",
		"W0802 graphql_loaders.vuka:76:6: Everyone returns a list without a limit",
	}
	joined := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("no warning %q in:\n%s", w, joined)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d warnings, want %d:\n%s", len(got), len(want), joined)
	}
}

// TestGraphQLArgAttrsAbove: an argument's attribute above the declaration,
// the older form, still works and is a warning (W0803) whose fix writes it
// before each parameter it names.
func TestGraphQLArgAttrsAbove(t *testing.T) {
	src := `package main

import (
	"context"

	"github.com/vuka-lang/web/v2/graphql"
)

type Order struct{ ID int64 }

@graphql.Query
@graphql.AsInt("lastID", "shelfID")
@graphql.Arg("after", "cursor")
func Orders(ctx context.Context, lastID int, shelfID *int, after *string) ([]Order, error) { return nil, nil }

func main() {}
`
	want := `package main

import (
	"context"

	"github.com/vuka-lang/web/v2/graphql"
)

type Order struct{ ID int64 }

@graphql.Query
func Orders(ctx context.Context, @graphql.AsInt lastID int, @graphql.AsInt shelfID *int, @graphql.Arg("cursor") after *string) ([]Order, error) { return nil, nil }

func main() {}
`
	compile := func(src string) (*transpile.Result, error) {
		return transpile.Package([]transpile.File{{Name: "orders.vuka", Src: []byte(src)}},
			transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	}
	res, err := compile(src)
	if err != nil {
		t.Fatal(err)
	}
	var edits []transpile.Edit
	var got []string
	for _, w := range res.Files[0].Warnings {
		if w.Code == "W0803" {
			got = append(got, w.Error())
			edits = append(edits, w.Edits...)
		}
	}
	if len(got) != 2 || got[0] != `orders.vuka:12:1: Orders: write @graphql.AsInt("lastID", "shelfID") before the parameter it is about; func Orders(…@graphql.AsInt lastID …)` {
		t.Fatalf("warnings:\n%s", strings.Join(got, "\n"))
	}
	slices.SortFunc(edits, func(a, b transpile.Edit) int { return b.Off - a.Off })
	fixed := src
	for _, e := range edits {
		fixed = fixed[:e.Off] + e.Text + fixed[e.End:]
	}
	if fixed != want {
		t.Fatalf("fixed:\n%s", fixed)
	}
	res, err = compile(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if ws := res.Files[0].Warnings; len(ws) != 1 || ws[0].Code != "W0802" {
		t.Errorf("warnings once fixed: %v", ws)
	}
	before, after := string(res.Files[0].Src), ""
	if res, err := compile(src); err == nil {
		after = string(res.Files[0].Src)
	}
	reg := func(s string) string {
		s = s[strings.Index(s, "graphql.Register("):]
		return strings.Replace(s[:strings.Index(s, "\n\t\t\t\tType:")], `, Arg: "cursor"`, "", 1)
	}
	if strings.Contains(before, "Register(") && reg(before) != reg(after) {
		t.Errorf("the registrations differ:\n%s\nfrom above:\n%s", reg(before), reg(after))
	}
}
