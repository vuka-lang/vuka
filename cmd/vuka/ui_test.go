package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
)

// runUI runs vuka ui args in the module at dir.
func runUI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	var out bytes.Buffer
	err := uiCmd(args, &out)
	return out.String(), err
}

func mustUI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runUI(t, dir, args...)
	if err != nil {
		t.Fatalf("vuka ui %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// uiGoMod is an app's go.mod requiring the ui library the components use.
const uiGoMod = "module example.com/shop\n\ngo 1.25\n\nrequire github.com/vuka-lang/ui/v2 v2.10.0\n"

// uiApp is a module like vuka new's: a stylesheet with a theme of its own
// and a vuka.toml with formatting options.
func uiApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), uiGoMod)
	writeFile(t, filepath.Join(dir, "styles/app.css"), "@import \"tailwindcss\";\n\n/* The theme. */\n@theme {\n  --color-brand-500: #e8662c;\n}\n")
	writeFile(t, filepath.Join(dir, "vuka.toml"), "[fmt]\ntailwind = true\n")
	return dir
}

// snapshot is every file under dir and its content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files[filepath.ToSlash(rel)] = read(t, p)
		}
		return nil
	})
	return files
}

func TestUIInit(t *testing.T) {
	dir := uiApp(t)
	out := mustUI(t, dir, "init")
	for _, w := range []string{"wrote styles/app.css: the design tokens\n", "wrote components/ui/cn.go\n", "wrote components/ui/cn_test.go\n",
		"wrote vuka.toml: [ui]\n", "\timport design \"example.com/shop/components/ui\"\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
	css := read(t, filepath.Join(dir, "styles/app.css"))
	tokens, _ := registryFS.ReadFile("registry/styles/default.css")
	if want := "@import \"tailwindcss\";\n\n" + string(tokens) + "\n/* The theme. */\n@theme {"; !strings.HasPrefix(css, want) {
		t.Errorf("the stylesheet: the tokens go after its imports, before its own theme:\n%s", css)
	}
	for _, w := range []string{"--color-primary: var(--primary);", ":root {\n  --radius: 0.625rem;", "@media (prefers-color-scheme: dark) {\n  :root:not([data-theme=\"light\"]) {",
		":root[data-theme=\"dark\"] {\n  --background: oklch(0.145 0 0);"} {
		if !strings.Contains(css, w) {
			t.Errorf("the stylesheet lacks %q", w)
		}
	}
	has(t, filepath.Join(dir, "components/ui/cn.go"), "package design\n", "func cn(classes ...string) string")
	toml := read(t, filepath.Join(dir, "vuka.toml"))
	if want := "[fmt]\ntailwind = true\n\n[ui]\nstyle = \"default\"\ncomponents = \"components/ui\"\npackage = \"design\"\ncss = \"styles/app.css\"\n"; toml != want {
		t.Errorf("vuka.toml:\n%s", toml)
	}
	if o, err := format.LoadOptions(dir); err != nil || !o.Tailwind {
		t.Errorf("vuka fmt reads vuka.toml with [ui]: %+v %v", o, err)
	}

	// Again: nothing changes.
	before := snapshot(t, dir)
	out = mustUI(t, dir, "init")
	if want := "styles/app.css has the design tokens (-f writes them again)\nvuka.toml has [ui]\n"; out != want {
		t.Errorf("init again said:\n%s", out)
	}
	if after := snapshot(t, dir); !maps.Equal(before, after) {
		t.Errorf("init again changed the app")
	}

	// -f writes the tokens again, in place; the rest of the stylesheet stays.
	writeFile(t, filepath.Join(dir, "styles/app.css"), strings.Replace(css, "--primary: oklch(0.205 0 0);", "--primary: oklch(0.5 0.2 260);", 1)+"\n.mine {}\n")
	out = mustUI(t, dir, "init", "-f")
	if !strings.Contains(out, "wrote styles/app.css: the design tokens") {
		t.Errorf("init -f said:\n%s", out)
	}
	if got := read(t, filepath.Join(dir, "styles/app.css")); got != css+"\n.mine {}\n" {
		t.Errorf("init -f:\n%s", got)
	}

	// A theme of the app's own is left alone; a dry run writes nothing.
	dir = uiApp(t)
	writeFile(t, filepath.Join(dir, "styles/app.css"), "@import \"tailwindcss\";\n@theme inline {\n  --color-background: var(--background);\n}\n")
	before = snapshot(t, dir)
	out = mustUI(t, dir, "init", "--dry-run")
	if want := "styles/app.css has design tokens of its own (--color-background); -f adds vuka ui's after its imports\nwould write components/ui/cn.go\nwould write components/ui/cn_test.go\nwould write vuka.toml: [ui]\n"; out != want {
		t.Errorf("init --dry-run said:\n%s", out)
	}
	if after := snapshot(t, dir); !maps.Equal(before, after) {
		t.Errorf("init --dry-run changed the app")
	}
}

func TestUIInitOptions(t *testing.T) {
	// No stylesheet: styles/app.css, importing Tailwind; another directory
	// and package.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), uiGoMod)
	mustUI(t, dir, "init", "--dir", "internal/kit", "--package=kit")
	if css := read(t, filepath.Join(dir, "styles/app.css")); !strings.HasPrefix(css, "@import \"tailwindcss\";\n\n/* vuka ui: the design tokens") {
		t.Errorf("the new stylesheet:\n%s", css)
	}
	has(t, filepath.Join(dir, "internal/kit/cn.go"), "\npackage kit\n")
	has(t, filepath.Join(dir, "internal/kit/cn_test.go"), "package kit\n")
	has(t, filepath.Join(dir, "vuka.toml"), "components = \"internal/kit\"\npackage = \"kit\"\n")
	mustUI(t, dir, "add", "card")
	has(t, filepath.Join(dir, "internal/kit/card.vuka.html"), "<script lang=\"go\">\npackage kit\n")

	// The [ui] table changes with the flags.
	mustUI(t, dir, "init", "--css", "web/site.css")
	has(t, filepath.Join(dir, "vuka.toml"), "css = \"web/site.css\"\n")
	has(t, filepath.Join(dir, "web/site.css"), "@import \"tailwindcss\";", "--color-ring")
	if n := strings.Count(read(t, filepath.Join(dir, "vuka.toml")), "[ui]"); n != 1 {
		t.Errorf("vuka.toml has %d [ui] tables", n)
	}

	for args, msg := range map[string]string{
		"init --package ui":   "--package ui: a package name in lower case, other than ui",
		"init --package Kit":  "--package Kit: a package name in lower case",
		"init --style new":    `no style "new"; the styles are default`,
		"init --dir ../x":     "--dir ../x: a directory inside the module",
		"init --fource":       "no flag --fource (did you mean --force?)",
		"init button":         "vuka ui init takes no arguments (vuka ui add button adds components)",
		"add --package kit x": "vuka ui add: no flag --package",
		"inti":                `vuka ui: no command "inti" (did you mean init?)`,
	} {
		if _, err := runUI(t, dir, strings.Fields(args)...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("vuka ui %s: %v, want %q", args, err, msg)
		}
	}
}

func TestUIAdd(t *testing.T) {
	dir := uiApp(t)
	if _, err := runUI(t, dir, "add", "button"); err == nil || !strings.Contains(err.Error(), "vuka.toml has no [ui]: run vuka ui init first") {
		t.Errorf("add before init: %v", err)
	}
	mustUI(t, dir, "init")

	// A component and those it uses.
	out := mustUI(t, dir, "add", "dialog")
	if !strings.HasPrefix(out, "wrote components/ui/button.vuka.html\nwrote components/ui/dialog.vuka.html\n\nUse them as tags (import design \"example.com/shop/components/ui\"):\n") {
		t.Errorf("add dialog said:\n%s", out)
	}
	for _, f := range []string{"button.vuka.html", "dialog.vuka.html"} {
		want, _ := registryFS.ReadFile("registry/ui/" + f)
		if got := read(t, filepath.Join(dir, "components/ui", f)); got != string(want) {
			t.Errorf("%s isn't the registry's", f)
		}
	}
	out = mustUI(t, dir, "add", "Tabs", "card")
	if !strings.HasPrefix(out, "wrote components/ui/tabs.vuka.html\nwrote components/ui/tabs.go\nwrote components/ui/card.vuka.html\n") {
		t.Errorf("add Tabs card said:\n%s", out)
	}
	out = mustUI(t, dir, "add", "dialog")
	if out != "components/ui/dialog.vuka.html is up to date\n" {
		t.Errorf("add dialog again said:\n%s", out)
	}

	// A file of the app's that differs: add prints the difference and
	// stops; a dry run only says so; -f overwrites it.
	button := filepath.Join(dir, "components/ui/button.vuka.html")
	orig := read(t, button)
	mine := strings.Replace(orig, "rounded-md text-sm font-medium", "rounded-full text-sm font-semibold", 1)
	writeFile(t, button, mine)
	out, err := runUI(t, dir, "add", "button")
	if err == nil || err.Error() != "vuka ui add: components/ui/button.vuka.html differs from the registry's (the difference is above); -f overwrites it" {
		t.Errorf("add over a changed file: %v", err)
	}
	for _, w := range []string{"components/ui/button.vuka.html differs from the registry's:\n", "--- components/ui/button.vuka.html\n+++ components/ui/button.vuka.html (vuka ui add)\n@@ -",
		"\n-const buttonBase = \"inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm font-semibold",
		"\n+const buttonBase = \"inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium",
		"\n \tChildren ui.Node\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("the difference lacks %q:\n%s", w, out)
		}
	}
	if read(t, button) != mine {
		t.Errorf("add changed the app's file")
	}
	// The dialog uses the button: the app's, kept.
	out = mustUI(t, dir, "add", "dialog")
	if !strings.Contains(out, "components/ui/button.vuka.html is yours, differing from the registry's (vuka ui diff button shows how)\n") || read(t, button) != mine {
		t.Errorf("add dialog over a changed button:\n%s", out)
	}
	out = mustUI(t, dir, "add", "--dry-run", "button", "badge")
	if !strings.Contains(out, "would write components/ui/badge.vuka.html\n") || !strings.Contains(out, "+++ components/ui/button.vuka.html (vuka ui add)") {
		t.Errorf("add --dry-run said:\n%s", out)
	}
	if exists(dir, "components/ui/badge.vuka.html") || read(t, button) != mine {
		t.Errorf("add --dry-run wrote")
	}
	cn := filepath.Join(dir, "components/ui/cn.go")
	writeFile(t, cn, read(t, cn)+"\n// the app's\n")
	out = mustUI(t, dir, "add", "-f", "button")
	if !strings.Contains(out, "wrote components/ui/button.vuka.html\n") || read(t, button) != orig {
		t.Errorf("add -f:\n%s", out)
	}
	if !strings.HasSuffix(read(t, cn), "// the app's\n") {
		t.Errorf("add -f button overwrote cn.go, which it only uses")
	}

	// Unknown names.
	for args, msg := range map[string]string{
		"add buton":    `vuka ui add: no component "buton" (did you mean button?); the components are alert, badge, button, card, dialog, input, label, skeleton, tabs`,
		"add carousel": `vuka ui add: no component "carousel"; the components are alert, badge, button, card, dialog, input, label, skeleton, tabs`,
		"add":          "usage: vuka ui add",
	} {
		if _, err := runUI(t, dir, strings.Fields(args)...); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("vuka ui %s: %v, want %q", args, err, msg)
		}
	}
	if out := mustUI(t, dir, "list"); !strings.Contains(out, "tabs     live     ") || !strings.Contains(out, "dialog   browser  ") || strings.Contains(out, "cn ") {
		t.Errorf("list:\n%s", out)
	}
}

