package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// chainOrm is the shape of the orm's query chains: Join with a table's On
// conditions, typed columns, and the generic method Values[R].
const chainOrm = `package orm

import "context"

type Cond struct{}

type Col[T any] struct{}

func (Col[T]) Eq(v any) Cond        { return Cond{} }
func (Col[T]) Between(a, b T) Cond  { return Cond{} }
func (Col[T]) Gte(v T) Cond         { return Cond{} }

type JoinKind int

const (
	Inner JoinKind = iota
	Left
)

type Join struct{}

type Table struct{}

func (Table) On(conds ...Cond) Join { return Join{} }

type Option int

const Distinct Option = 1

type Agg struct{}

func Count(c any, opts ...Option) Agg { return Agg{} }

type Date struct{}

func Today() Date               { return Date{} }
func (Date) AddDays(n int) Date { return Date{} }

type QS[T any] struct{}

func (q QS[T]) Join(j Join, kind ...JoinKind) QS[T] { return q }
func (q QS[T]) OrderBy(cols ...any) QS[T]          { return q }
func (q QS[T]) Values[R any](cols ...any) VS[R]    { return VS[R]{} }

type VS[R any] struct{}

func (VS[R]) All(ctx context.Context) ([]R, error) { return nil, nil }
`

const chainModels = `
type CategoryRow struct{}

var Category struct {
	orm.Table
	ID      orm.Col[int64]
	Name    orm.Col[string]
	Objects orm.QS[CategoryRow]
}

var ProductCategory struct {
	orm.Table
	Category orm.Col[int64]
	Shelf    orm.Col[int64]
}

var Shelf struct {
	orm.Table
	ID orm.Col[int64]
}

var Product struct {
	orm.Table
	ID       orm.Col[int64]
	Shelf    orm.Col[int64]
	Status   orm.Col[int]
	ClosesAt orm.Col[orm.Date]
}
`

const chainBody = `	return Category.Objects.
		Join(ProductCategory.On(ProductCategory.Category.Eq(Category.ID)), orm.Left).
		Join(Shelf.On(ProductCategory.Shelf.Eq(Shelf.ID)), orm.Left).
		Join(Product.On(
			Product.Shelf.Eq(Shelf.ID),
			Product.Status.Between(2, 3),
			Product.ClosesAt.Gte(orm.Today().AddDays(-3)),
		), orm.Left).
		OrderBy(Category.Name).
		Values[CategoryCount](Category.ID, Category.Name, orm.Count(Product.ID, orm.Distinct)).
		All(ctx)
`

// chainVariants are complete chains the repair once broke, or might: the
// full query, each of its lines after a multi-line On, the generic call.
var chainVariants = map[string]string{
	"full": chainBody,
	"orderAfterOn": `	return Category.Objects.
		Join(Product.On(
			Product.Shelf.Eq(Shelf.ID),
		), orm.Left).
		OrderBy(Category.Name).
		Values[CategoryCount](Category.ID).
		All(ctx)
`,
	"valuesAfterOn": `	return Category.Objects.
		Join(Product.On(
			Product.Shelf.Eq(Shelf.ID),
		), orm.Left).
		Values[CategoryCount](Category.ID).
		All(ctx)
`,
	"joinAfterOn": `	return Category.Objects.
		Join(Product.On(
			Product.Shelf.Eq(Shelf.ID),
		), orm.Left).
		Join(Shelf.On(ProductCategory.Shelf.Eq(Shelf.ID)), orm.Left).
		Values[CategoryCount](Category.ID).
		All(ctx)
`,
	"allAfterValues": `	return Category.Objects.
		Values[CategoryCount](
			Category.ID,
			orm.Count(Product.ID, orm.Distinct),
		).
		All(ctx)
`,
	"firstJoin": `	return Category.Objects.
		Join(Product.On(Product.Shelf.Eq(Shelf.ID), Product.Status.Between(2, 3)), orm.Left).
		Values[CategoryCount](Category.ID).
		All(ctx)
`,
}

var modelRef = regexp.MustCompile(`(^|[^.\w])(ProductCategory|Category|Shelf|Product)\b`)

func chainFunc(name, body string) string {
	return "func " + name + "(ctx context.Context) ([]CategoryCount, error) {\n" + body + "}\n"
}

// TestCompletableChainAfterArgs: a complete chain whose line closes a
// multi-line argument list and ends with a dot takes no placeholder.
func TestCompletableChainAfterArgs(t *testing.T) {
	for name, body := range chainVariants {
		src := "package main\n\n" + chainFunc("f", body)
		if out, _ := completable([]byte(src)); string(out) != src {
			t.Errorf("%s: completable changed a complete chain:\n%s", name, out)
		}
	}
	typing := "package main\n\nfunc f() {\n\tx := Product.On(\n\t\ta,\n\t).\n\ty := 1\n}\n"
	if out, _ := completable([]byte(typing)); !strings.Contains(string(out), ")."+placeholder+"\n") {
		t.Errorf("no placeholder after a dot being typed:\n%s", out)
	}
}

// TestLSPChainAfterMultilineArgs: complete, saved files holding the query
// chains, their models in the same package and in another, have no
// diagnostics but the one each file makes on purpose.
func TestLSPChainAfterMultilineArgs(t *testing.T) {
	files := map[string]string{
		"orm/orm.vuka":           chainOrm,
		"entities/entities.vuka": "package entities\n\nimport \"lsptest/orm\"\n" + chainModels,
	}
	head := "import (\n\t\"context\"\n\n\t\"lsptest/orm\"\n)\n\ntype CategoryCount struct {\n\tID    int64\n\tName  string\n\tCount int\n}\n\n"
	same := "package same\n\n" + head + chainModels + "\nvar _ = sentinel\n"
	other := "package other\n\n" + strings.Replace(head, "\"lsptest/orm\"", "\"lsptest/entities\"\n\t\"lsptest/orm\"", 1) + "var _ = sentinel\n"
	for name, body := range chainVariants {
		same += "\n" + chainFunc(name, body)
		other += "\n" + chainFunc(name, modelRef.ReplaceAllString(body, "${1}entities.$2"))
	}
	for name, src := range map[string]string{"same/q.vuka": same, "other/q.vuka": other} {
		t.Run(filepath.Dir(name), func(t *testing.T) {
			files[name] = src
			defer delete(files, name)
			c, dir := startLSPGo127(t, files)
			uri := pathToURI(filepath.Join(dir, name))
			c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
				"uri": uri, "languageId": "vuka", "version": 1, "text": src}})
			c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}})
			ds := c.waitDiags(uri, func(ds []any) bool {
				b, _ := json.Marshal(ds)
				return strings.Contains(string(b), "sentinel")
			})
			if len(ds) != 1 {
				b, _ := json.Marshal(ds)
				t.Errorf("diagnostics besides the sentinel's: %s", b)
			}
		})
	}
}
