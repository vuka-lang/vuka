package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const scopeUser = `package models

import "github.com/vuka-lang/orm/v2"

type User struct {
	orm.Model[User]
	ID    int64
	Email string
}

type Plain struct{ ID int64 }

type Alias = User
`

func scopePage(name, body string) string {
	return "package " + name + "\n\nimport (\n\t\"github.com/vuka-lang/orm/v2\"\n\n\t\"example.com/app/models\"\n)\n\nfunc Find() {\n" + body + "\n}\n"
}

// scopeApp is a module whose pages call For on the models package's types.
func scopeApp(t *testing.T, extra map[string]string) string {
	files := map[string]string{
		"go.mod":           "module example.com/app\n\ngo 1.25\n",
		"main.go":          "package main\n\nimport _ \"example.com/app/pages/a\"\n\nfunc main() {}\n",
		"models/user.go":   scopeUser,
		"pages/a/a.go":     scopePage("a", "\torm.For[models.User]()"),
		"pages/b/b.go":     scopePage("b", "\torm.For[models.User]()"),
		"util/util.go":     "package util\n\ntype Money int64\n",
		"testdata/x/x.go":  "package x\n",
		"nested/go.mod":    "module example.com/nested\n",
		"nested/nested.go": "package nested\n",
		"pages/c/c.go":     "package c\n\nimport \"example.com/app/util\"\n\nvar _ util.Money\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return tempModule(t, files)
}

func scopeState(t *testing.T, root string) *ormState {
	t.Helper()
	st := newSourceShapes().ormModuleState(root, "")
	if st == nil {
		t.Fatal("no state")
	}
	return st
}

// TestPlanORM: an edit is answered in the smallest directory holding the
// models directories it reaches and what their answers read, or by the
// whole module when they span it or the source can't tell.
func TestPlanORM(t *testing.T) {
	billing := `package billing

import (
	"encoding/json"
	"os"
	"github.com/vuka-lang/orm/v2"

	"example.com/app/models"
)

type Invoice struct {
	orm.Model[Invoice]
	ID    int64
	Owner orm.FK[models.User] ` + "`orm:\"related:invoices\"`" + `
}
`
	for _, c := range []struct {
		name  string
		extra map[string]string
		edit  map[string]string // path → new content ("" removes)
		unit  string            // "" with no affected: none; "." the whole module
		want  []string          // affected
		why   string
	}{
		{name: "a model", edit: map[string]string{"models/user.go": strings.Replace(scopeUser, "Email string", "Email string\n\tName string", 1)},
			unit: "models", want: []string{"models"}},
		{name: "a comment", edit: map[string]string{"models/user.go": "// Package models.\n" + scopeUser}},
		{name: "a page's body, the same For", edit: map[string]string{"pages/a/a.go": scopePage("a", "\torm.For[models.User]()\n\tprintln()")}},
		{name: "a page naming another model", edit: map[string]string{"pages/a/a.go": scopePage("a", "\torm.For[models.User]()\n\torm.For[models.Plain]()")},
			unit: ".", why: "across the module"},
		{name: "a page naming an alias", edit: map[string]string{"pages/a/a.go": scopePage("a", "\torm.For[models.Alias]()")},
			unit: ".", why: "names an alias"},
		{name: "a For of a type literal", edit: map[string]string{"pages/a/a.go": scopePage("a", "\torm.For[models.User]()\n\torm.For[[]int]()")}},
		{name: "a For of something else", edit: map[string]string{"pages/a/a.go": scopePage("a", "\torm.For[models.User]()\n\torm.For[func() int]()")}},
		{name: "a package no model imports", edit: map[string]string{"util/util.go": "package util\n\ntype Money int32\n"}},
		{name: "a package a model imports",
			extra: map[string]string{"models/money.go": "package models\n\nimport \"example.com/app/util\"\n\nvar _ util.Money\n"},
			edit:  map[string]string{"util/util.go": "package util\n\ntype Money int32\n"},
			unit:  "models", want: []string{"models"}},
		{name: "a new models package", edit: map[string]string{"shop/shop.go": "package shop\n\nimport \"github.com/vuka-lang/orm/v2\"\n\ntype Cart struct {\n\torm.Model[Cart]\n\tID int64\n}\n"},
			unit: "shop", want: []string{"shop"}},
		{name: "a models package removed",
			extra: map[string]string{"shop/shop.go": "package shop\n\nimport \"github.com/vuka-lang/orm/v2\"\n\ntype Cart struct {\n\torm.Model[Cart]\n\tID int64\n}\n"},
			edit:  map[string]string{"shop/shop.go": ""},
			unit:  "shop", want: []string{"shop"}},
		{name: "a relation into another package", extra: map[string]string{"billing/invoice.go": billing},
			edit: map[string]string{"billing/invoice.go": strings.Replace(billing, "invoices", "bills", 1)},
			unit: ".", why: "across the module"},
		{name: "a relation inside a directory",
			extra: map[string]string{"models/billing/invoice.go": billing},
			edit:  map[string]string{"models/billing/invoice.go": strings.Replace(billing, "invoices", "bills", 1)},
			unit:  "models", want: []string{"models", "models/billing"}},
		{name: "a model a relation leads to",
			extra: map[string]string{"models/billing/invoice.go": billing},
			edit:  map[string]string{"models/user.go": strings.Replace(scopeUser, "Email string", "Email string\n\tName string", 1)},
			unit:  "models", want: []string{"models", "models/billing"}},
		{name: "a model by a local embed",
			extra: map[string]string{"shop/shop.go": "package shop\n\nimport \"github.com/vuka-lang/orm/v2\"\n\ntype Cart struct {\n\tbase\n\tID int64\n}\n\ntype base struct{ orm.Model[Cart] }\n"},
			edit:  map[string]string{"shop/shop.go": "package shop\n\nimport \"github.com/vuka-lang/orm/v2\"\n\ntype Cart struct {\n\tbase\n\tID int64\n\tN int\n}\n\ntype base struct{ orm.Model[Cart] }\n"},
			unit:  "shop", want: []string{"shop"}},
		{name: "an embed named by another file's import",
			extra: map[string]string{
				"shop/cart.go": "package shop\n\nimport \"github.com/vuka-lang/orm/v2\"\n\n// Cart is an orm.Model[Cart] through base.\ntype Cart struct {\n\tbase\n\tID int64\n}\n\nvar _ orm.Meta\n",
				"shop/base.go": "package shop\n\nimport \"example.com/app/util\"\n\ntype base struct{ util.Money }\n",
			},
			edit: map[string]string{"util/util.go": "package util\n\ntype Money int32\n"},
			unit: "shop", want: []string{"shop"}},
		{name: "a file that doesn't parse", edit: map[string]string{"models/user.go": scopeUser + "\nfunc ("}, unit: ".", why: "doesn't parse"},
		{name: "go.mod", edit: map[string]string{"go.mod": "module example.com/app\n\ngo 1.26\n"}, unit: ".", why: "go.mod"},
		{name: "testdata", edit: map[string]string{"testdata/x/x.go": "package x\n\nvar X int\n"}},
		{name: "a nested module", edit: map[string]string{"nested/nested.go": "package nested\n\nvar X int\n"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scopeApp(t, c.extra)
			old := scopeState(t, root)
			for path, src := range c.edit {
				if src == "" {
					removeFile(t, filepath.Join(root, path))
				} else {
					writeFile(t, filepath.Join(root, path), src)
				}
			}
			now := scopeState(t, root)
			plan := planORM(root, "example.com/app", old, now)
			switch {
			case c.unit == ".":
				if plan.Why == "" || !strings.Contains(plan.Why, c.why) {
					t.Fatalf("plan %+v, want the whole module for %q", plan, c.why)
				}
				return
			case plan.Why != "":
				t.Fatalf("the whole module: %s", plan.Why)
			case c.unit == "":
				if len(plan.Affected) != 0 {
					t.Fatalf("affected %v, want none", plan.Affected)
				}
				return
			}
			if want := filepath.Join(root, c.unit); plan.Unit != want {
				t.Errorf("unit %s, want %s", plan.Unit, want)
			}
			var got []string
			for d := range plan.Affected {
				rel, _ := filepath.Rel(root, d)
				got = append(got, filepath.ToSlash(rel))
			}
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Errorf("affected %v, want %v", got, c.want)
			}
		})
	}
}