// The registry's index and files agree: each file is listed once, exists
// and has the digest the index says (go test -run TestUIRegistry -update
// writes them), each dependency is an entry, each file is in the package
// the index says, and the conformance example's components are the
// registry's.
func TestUIRegistry(t *testing.T) {
	reg, err := readRegistry(regSource{})
	if err != nil {
		t.Fatal(err)
	}
	stale := false
	digest := func(f *registryFile) []byte {
		src, err := registryFS.ReadFile("registry/" + f.Path)
		if err != nil {
			t.Error(err)
			return nil
		}
		if sum := sha256Hex(src); sum != f.SHA256 {
			if !*updateFiles {
				t.Errorf("%s: the index's sha256 is %s, the file's %s: go test -run TestUIRegistry -update", f.Path, f.SHA256, sum)
			}
			f.SHA256, stale = sum, true
		}
		return src
	}
	listed := map[string]bool{}
	for i := range reg.Items {
		it := &reg.Items[i]
		if it.Title == "" || it.Description == "" {
			t.Errorf("%s: needs a title and a description", it.Name)
		}
		if _, err := reg.resolve("add", []string{it.Name}); err != nil {
			t.Error(err)
		}
		for j := range it.Files {
			f := &it.Files[j]
			if listed[f.Path] {
				t.Errorf("%s is listed twice", f.Path)
			}
			listed[f.Path] = true
			src := digest(f)
			if src != nil && !bytes.Contains(src, []byte("\npackage "+reg.Package+"\n")) && !bytes.HasPrefix(src, []byte("package "+reg.Package+"\n")) {
				t.Errorf("%s isn't in package %s", f.Path, reg.Package)
			}
		}
		if it.Tier > 0 && uiExample[it.Name] == "" {
			t.Errorf("no example of %s for vuka ui add to print", it.Name)
		}
	}
	for name, s := range reg.Styles {
		digest(&s)
		reg.Styles[name] = s
	}
	if stale && *updateFiles {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reg); err != nil {
			t.Fatal(err)
		}
		writeFile(t, "registry/index.json", b.String())
	}
	files, _ := fs.Glob(registryFS, "registry/ui/*")
	for _, f := range files {
		if !listed[strings.TrimPrefix(f, "registry/")] {
			t.Errorf("%s isn't in the index", f)
		}
	}
	example := "../../examples/conformance/uikit/vuka/components/ui"
	got, _ := filepath.Glob(filepath.Join(example, "*"))
	if len(got) != len(files) {
		t.Errorf("%s has %d files, the registry %d", example, len(got), len(files))
	}
	for _, f := range files {
		want, _ := registryFS.ReadFile(f)
		if have, err := os.ReadFile(filepath.Join(example, filepath.Base(f))); err != nil || !bytes.Equal(have, want) {
			t.Errorf("%s isn't the registry's %s: cp registry/ui/* %s", filepath.Join(example, filepath.Base(f)), f, example)
		}
	}
}

func TestUnifiedText(t *testing.T) {
	a := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"
	b := "1\n2\n3\nfour\n5\n6\n7\n8\n9\n10\n11\n12\n13"
	want := `--- f
+++ f (vuka ui add)
@@ -1,7 +1,7 @@
 1
 2
 3
-4
+four
 5
 6
 7
@@ -10,3 +10,4 @@
 10
 11
 12
+13
\ No newline at end of file
`
	if got := string(unifiedText("f", []byte(a), []byte(b))); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := string(unifiedText("f", []byte("x\n"), []byte(""))); got != "--- f\n+++ f (vuka ui add)\n@@ -1 +0,0 @@\n-x\n" {
		t.Errorf("all removed:\n%s", got)
	}
}
