package transpile_test

import (
	"os"
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
		"W0802 graphql_loaders.vuka:77:6: Everyone returns a list without a limit",
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
