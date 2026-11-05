package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const membersModels = `package models

import (
	"context"
	"time"

	"github.com/vuka-lang/orm/v2"
)

type User struct {
	orm.Base @orm.Meta{Table: "users"}
	ID       int64
	Email    string @orm.Char{Max: 120}
	Expires  time.Time
}

type Post struct {
	orm.Base @orm.Meta{Table: "posts"}
	ID       int64
	Title    string
	Author   orm.FK[User]
}

func (p *Post) BeforeSave(ctx context.Context) error { return nil }

//HOOK
`

const membersShapes = `package shapes

type Base struct {
	Name string
}

func (b Base) Title() string    { return b.Name }
func (b *Base) Rename(n string) { b.Name = n }

type Circle struct {
	Base
	R float64
}

func (c Circle) Area() float64 { return 3 * c.R * c.R }
`

const membersSignup = `package forms

import "github.com/vuka-lang/ui/v2"

type Signup struct {
	ui.FormState
	Email string
	Name  string
}
`

const membersJoin = `<script lang="go">
package forms

import "github.com/vuka-lang/ui/v2"

type Join struct {
	ui.Live
	Form Signup
}
</script>

<main>
  <ui.Form for={Form}>
    <label for={Form.Email}>Email</label>
    <input model={Form.Email}/>
    <p>{Form.Name}</p>
  </ui.Form>
</main>
`

const membersMain = `package main

import (
	"context"
	"fmt"

	"lsptest/forms"
	"lsptest/models"
	"lsptest/shapes"
)

func main() {
	var c shapes.Circle
	pc := &c
	var u models.User
	var f forms.Signup
	fmt.Println(c, pc, u.ID, f.Email, context.Background())
	//PROBE
}
`

// startLSPWithORM is startLSPWith for a module requiring the orm checkout.
func startLSPWithORM(t *testing.T, files map[string]string) (*lspClient, string, string) {
	t.Helper()
	if _, err := findGopls(""); err != nil {
		needs(t, "%v", err)
	}
	orm := ormDir(t)
	dir := t.TempDir()
	mod, sum := testModule(t, "lsptest")
	ormSum, _ := os.ReadFile(filepath.Join(orm, "go.sum"))
	mod += "\nrequire github.com/vuka-lang/orm/v2 v2.0.0\n\nreplace github.com/vuka-lang/orm/v2 => " + orm + "\n"
	files["go.mod"], files["go.sum"] = mod, sum+string(ormSum)
	for name, src := range files {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	return startLSPAt(t, dir, lspOptions{renameCommands: true}), dir, orm
}

// membersEditor edits and completes the module's files as an editor does.
type membersEditor struct {
	t        *testing.T
	c        *lspClient
	dir      string
	versions map[string]int
}

// complete puts probe+after in place of mark in base, the text of file, and
// completes at the end of probe.
func (e *membersEditor) complete(file, base, mark, probe, after string) map[string]map[string]any {
	e.t.Helper()
	uri := pathToURI(filepath.Join(e.dir, file))
	lang := "vuka"
	if strings.HasSuffix(file, ".vuka.html") {
		lang = "vuka-html"
	}
	src := strings.Replace(base, mark, probe+after, 1)
	if e.versions[uri] == 0 {
		e.c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": lang, "version": 1, "text": base}})
		e.versions[uri] = 1
		// Built once as it is: what an edit breaks keeps this generation.
		e.c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	}
	e.versions[uri]++
	e.c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": e.versions[uri]},
		"contentChanges": []any{map[string]any{"text": src}}})
	off := strings.Index(src, probe+after) + len(probe)
	return completionLabels(e.t, e.c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), off)}))
}

