package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoLinkEmbedsDeclaring: a package whose struct types embed a type
// marked with vukaDeclares (as ORM models embed orm.Base) is linked into the
// mains with no decorator in it; embedding an unmarked type isn't, and
// //vuka:nolink still opts a package out. The fixture mirrors the ORM's
// generic shape: Base[Self] is a Model[Self], both marked, Self filled in.
func TestAutoLinkEmbedsDeclaring(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	vuka := buildVuka(t)
	repo, _ := filepath.Abs("../..")
	root := tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka/v2 v2.0.0\n\nreplace github.com/vuka-lang/vuka/v2 => " + repo + "\n",
		"orm/orm.vuka": `package orm

var Models []string

type Model[T any] struct{ ID int64 }

func (Model[T]) vukaDeclares() {}

type Base[Self any] Model[Self]

func (Base[Self]) vukaDeclares() {}

// Plain has no marker: embedding it registers nothing.
type Plain struct{ Note string }

func Register(name string) bool { Models = append(Models, name); return true }
`,
		"models/post.vuka":  "package models\n\nimport \"example.com/app/orm\"\n\ntype Post struct {\n\torm.Base\n\tTitle string\n}\n\nvar _ = orm.Register(\"post\")\n",
		"models/user.vuka":  "package models\n\nimport \"example.com/app/orm\"\n\ntype User struct {\n\t*orm.Model[User]\n}\n",
		"notes/note.vuka":   "package notes\n\nimport \"example.com/app/orm\"\n\ntype Note struct {\n\torm.Plain\n}\n\nvar _ = orm.Register(\"note\")\n",
		"drafts/draft.vuka": "//vuka:nolink\npackage drafts\n\nimport \"example.com/app/orm\"\n\ntype Draft struct {\n\torm.Base\n}\n\nvar _ = orm.Register(\"draft\")\n",
		"main.vuka":         "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/app/orm\"\n)\n\nfunc main() { fmt.Println(orm.Models) }\n",
	})
	if out, err := runVuka(t, vuka, root, "gen", "-inplace"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out)
	}
	link, _ := os.ReadFile(filepath.Join(root, "vuka_link_gen.go"))
	if !strings.Contains(string(link), `_ "example.com/app/models"`) {
		t.Errorf("the model package isn't linked:\n%s", link)
	}
	if strings.Contains(string(link), "notes") || strings.Contains(string(link), "drafts") {
		t.Errorf("an unmarked embedding or a //vuka:nolink package is linked:\n%s", link)
	}
	bin := filepath.Join(root, "app")
	if out, err := runVuka(t, vuka, root, "build", "-o", bin, "."); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, out)
	}
	if got, _ := exec.Command(bin).Output(); strings.TrimSpace(string(got)) != "[post]" {
		t.Errorf("registered models: %q", got)
	}
}
