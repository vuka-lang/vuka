package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modelDefsModels = `package models

import (
	"time"

	"github.com/vuka-lang/orm/v2"
)

type Stamps struct {
	Created time.Time
}

type User struct {
	orm.Base @orm.Meta{Table: "users"}
	Stamps
	ID    int64
	// PasswordLastUpdated is when the password last changed.
	PasswordLastUpdated time.Time
	Email string
}

type Post struct {
	orm.Base @orm.Meta{Table: "posts"}
	ID     int64
	Title  string
	Author orm.FK[User] @orm.Rel{Related: "posts"}
}
`

const modelDefsItem = `package shop

import "github.com/vuka-lang/orm/v2"

type Item struct {
	orm.Model[Item]
	ID   int64
	Name string
}
`

const modelDefsMain = `package main

import (
	"fmt"

	"lsptest/models"
	"lsptest/shop"
)

func main() {
	var u models.User
	when := models.User.PasswordLastUpdated
	mail := models.Post.Author.Email
	made := models.User.Created
	q := models.User.Objects
	posts := models.UserColumns.Posts().Title
	name := shop.Item.Name
	fmt.Println(u.PasswordLastUpdated, when, mail, made, q, posts, name)
}
`

const modelDefsPage = `<script lang="go">
package pages

import (
	"github.com/vuka-lang/ui/v2"

	"lsptest/models"
)

type List struct {
	ui.Live
}
</script>

<main>
  <p title={models.User.Email.Name()}>{models.User.PasswordLastUpdated.Name()}</p>
</main>
`