// TestLSPMemberCompletion: completion offers a value's members in every
// place a Go expression is written, for a plain struct with embedded
// methods, a ui form and an orm model (whose typed fields and scanners
// the orm generates into the build's overlay, never on disk); a model's
// type offers its statics and its field references as typed fields, and a
// method being declared on it the orm's hooks it doesn't define.
func TestLSPMemberCompletion(t *testing.T) {
	c, dir, orm := startLSPWithORM(t, map[string]string{
		"main.vuka": membersMain, "models/models.vuka": membersModels, "shapes/shapes.vuka": membersShapes,
		"forms/signup.go": membersSignup, "forms/join.vuka.html": membersJoin,
	})
	e := &membersEditor{t: t, c: c, dir: dir, versions: map[string]int{}}
	main := func(probe string) (string, string, string, string, string) {
		return "main.vuka", membersMain, "//PROBE", "x := " + probe, "\n\tfmt.Println(x)"
	}
	join := func(mark, probe, after string) (string, string, string, string, string) {
		return "forms/join.vuka.html", membersJoin, mark, probe, after
	}
	hasHooks := false
	if src, err := os.ReadFile(filepath.Join(orm, "meta.go")); err == nil {
		hasHooks = bytes.Contains(src, []byte("IndexesDeclarer"))
	}
	for _, tc := range []struct {
		name                      string
		file, base, mark, p, rest string
		want, absent              []string
		details                   map[string]string // label → a part of its detail
		needsHooks                bool
		midEdit                   bool // the build of the text being typed reports nothing about it
	}{
		{name: "a value's method set", want: []string{"Area", "Title", "R", "Name"}},
		{name: "a pointer's method set", want: []string{"Rename", "Area", "Title"}},
		{name: "methods promoted from orm.Base", want: []string{"Save", "SaveFields", "Refresh", "Delete", "Load", "ID", "Email"}},
		{name: "methods promoted from ui.FormState", want: []string{"Email", "Name", "Valid", "Load", "Fail"}},
		{name: "generated typed fields", want: []string{"ID", "Email", "Expires"}, details: map[string]string{"Email": "TextField[string]"}},
		{name: "a model's manager", want: []string{"Filter", "All", "Get"}},
		{name: "a model's type", want: []string{"Objects", "ID", "Email", "As", "On"}, absent: []string{"Model", "Expires.Add"},
			details: map[string]string{"Objects": "Manager[models.User]", "Email": "orm.TextField[string]"}},
		{name: "a relation", want: []string{"Author"}, details: map[string]string{"Author": "UserPath"}},
		{name: "a relation's fields", want: []string{"Email", "ID"}},
		{name: "a markup expression", want: []string{"Email", "Name", "Valid"}},
		{name: "an attribute value", want: []string{"Email", "Name", "Valid"}, midEdit: true},
		{name: "a field reference", want: []string{"Email", "Name", "Valid"}, midEdit: true},
		{name: "an attribute's argument", want: []string{"Second", "Minute"}},
		{name: "a model's hooks", want: []string{"BeforeCreate", "AfterSave", "Validate", "Indexes", "Checks"}, absent: []string{"BeforeSave"},
			details: map[string]string{"Validate": "Validate(ctx context.Context) error", "Indexes": "Indexes() []orm.Index"}, needsHooks: true},
	} {
		switch tc.name {
		case "a value's method set":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("c.")
		case "a pointer's method set":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("pc.")
		case "methods promoted from orm.Base":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("u.")
		case "methods promoted from ui.FormState":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("f.")
		case "generated typed fields":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("models.UserColumns.")
		case "a model's manager":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("models.User.Objects.")
		case "a model's type":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("models.User.")
		case "a relation":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("models.Post.")
		case "a relation's fields":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = main("models.Post.Author.")
		case "a markup expression":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = join("{Form.Name}", "{Form.", "}")
		case "an attribute value":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = join("model={Form.Email}", "model={Form.", "}")
		case "a field reference":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = join("label for={Form.Email}", "label for={Form.", "}")
		case "an attribute's argument":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = "models/models.vuka", membersModels, "@orm.Char{Max: 120}", "@orm.Char{Max: int(time.", ")}"
		case "a model's hooks":
			tc.file, tc.base, tc.mark, tc.p, tc.rest = "models/models.vuka", membersModels, "//HOOK", "func (p Post) ", "\n"
		}
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsHooks && !hasHooks {
				t.Skipf("the orm at %s declares no IndexesDeclarer", orm)
			}
			e.t = t
			items := e.complete(tc.file, tc.base, tc.mark, tc.p, tc.rest)
			b, _ := json.Marshal(items)
			for _, l := range tc.want {
				if items[l] == nil {
					t.Errorf("no %s: %.2000s", l, b)
				}
			}
			for _, l := range tc.absent {
				if items[l] != nil {
					t.Errorf("%s offered", l)
				}
			}
			if tc.midEdit {
				// A field reference being typed is no error: the package's Go
				// builds, rather than completion reading the last good build's.
				for deadline := time.After(3 * time.Second); ; {
					select {
					case d := <-c.diags:
						if b, _ := json.Marshal(d); bytes.Contains(b, []byte("__vuka_complete")) {
							t.Errorf("the build reports the text being typed: %s", b)
						}
						continue
					case <-deadline:
					}
					break
				}
			}
			for l, part := range tc.details {
				if d, _ := items[l]["detail"].(string); !strings.Contains(d, part) {
					t.Errorf("%s's detail = %q, want %q in it", l, d, part)
				}
			}
		})
	}

	t.Run("an edit of the model", func(t *testing.T) {
		e.t = t
		edited := strings.Replace(membersModels, "\tEmail    string @orm.Char{Max: 120}\n", "\tEmail    string @orm.Char{Max: 120}\n\tNickname string\n", 1)
		e.complete("models/models.vuka", membersModels, membersModels, edited, "")
		// The models' package builds after the request's own.
		deadline := time.Now().Add(60 * time.Second)
		for {
			typed, _ := e.complete(main("models.User."))["Nickname"]["detail"].(string)
			if typed == "orm.TextField[string]" && e.complete(main("models.UserColumns."))["Nickname"] != nil && e.complete(main("u."))["Nickname"] != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("no typed Nickname after the edit (detail %q)\n%s", typed, c.log.String())
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
}