// TestPlanORMOverlay: the overlay's Go counts as the directory's, and a
// file only the overlay has outside the module asks the whole module.
func TestPlanORMOverlay(t *testing.T) {
	root := scopeApp(t, nil)
	tmp := t.TempDir()
	s := newSourceShapes()
	old := s.ormModuleState(root, "")
	user := filepath.Join(tmp, "user.go")
	writeFile(t, user, strings.Replace(scopeUser, "Email string", "Email string\n\tName string", 1))
	ov := writeOverlay(t, tmp, map[string]string{filepath.Join(root, "models", "user.go"): user})
	plan := planORM(root, "example.com/app", old, s.ormModuleState(root, ov))
	if plan.Why != "" || plan.Unit != filepath.Join(root, "models") {
		t.Errorf("overlaid model: %+v", plan)
	}
	outside := filepath.Join(filepath.Dir(root), "elsewhere", "x.go")
	ov = writeOverlay(t, tmp, map[string]string{outside: user})
	if plan := planORM(root, "example.com/app", old, s.ormModuleState(root, ov)); !strings.Contains(plan.Why, "isn't one of the module's packages") {
		t.Errorf("overlaid outside the module: %+v", plan)
	}
}

func TestMergeORM(t *testing.T) {
	gen := func(path, src string) load.Generated { return load.Generated{Target: path, Src: []byte(src)} }
	kept := []load.Generated{gen("/m/a/orm_scanners_gen.go", "a1"), gen("/m/b/orm_scanners_gen.go", "b1"), gen("/m/b/user_gen.go", "b1u")}
	fresh := []load.Generated{gen("/m/b/orm_scanners_gen.go", "b2"), gen("/m/b/sub/orm_scanners_gen.go", "s2"), gen("/m/c/orm_scanners_gen.go", "c2")}
	got := mergeORM(kept, fresh, map[string]bool{"/m/b": true, "/m/c": true})
	var lines []string
	for _, g := range got {
		lines = append(lines, g.Target+" "+string(g.Src))
	}
	want := []string{"/m/a/orm_scanners_gen.go a1", "/m/b/orm_scanners_gen.go b2", "/m/c/orm_scanners_gen.go c2"}
	if !slices.Equal(lines, want) {
		t.Errorf("merged\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestCommonDir(t *testing.T) {
	sep := string(filepath.Separator)
	j := func(parts ...string) string { return sep + strings.Join(parts, sep) }
	for _, c := range []struct {
		dirs []string
		want string
	}{
		{[]string{j("m", "a")}, j("m", "a")},
		{[]string{j("m", "a"), j("m", "a", "b")}, j("m", "a")},
		{[]string{j("m", "ab"), j("m", "a")}, j("m")},
		{[]string{j("m", "a", "x"), j("m", "b", "y")}, j("m")},
	} {
		if got := commonDir(c.dirs); got != c.want {
			t.Errorf("commonDir(%v) = %s, want %s", c.dirs, got, c.want)
		}
	}
}

// TestORMFacts: what a file's facts say of its models and For calls.
func TestORMFacts(t *testing.T) {
	src := `package models

import (
	"encoding/json"
	"os"
	db "github.com/vuka-lang/orm/v2"
	. "example.com/app/util"
)

type User struct {
	*db.Model[User]
	ID int64
}

type Post struct {
	db.Model[User]
	Stamps
}

type Box[T any] struct{ V T }

type Ref = User

func f() {
	db.For[*User]()
	db.For[Box[int]]()
	db.For[map[string]int]()
	_ = x.For
}
`
	ff := shapesOf([]byte(src)).orm
	if ff.Broken || !ff.ORM || ff.Opaque || ff.Pkg != "models" {
		t.Fatalf("facts %+v", ff)
	}
	byName := map[string]ormTypeFacts{}
	for _, ty := range ff.Types {
		byName[ty.Name] = ty
	}
	if !byName["User"].Self || byName["Post"].Self {
		t.Errorf("self: User %v, Post %v", byName["User"].Self, byName["Post"].Self)
	}
	if !byName["Box"].Generic || !byName["Ref"].Alias {
		t.Errorf("Box %+v, Ref %+v", byName["Box"], byName["Ref"])
	}
	if got := byName["Post"].Embeds; len(got) != 2 || got[0] != (ormExpr{"db", "Model", true}) || got[1] != (ormExpr{"", "Stamps", false}) {
		t.Errorf("Post embeds %+v", got)
	}
	if want := []ormExpr{{"", "User", false}, {"", "Box", true}}; !slices.Equal(ff.Targets, want) {
		t.Errorf("targets %+v, want %+v", ff.Targets, want)
	}
	if bad := shapesOf([]byte("package x\n\nfunc (")).orm; !bad.Broken {
		t.Error("a file that doesn't parse has facts")
	}
	if odd := shapesOf([]byte("package x\n\nvar _ = orm.For[a.b.C]\n")).orm; !odd.Opaque {
		t.Error("For[a.b.C] is named plainly")
	}
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// writeOverlay writes a go build -overlay file replacing each target with
// its file.
func writeOverlay(t *testing.T, dir string, replace map[string]string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "overlay.json")
	writeFile(t, path, string(data))
	return path
}
