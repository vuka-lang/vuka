package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// TestScopedORMPass: after an edit, the module's orm pass asks the tool
// again only in the directory holding what the edit reaches, and its answer
// is the one asking the whole module gives: for a model; for a relation's
// Related name, which is the reverse relation of another package's model;
// and, across the module, by asking the whole module.
func TestScopedORMPass(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the orm tool")
	}
	orm := ormDir(t)
	web := webCheckout(t)
	t.Setenv("VUKA_CACHE", t.TempDir())
	saved := shapes
	t.Cleanup(func() { shapes, ormRunHook = saved, nil })

	user := `package models

import "github.com/vuka-lang/orm/v2"

type User struct {
	orm.Model[User]
	ID    int64
	Email string
}
`
	invoice := `package billing

import (
	"github.com/vuka-lang/orm/v2"

	"example.com/app/%s"
)

type Invoice struct {
	orm.Model[Invoice]
	ID    int64
	Total int64
	Owner orm.FK[models.User] ` + "`orm:\"related:invoices\"`" + `
}
`
	page := `package %s

import (
	"context"

	"github.com/vuka-lang/orm/v2"

	"example.com/app/%s"
)

func Users(ctx context.Context) ([]models.User, error) { return orm.For[models.User]().All(ctx) }
`
	for _, c := range []struct {
		name, models, billing string // directories
		edit                  string // the file edited
		old, new              string
		run                   string // where the tool runs after the edit
		changes               string // a directory whose answer the edit changes, besides its own
	}{
		{name: "a model", models: "models", edit: "models/user.go", old: "Email string", new: "Email string\n\tName  string", run: "models"},
		{name: "a related name", models: "data/models", billing: "data/billing", edit: "data/billing/invoice.go", old: "invoices", new: "bills",
			run: "data", changes: "data/models"},
		{name: "across the module", models: "models", billing: "billing", edit: "billing/invoice.go", old: "invoices", new: "bills",
			run: ".", changes: "models"},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{
				"main.go":                          "package main\n\nimport (\n\t_ \"example.com/app/pages/p1\"\n\t_ \"example.com/app/pages/p2\"\n\t_ \"example.com/app/pages/p3\"\n)\n\nfunc main() {}\n",
				filepath.Join(c.models, "user.go"): user,
			}
			for _, p := range []string{"p1", "p2", "p3"} {
				files["pages/"+p+"/"+p+".go"] = sprintf(page, p, c.models)
			}
			if c.billing != "" {
				files[filepath.Join(c.billing, "invoice.go")] = sprintf(invoice, c.models)
			}
			root := localModule(t, web, files)
			withModule(t, root, ormModule, orm)
			// The tool's first go list completes go.mod, which asks the whole
			// module again.
			if _, err := runORM(root, []byte(readFile(t, filepath.Join(root, "go.mod"))), ".", "", false); err != nil {
				t.Fatal(err)
			}
			mod := []byte(readFile(t, filepath.Join(root, "go.mod")))
			var runs []string
			ormRunHook = func(dir string) {
				if dir != "." {
					dir, _ = filepath.Rel(root, dir)
				}
				runs = append(runs, filepath.ToSlash(dir))
			}
			shapes = newSourceShapes()
			gens := []load.Generated{{Target: filepath.Join(root, "x.go"), Src: []byte("models.UserFields.Email")}}
			before, err := ormMissing(root, "", gens, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(runs, []string{"."}) {
				t.Fatalf("the first pass ran the tool in %v", runs)
			}
			if len(before) == 0 {
				t.Fatal("the first pass generated nothing")
			}

			path := filepath.Join(root, c.edit)
			src, _ := os.ReadFile(path)
			writeFile(t, path, strings.Replace(string(src), c.old, c.new, 1))
			runs = nil
			after, err := ormMissing(root, "", gens, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(runs, []string{c.run}) {
				t.Errorf("after the edit, the tool ran in %v, want [%s]", runs, c.run)
			}
			ormRunHook = nil
			whole, err := runORM(root, mod, ".", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := answerText(root, after), answerText(root, whole); got != want {
				t.Errorf("the scoped answer\n%s\nisn't the whole module's\n%s", got, want)
			}
			if c.changes != "" {
				dir := filepath.Join(root, c.changes)
				if answerIn(before, dir) == answerIn(after, dir) {
					t.Errorf("the answer of %s didn't change", c.changes)
				}
			}
		})
	}
}

func sprintf(format string, args ...string) string {
	for _, a := range args {
		format = strings.Replace(format, "%s", a, 1)
	}
	return format
}

// answerText is the orm tool's answer, each file by its path under root.
func answerText(root string, files []load.Generated) string {
	var b bytes.Buffer
	for _, g := range files {
		rel, _ := filepath.Rel(root, g.Target)
		b.WriteString("== " + filepath.ToSlash(rel) + "\n")
		b.Write(g.Src)
	}
	return b.String()
}

func answerIn(files []load.Generated, dir string) string {
	var b bytes.Buffer
	for _, g := range files {
		if filepath.Dir(g.Target) == dir {
			b.WriteString(g.Target + "\n")
			b.Write(g.Src)
		}
	}
	return b.String()
}
