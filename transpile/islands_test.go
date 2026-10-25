package transpile_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A wrong value for an imported island's prop is Go's type error, at the
// attribute in the .vuka file.
func TestIslandPropTypeErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the program")
	}
	dir := "testdata/golden"
	src, err := os.ReadFile(filepath.Join(dir, "islands_typeerr.vuka"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := transpile.Package([]transpile.File{{Name: "islands_typeerr.vuka", Src: src}},
		transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	mod, sum := goldenModule(t)
	os.WriteFile(filepath.Join(tmp, "go.mod"), mod, 0o644)
	os.WriteFile(filepath.Join(tmp, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(tmp, "main.go"), append(res.Files[0].Src, "\nfunc main() {}\n"...), 0o644)
	cmd := exec.Command("go", "build", "-gcflags=-e", "-o", os.DevNull, ".")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, _ := cmd.CombinedOutput()
	for _, want := range []string{
		`islands_typeerr.vuka:9:`, `cannot use 3 (untyped int constant) as string value`,
		`islands_typeerr.vuka:10:`, `cannot use "today" (untyped string constant) as "time".Time value`,
		`islands_typeerr.vuka:11:`, `cannot use "3" (untyped string constant) as float64 value`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("go build doesn't report %s:\n%s", want, out)
		}
	}
}

func TestIslandRootImport(t *testing.T) {
	root, err := filepath.Abs("testdata/islandroot")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "pages", "(admin)", "users", "[id]")
	src, err := os.ReadFile(filepath.Join(dir, "x.vuka.html"))
	if err != nil {
		t.Fatal(err)
	}
	compile := func(root string, src []byte) (*transpile.Result, error) {
		return transpile.Package([]transpile.File{{Name: "x.vuka.html", Src: src}}, transpile.Options{
			Dir: dir, ModuleRoot: root, Importer: load.NewImporter("testdata/golden", ""),
		})
	}
	res, err := compile(root, src)
	if err != nil {
		t.Fatal(err)
	}
	compare(t, "testdata/islandroot/x.golden", res.Files[0].Src)
	for _, tc := range []struct{ name, root, path, want string }{
		{"missing", root, "@/web/islands/Missing.vue", filepath.Join(root, "web", "islands", "Missing.vue")},
		{"escape", root, "@/../Form.vue", "escapes the module root"},
		{"unknown", "", "@/web/islands/Form.vue", "main module root is unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := []byte(strings.ReplaceAll(string(src), "@/web/islands/Form.vue", tc.path))
			_, err := compile(tc.root, text)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("got %v; want %s and %s", err, tc.want, tc.path)
			}
			list, ok := err.(transpile.ErrorList)
			if !ok || len(list) == 0 || list[0].Code != "E0303" {
				t.Fatalf("want E0303: %v", err)
			}
		})
	}
	t.Run("island-only", func(t *testing.T) {
		page := []byte(strings.ReplaceAll(strings.ReplaceAll(string(src), "type Edit struct{}", "type Edit struct{ Title string }"), `<main><Form title="Edit" count={2} client:load /></main>`, `<Form title={Title} client:load />`))
		if _, err := compile(root, page); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"@/../Form.vue", "@/web/islands/Missing.vue"} {
			_, err := compile(root, []byte(strings.ReplaceAll(string(page), "@/web/islands/Form.vue", path)))
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("invalid island-only import: %v", err)
			}
		}
		if _, err := compile("", page); err == nil || !strings.Contains(err.Error(), "main module root is unknown") {
			t.Fatalf("unknown island-only root: %v", err)
		}
	})
	// The generated contract must include props imported relative to the Vue file.
	if got := string(res.Files[0].Src); !strings.Contains(got, "Title string") || !strings.Contains(got, "Count *float64") {
		t.Fatalf("missing props: %s", got)
	}
}

