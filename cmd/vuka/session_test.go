package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
)

// TestSessionTempl edits a module of a Vuka package, a .templ package
// importing it and a main using both, and checks each of a session's runs
// against load.Transpile from scratch.
func TestSessionTempl(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	files := map[string]string{
		"data/data.vuka":   "package data\n\ntype Pet struct{ Name string }\n\nfunc All() Result[[]Pet] { return Ok([]Pet{{Name: \"Rex\"}}) }\n",
		"views/pets.templ": "package views\n\nimport \"example.com/s/data\"\n\ntempl Pets(pets []data.Pet) {\n\tfor _, p := range pets {\n\t\t<p>{ p.Name }</p>\n\t}\n}\n",
		"main.vuka": `package main

import (
	"context"
	"os"

	"example.com/s/data"
	"example.com/s/views"
)

func run() Result[int] {
	pets := data.All()?
	views.Pets(pets).Render(context.Background(), os.Stdout)
	return Ok(len(pets))
}

func main() { run() }
`,
	}
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/s")
	root := tempModule(t, files)
	s, err := load.NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string) {
		t.Helper()
		pkgs, err := load.Discover(root, "example.com/s", root, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, _, gotErr := s.Transpile(pkgs, t.TempDir(), load.Options{})
		want, _, wantErr := load.Transpile(pkgs, t.TempDir(), load.Options{})
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
			t.Fatalf("%s: errors: session %v, Transpile %v", what, gotErr, wantErr)
		}
		if wantErr != nil {
			t.Fatalf("%s: %v", what, wantErr)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: session made %d files, Transpile %d", what, len(got), len(want))
		}
		for i := range got {
			if got[i].Target != want[i].Target || string(got[i].Src) != string(want[i].Src) {
				t.Fatalf("%s: %s differs:\n%s\n---\n%s", what, got[i].Target, got[i].Src, want[i].Src)
			}
		}
	}
	edit := func(file, old, new string) {
		t.Helper()
		path := filepath.Join(root, file)
		src, _ := os.ReadFile(path)
		if !strings.Contains(string(src), old) {
			t.Fatalf("%s has no %q", file, old)
		}
		writeFile(t, path, strings.Replace(string(src), old, new, 1))
	}
	check("first")
	check("again")
	edit("views/pets.templ", "<p>{ p.Name }</p>", "<li>{ p.Name }</li>")
	check("a .templ body")
	edit("views/pets.templ", "templ Pets(pets []data.Pet) {", "templ Pets(pets []data.Pet, title string) {\n\t<h1>{ title }</h1>")
	edit("main.vuka", "views.Pets(pets)", "views.Pets(pets, \"Pets\")")
	check("a .templ signature")
	edit("data/data.vuka", "type Pet struct{ Name string }", "type Pet struct{ Name, Kind string }")
	check("a type the .templ package uses")
	edit("data/data.vuka", "func All() Result[[]Pet]", "func All() Option[[]Pet]")
	edit("data/data.vuka", "return Ok([]Pet{{Name: \"Rex\"}})", "return Some([]Pet{{Name: \"Rex\"}})")
	edit("main.vuka", "func run() Result[int] {", "func run() Option[int] {")
	edit("main.vuka", "return Ok(len(pets))", "return Some(len(pets))")
	check("Result to Option")
}