// TestLSPModelDefinition: a model's typed fields are the orm's generated
// Go, which the editor never has; definition on them lands on the model's
// source — the type for Model, the struct field (an embedded struct's
// included) for Model.Field and Model.Rel.Field, in a .vuka or a .go file —
// as on an instance's field, hover shows the model field's declaration and
// doc with the typed field's type, and references join the field's uses
// with its typed field's. Model.Objects leads to the orm's manager.
func TestLSPModelDefinition(t *testing.T) {
	t.Parallel()
	files := map[string]string{"main.vuka": modelDefsMain, "models/models.vuka": modelDefsModels, "shop/item.go": modelDefsItem,
		"pages/list.vuka.html": modelDefsPage}
	in := map[string]string{}
	for k, v := range files {
		in[k] = v
	}
	c, dir, orm := startLSPWithORM(t, in)
	uri := func(name string) string { return pathToURI(filepath.Join(dir, name)) }
	for _, name := range []string{"main.vuka", "pages/list.vuka.html"} {
		lang := "vuka"
		if strings.HasSuffix(name, ".html") {
			lang = "vuka-html"
		}
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri(name), "languageId": lang, "version": 1, "text": files[name]}})
		c.waitDiags(uri(name), func(ds []any) bool { return len(ds) == 0 })
	}
	// at is name's position at needle, moved in by in bytes.
	at := func(name, needle string, in int) map[string]any {
		i := strings.Index(files[name], needle)
		if i < 0 {
			t.Fatalf("%s has no %q", name, needle)
		}
		return map[string]any{"textDocument": map[string]any{"uri": uri(name)}, "position": positionOf([]byte(files[name]), i+in)}
	}
	type loc struct {
		file string
		line uint32
	}
	line := func(name, needle string) loc {
		i := strings.Index(files[name], needle)
		if i < 0 {
			t.Fatalf("%s has no %q", name, needle)
		}
		return loc{name, positionOf([]byte(files[name]), i).Line}
	}
	locs := func(raw json.RawMessage) map[loc]bool {
		var list []map[string]any
		if json.Unmarshal(raw, &list) != nil {
			var one map[string]any
			_ = json.Unmarshal(raw, &one)
			list = []map[string]any{one}
		}
		out := map[loc]bool{}
		for _, l := range list {
			u, _ := l["uri"].(string)
			r, _ := toRange(l["range"])
			rel, err := filepath.Rel(dir, uriToPath(u))
			if err != nil || strings.HasPrefix(rel, "..") {
				rel, _ = filepath.Rel(orm, uriToPath(u))
				rel = "orm:" + rel
			}
			out[loc{filepath.ToSlash(rel), r.Start.Line}] = true
		}
		return out
	}
	const models, main, page = "models/models.vuka", "main.vuka", "pages/list.vuka.html"
	userDecl, postDecl := line(models, "type User struct"), line(models, "type Post struct")
	pwDecl, emailDecl, authorDecl := line(models, "\tPasswordLastUpdated"), line(models, "\tEmail string"), line(models, "\tAuthor ")
	createdDecl, nameDecl := line(models, "\tCreated "), line("shop/item.go", "\tName string")
	src, _ := os.ReadFile(filepath.Join(orm, "ormgen", "fields.go"))
	directives := bytes.Contains(src, []byte("//orm:source"))
	refs := func(p map[string]any) map[string]any {
		p["context"] = map[string]any{"includeDeclaration": true}
		return p
	}
	for _, ch := range []struct {
		what, method string
		params       map[string]any
		want         []loc
		has          []string
	}{
		{"definition on the model", "textDocument/definition", at(main, "models.User.PasswordLastUpdated", len("models.Us")), []loc{userDecl}, nil},
		{"definition on a typed field", "textDocument/definition", at(main, "models.User.PasswordLastUpdated", len("models.User.Pass")), []loc{pwDecl}, nil},
		{"definition on a relation's model", "textDocument/definition", at(main, "models.Post.Author", len("models.Po")), []loc{postDecl}, nil},
		{"definition on a relation", "textDocument/definition", at(main, "models.Post.Author", len("models.Post.Au")), []loc{authorDecl}, nil},
		{"definition through a relation", "textDocument/definition", at(main, "Author.Email", len("Author.Em")), []loc{emailDecl}, nil},
		{"definition on an embedded struct's field", "textDocument/definition", at(main, "models.User.Created", len("models.User.Cr")), []loc{createdDecl}, nil},
		{"definition on a Go model's field", "textDocument/definition", at(main, "shop.Item.Name", len("shop.Item.Na")), []loc{nameDecl}, nil},
		{"definition on an instance's field", "textDocument/definition", at(main, "u.PasswordLastUpdated", 3), []loc{pwDecl}, nil},
		{"definition on the manager", "textDocument/definition", at(main, "models.User.Objects", len("models.User.Obj")), []loc{{"orm:model.go", 0}}, nil},
		{"definition in an attribute", "textDocument/definition", at(page, "models.User.Email", len("models.User.Em")), []loc{emailDecl}, nil},
		{"definition in markup", "textDocument/definition", at(page, "{models.User.PasswordLastUpdated", len("{models.User.Pass")), []loc{pwDecl}, nil},
		{"hover on a typed field", "textDocument/hover", at(main, "models.User.PasswordLastUpdated", len("models.User.Pass")), nil,
			[]string{"field PasswordLastUpdated time.Time", "when the password last changed", "orm.TimeField"}},
		{"hover on the model", "textDocument/hover", at(main, "models.User.PasswordLastUpdated", len("models.Us")), nil, []string{"type User struct"}},
		{"hover on an embedded struct's field", "textDocument/hover", at(main, "models.User.Created", len("models.User.Cr")), nil, []string{"field Created time.Time", "orm.TimeField"}},
		{"references of a model's field", "textDocument/references", refs(at(models, "PasswordLastUpdated time", 2)),
			[]loc{pwDecl, line(main, "when := models.User.PasswordLastUpdated"), line(main, "u.PasswordLastUpdated"), line(page, "{models.User.PasswordLastUpdated")}, nil},
		{"references of a typed field", "textDocument/references", refs(at(main, "models.User.PasswordLastUpdated", len("models.User.Pass"))),
			[]loc{pwDecl, line(main, "when := models.User.PasswordLastUpdated"), line(main, "u.PasswordLastUpdated"), line(page, "{models.User.PasswordLastUpdated")}, nil},
	} {
		t.Run(ch.what, func(t *testing.T) {
			m := c.callWithin(answerDeadline, ch.method, ch.params)
			if len(m.Error) > 0 {
				t.Fatalf("%s", m.Error)
			}
			for _, h := range ch.has {
				if !strings.Contains(string(m.Result), h) {
					t.Errorf("no %q in %s", h, m.Result)
				}
			}
			got := locs(m.Result)
			for _, w := range ch.want {
				if w.file == "orm:model.go" {
					found := false
					for g := range got {
						found = found || g.file == w.file
					}
					if !found {
						t.Errorf("not in the orm's model.go: %s", m.Result)
					}
					continue
				}
				if !got[w] {
					t.Errorf("no %s:%d in %s", w.file, w.line+1, m.Result)
				}
			}
			if ch.method == "textDocument/definition" && len(got) != 1 {
				t.Errorf("%d locations: %s", len(got), m.Result)
			}
			if ch.method == "textDocument/references" && (got[userDecl] || got[line(models, "\tStamps\n")]) {
				t.Errorf("a use in the generated Go as the model's: %s", m.Result)
			}
			if strings.Contains(string(m.Result), "_gen.go") {
				t.Errorf("a location in generated Go: %s", m.Result)
			}
		})
	}

	// An inverse is declared on the other model: only the orm's directive
	// says which field.
	t.Run("definition on an inverse relation", func(t *testing.T) {
		if !directives {
			t.Skipf("the orm at %s writes no //orm:source directives", orm)
		}
		m := c.callWithin(answerDeadline, "textDocument/definition", at(main, "models.UserColumns.Posts", len("models.UserColumns.Po")))
		if got := locs(m.Result); !got[authorDecl] || len(got) != 1 {
			t.Errorf("want only %s:%d: %s%s", authorDecl.file, authorDecl.line+1, m.Result, m.Error)
		}
	})
}