func TestVukaIslandRootImport(t *testing.T) {
	root, err := filepath.Abs("testdata/golden")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "islands_import.vuka"))
	if err != nil {
		t.Fatal(err)
	}
	src = []byte(strings.ReplaceAll(string(src), "./islands/Picker.vue", "@/islands/Picker.vue"))
	res, err := transpile.Package([]transpile.File{{Name: "root.vuka", Src: src}}, transpile.Options{Dir: t.TempDir(), ModuleRoot: root, Importer: load.NewImporter(root, "")})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.Files[0].Src); !strings.Contains(got, `RegisterIsland[Picker]("@/islands/Picker.vue")`) {
		t.Fatalf("registration: %s", got)
	}
}

// Adoption can mix old relative spellings with @/ in the same package.
func TestIslandImportSpellings(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pages")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Form.vue", "Other.vue"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`<script setup lang="ts">defineProps<{ title: string }>()</script>`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	shape := func(name, path string) transpile.File {
		return transpile.File{Name: name + ".vuka.html", Src: []byte(fmt.Sprintf("package pages\nimport Form %q\nimport ui \"vukatest.local/stateui\"\ntype %s struct{}\n<main><Form title=\"ok\" /></main>\n", path, name))}
	}
	opts := transpile.Options{Dir: dir, ModuleRoot: root, Importer: load.NewImporter("testdata/golden", "")}
	for _, paths := range [][2]string{{"../Form.vue", "@/Form.vue"}, {"@/Form.vue", "../Form.vue"}} {
		res, err := transpile.Package([]transpile.File{shape("First", paths[0]), shape("Second", paths[1])}, opts)
		if err != nil {
			t.Fatal(err)
		}
		var code string
		for _, file := range res.Files {
			code += string(file.Src)
		}
		if strings.Count(code, "type Form struct") != 1 {
			t.Fatalf("expected one shared props type: %s", code)
		}
	}
	_, err := transpile.Package([]transpile.File{shape("First", "../Form.vue"), shape("Second", "@/Other.vue")}, opts)
	if list, ok := err.(transpile.ErrorList); !ok || len(list) == 0 || list[0].Code != "E0302" {
		t.Fatalf("distinct targets: %v", err)
	}
}

// All import forms use the same missing-file diagnostic, at the original
// import string, with an Offset usable by partial template recovery.
func TestIslandImportMissingPositions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pages")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../Missing.vue", "@/Missing.vue"} {
		for _, kind := range []string{"wrapped", "island-only", "declared", "script-wrapped", "script-island-only"} {
			t.Run(path+kind, func(t *testing.T) {
				header := fmt.Sprintf("package pages\nimport ui \"vukatest.local/stateui\"\nimport Form %q\n", path)
				name, src := "page.vuka.html", header+"type Page struct{ Title string }\n<main><Form title={Title} /></main>\n"
				if strings.HasSuffix(kind, "island-only") {
					src = header + "type Page struct{ Title string }\n<Form title={Title} />\n"
				}
				if kind == "declared" {
					name, src = "page.vuka", header+"@ui.Island\ntype Form struct{ Title string }\n"
				}
				if strings.HasPrefix(kind, "script-") {
					at := strings.Index(src, "\n<")
					src = "<script lang=\"go\">\n" + src[:at] + "\n</script>" + src[at:]
				}
				_, err := transpile.Package([]transpile.File{{Name: name, Src: []byte(src)}}, transpile.Options{Dir: dir, ModuleRoot: root, Importer: load.NewImporter("testdata/golden", "")})
				list, ok := err.(transpile.ErrorList)
				if !ok || len(list) == 0 || list[0].Code != "E0303" {
					t.Fatalf("missing: %v", err)
				}
				e := list[0]
				wantOffset := strings.Index(src, strconv.Quote(path))
				wantLine := strings.Count(src[:wantOffset], "\n") + 1
				if e.Pos.Offset != wantOffset || e.Pos.Line != wantLine || e.Pos.Column != 13 {
					t.Fatalf("position: %+v, want offset %d at %d:13", e.Pos, wantOffset, wantLine)
				}
				target := filepath.Join(root, "Missing.vue")
				want := fmt.Sprintf("can't read island component %s: %s doesn't exist", path, target)
				if e.Msg != want || strings.Count(e.Msg, target) != 1 {
					t.Fatalf("message: %s, want %s", e.Msg, want)
				}
			})
		}
	}
}
