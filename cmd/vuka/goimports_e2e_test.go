package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// goImportsApp is an app whose package entities has only .vuka files (orm
// models), imported by plain Go: a Go package (dtos, which reads the
// models' typed fields too, and has a Go test), and a Go file of a package
// of .vuka.html components; a Vuka package (accounts) embeds a model and
// imports dtos, and the page imports both.
func goImportsApp(t *testing.T, edit map[string]string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	orm := ormDir(t)
	files := map[string]string{
		"main.go": "package main\n\nimport (\n\t\"example.com/app/pages\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc main() { web.New(pages.Routes()).Run(\"\") }\n",
		"entities/models.vuka": `package entities

import "github.com/vuka-lang/orm/v2"

type Category struct {
	orm.Base @orm.Meta{Table: "categories"}
	ID   int64  @orm.PK
	Name string @orm.Char{Max: 100}
}

type User struct {
	orm.Base @orm.Meta{Table: "users"}
	ID    int64  @orm.PK
	Email string @orm.Char{Max: 120}
}

func NameLookup() string { return Category.Name.Name() }
`,
		"dtos/advert.go": `package dtos

import "example.com/app/entities"

type CategoryCount struct {
	Category entities.Category
	Count    int
}

func NameColumn() string { return entities.CategoryFields.Name.Name() }
`,
		"dtos/advert_test.go": `package dtos

import (
	"testing"

	"example.com/app/entities"
)

func TestCount(t *testing.T) {
	c := CategoryCount{Category: entities.Category{Name: "x"}}
	if c.Category.Name != "x" || NameColumn() == "" {
		t.Fatal(c)
	}
}
`,
		"components/card.vuka.html": "<script lang=\"go\">\npackage components\n\ntype Card struct {\n\tTitle string\n}\n</script>\n\n<div>{Title}</div>\n",
		"components/count.go":       "package components\n\nimport \"example.com/app/dtos\"\n\nfunc CountLabel(c dtos.CategoryCount) string { return c.Category.Name }\n",
		"modules/accounts/account.vuka": `package accounts

import (
	"example.com/app/dtos"
	"example.com/app/entities"
)

type Account struct {
	entities.User
}

func (a *Account) Address() string { return a.Email }

func Count(c entities.Category) dtos.CategoryCount { return dtos.CategoryCount{Category: c, Count: 1} }
`,
		"pages/index.vuka.html": `<script lang="go">
package pages

import (
	"example.com/app/components"
	"example.com/app/modules/accounts"
)

var _ = accounts.Count

type Index struct{}
</script>

<section><components.Card Title="x"/></section>
`,
	}
	for name, src := range edit {
		if src == "" {
			delete(files, name)
		} else {
			files[name] = src
		}
	}
	root := localModule(t, webCheckout(t), ormColumnFixtures(orm, files))
	withModule(t, root, "github.com/vuka-lang/orm/v2", orm)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	return root
}

// TestGoImportsVuka: plain Go importing a package of only .vuka files
// builds, vets and tests, and vuka explain shows a file of it.
func TestGoImportsVuka(t *testing.T) {
	root := goImportsApp(t, nil)
	for _, c := range [][]string{
		{"build", "-o", filepath.Join(t.TempDir(), "app"), "."},
		{"vet", "./..."},
		{"vet", "./entities/"},
		{"test", "./dtos/"},
	} {
		var stderr bytes.Buffer
		if err := runGo(c[0], c[1:], io.Discard, &stderr); err != nil {
			t.Errorf("vuka %s: %v\n%s", strings.Join(c, " "), err, stderr.String())
		}
	}
	if _, err := generate(filepath.Join(root, "entities", "models.vuka")); err != nil {
		t.Errorf("explain entities/models.vuka: %v", err)
	}
}

// TestGoImportsVukaErrors: an error of the Go file importing the Vuka
// package is reported there, alone, its importers counted; an import of a
// path the module hasn't says so at the import, never "not in std".
func TestGoImportsVukaErrors(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"goerror", strings.Replace(goAdvert, "c.Category.Name", "c.Category.Nme", 1), "dtos/advert.go:13:"},
		{"missing", strings.Replace(goAdvert, `"example.com/app/entities"`, `"example.com/app/entitis"`, 1), "example.com/app/entitis isn't a package of module example.com/app: entitis/ doesn't exist"},
	} {
		t.Run(c.name, func(t *testing.T) {
			goImportsApp(t, map[string]string{"dtos/advert.go": c.src, "dtos/advert_test.go": ""})
			var stderr bytes.Buffer
			if runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
				t.Fatal("the build passed")
			}
			out := stderr.String()
			out = out[strings.Index(out, "error["):]
			name := "goimports-" + c.name + ".golden"
			if c.name == "missing" && strings.Contains(out, "entities.CategoryColumns") {
				name = "goimports-missing-columns.golden"
			}
			golden(t, name, out)
			if !strings.Contains(out, c.want) || strings.Contains(out, "not in std") || strings.Contains(out, "accounts") {
				t.Errorf("the report:\n%s", out)
			}
		})
	}
}

// goAdvert is dtos/advert.go with a method reading a model's field.
const goAdvert = `package dtos

import "example.com/app/entities"

type CategoryCount struct {
	Category entities.Category
	Count    int
}

func NameColumn() string { return entities.CategoryFields.Name.Name() }

func (c CategoryCount) Label() string {
	return c.Category.Name
}
`

// TestVetScope: vuka vet of a package reports that package's errors and
// those of what it imports, never another package's: a page's error is
// left out of vuka vet ./entities/, which passes and counts it, and
// vuka vet ./... reports it.
func TestVetScope(t *testing.T) {
	brokenPage := "<script lang=\"go\">\npackage pages\n\nimport (\n\t\"example.com/app/components\"\n\t\"example.com/app/modules/accounts\"\n)\n\nvar _ = accounts.Count\n\ntype Index struct{}\n\nfunc helper() {} @doc(\"Lists\")\n</script>\n\n<section><components.Card Title=\"x\"/></section>\n"
	goImportsApp(t, map[string]string{"pages/index.vuka.html": brokenPage})
	var stderr bytes.Buffer
	if err := runGo("vet", []string{"./entities/"}, io.Discard, &stderr); err != nil {
		t.Errorf("vuka vet ./entities/: %v\n%s", err, stderr.String())
	}
	if out := stderr.String(); strings.Contains(out, "error[") || !strings.Contains(out, "1 error in packages not asked about") {
		t.Errorf("vuka vet ./entities/:\n%s", out)
	}
	stderr.Reset()
	if runGo("vet", []string{"./..."}, io.Discard, &stderr) == nil || !strings.Contains(stderr.String(), "pages/index.vuka.html") {
		t.Errorf("vuka vet ./... doesn't report the page:\n%s", stderr.String())
	}
}

// TestVetScopeCause: vuka vet of a package whose import doesn't compile
// reports that import's error, the cause.
func TestVetScopeCause(t *testing.T) {
	goImportsApp(t, map[string]string{"entities/broken.vuka": "package entities\n\nfunc broken() int { return undefinedName }\n"})
	var stderr bytes.Buffer
	if runGo("vet", []string{"./modules/accounts/"}, io.Discard, &stderr) == nil {
		t.Fatal("vuka vet passed")
	}
	out := stderr.String()
	if !strings.Contains(out, "entities/broken.vuka:3:") || strings.Contains(out, "not in std") || strings.Count(out, "error[") != 1 {
		t.Errorf("vuka vet ./modules/accounts/:\n%s", out)
	}
}
