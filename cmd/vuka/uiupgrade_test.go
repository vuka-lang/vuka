package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	modmodule "golang.org/x/mod/module"
	"golang.org/x/mod/zip"
)

func TestMerge3(t *testing.T) {
	for _, c := range []struct {
		name, base, ours, theirs, want string
		conflicts                      int
	}{
		{"theirs only", "a\nb\nc\n", "a\nb\nc\n", "a\nB\nc\n", "a\nB\nc\n", 0},
		{"ours only", "a\nb\nc\n", "a\nb\nC\n", "a\nb\nc\n", "a\nb\nC\n", 0},
		{"both apart", "a\nb\nc\nd\ne\n", "A\nb\nc\nd\ne\n", "a\nb\nc\nd\nE\n", "A\nb\nc\nd\nE\n", 0},
		{"the same change", "a\nb\n", "a\nX\n", "a\nX\n", "a\nX\n", 0},
		{"insertions apart", "a\nb\nc\n", "a\nours\nb\nc\n", "a\nb\nc\ntheirs\n", "a\nours\nb\nc\ntheirs\n", 0},
		{"a conflict", "a\nb\nc\n", "a\nmine\nc\n", "a\nnew\nc\n", "a\n<<<<<<< yours\nmine\n=======\nnew\n>>>>>>> lib@v2\nc\n", 1},
		{"no newline at the end", "a\nb", "a\nmine", "a\nnew", "a\n<<<<<<< yours\nmine\n=======\nnew\n>>>>>>> lib@v2\n", 1},
	} {
		got, n := merge3([]byte(c.base), []byte(c.ours), []byte(c.theirs), "lib@v2")
		if string(got) != c.want || n != c.conflicts {
			t.Errorf("%s: %d conflicts\n%s\nwant %d\n%s", c.name, n, got, c.conflicts, c.want)
		}
	}
}

// widgetsProxy is a module proxy in a directory serving the stand-in
// library (testdata/uilib/widgets) as example.com/widgets v1.0.0 and, with
// changes applies made, v1.1.0; the go command reads it, into a module
// cache of the test's own.
func widgetsProxy(t *testing.T, changes func(dir string)) {
	t.Helper()
	proxy, work := t.TempDir(), t.TempDir()
	lib, _ := filepath.Abs(filepath.Join("testdata", "uilib", "widgets"))
	var list []string
	for _, v := range []string{"v1.0.0", "v1.1.0"} {
		dir := filepath.Join(work, v)
		if err := os.CopyFS(dir, os.DirFS(lib)); err != nil {
			t.Fatal(err)
		}
		if v == "v1.1.0" {
			changes(dir)
		}
		at := filepath.Join(proxy, "example.com", "widgets", "@v")
		os.MkdirAll(at, 0o755)
		zf, err := os.Create(filepath.Join(at, v+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		if err := zip.CreateFromDir(zf, modmodule.Version{Path: "example.com/widgets", Version: v}, dir); err != nil {
			t.Fatal(err)
		}
		zf.Close()
		writeFile(t, filepath.Join(at, v+".mod"), read(t, filepath.Join(dir, "go.mod")))
		writeFile(t, filepath.Join(at, v+".info"), `{"Version":"`+v+`","Time":"2026-01-01T00:00:00Z"}`)
		list = append(list, v)
	}
	writeFile(t, filepath.Join(proxy, "example.com", "widgets", "@v", "list"), strings.Join(list, "\n")+"\n")
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOFLAGS", "-modcacherw")
}

// versionedWidgetsApp is widgetsApp with the library read from its module
// at version.
func versionedWidgetsApp(t *testing.T, version string) string {
	t.Helper()
	dir := widgetsApp(t)
	setWidgetsVersion(t, dir, version)
	return dir
}

func setWidgetsVersion(t *testing.T, dir, version string) {
	t.Helper()
	toml := read(t, filepath.Join(dir, "vuka.toml"))
	lines := strings.Split(toml, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "dir = "):
			lines[i] = "module = \"example.com/widgets\"\nversion = \"" + version + "\""
		case strings.HasPrefix(l, "version = "):
			lines[i] = "version = \"" + version + "\""
		}
	}
	writeFile(t, filepath.Join(dir, "vuka.toml"), strings.Join(lines, "\n"))
}

func replaceIn(t *testing.T, file, old, new string) {
	t.Helper()
	src := read(t, file)
	if !strings.Contains(src, old) {
		t.Fatalf("%s hasn't %q", file, old)
	}
	writeFile(t, file, strings.Replace(src, old, new, 1))
}

// TestUIUpgrade: vuka ui upgrade takes a library's components to its new
// version keeping the app's edits: a file only the library changed is
// replaced, one both changed apart is merged, one both changed in the same
// place gets conflict markers and keeps its component at the old version
// in [ui.sources]; a file new in the library is written; --check reports
// and fails, changing nothing; once the conflict is resolved, upgrade
// again records the new version.
func TestUIUpgrade(t *testing.T) {
	widgetsProxy(t, func(lib string) {
		replaceIn(t, filepath.Join(lib, "components/button/button.templ"), "\t\tdata-variant={ string(p.Variant) }\n", "\t\tdata-variant={ string(p.Variant) }\n\t\tdata-slot=\"button\"\n")
		replaceIn(t, filepath.Join(lib, "components/dialog/dialog.templ"), "type Props struct {\n\tID string\n}", "type Props struct {\n\tID    string\n\tClass string\n}")
		replaceIn(t, filepath.Join(lib, "utils/utils.go"), "package utils", "// Package utils helps the components.\npackage utils")
		writeFile(t, filepath.Join(lib, "components/button/group.templ"), "package button\n\ntempl Group() {\n\t<div role=\"group\">{ children... }</div>\n}\n")
	})
	dir := versionedWidgetsApp(t, "v1.0.0")
	mustUI(t, dir, "add", "widgets/dialog")
	if toml := read(t, filepath.Join(dir, "vuka.toml")); !strings.Contains(toml, `dialog = "widgets@v1.0.0"`) {
		t.Fatalf("vuka.toml:\n%s", toml)
	}
	if out := mustUI(t, dir, "upgrade", "--check"); !strings.Contains(out, "the app's library components are up to date") {
		t.Errorf("upgrade --check at the version taken:\n%s", out)
	}

	button := filepath.Join(dir, "components/ui/button/button.templ")
	dialog := filepath.Join(dir, "components/ui/dialog/dialog.templ")
	replaceIn(t, button, "\tVariantOutline Variant = \"outline\"\n", "\tVariantOutline Variant = \"outline\"\n\tVariantGhost   Variant = \"ghost\"\n")
	replaceIn(t, dialog, "type Props struct {\n\tID string\n}", "type Props struct {\n\tID    string\n\tTitle string\n}")
	setWidgetsVersion(t, dir, "v1.1.0")

	before := snapshot(t, dir)
	out, err := runUI(t, dir, "upgrade", "--check")
	if err == nil || !strings.Contains(err.Error(), "widgets/button, widgets/dialog, widgets/utils have changes upstream") {
		t.Errorf("upgrade --check: %v", err)
	}
	for _, want := range []string{
		"widgets/button: v1.0.0 → v1.1.0, changed upstream: button.templ, group.templ; edited in the app: button.templ",
		"widgets/dialog: v1.0.0 → v1.1.0, changed upstream: dialog.templ; edited in the app: dialog.templ",
		"widgets/shared: v1.0.0 → v1.1.0, no changes upstream",
		"widgets/utils: v1.0.0 → v1.1.0, changed upstream: utils.go\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("upgrade --check doesn't say %q:\n%s", want, out)
		}
	}
	if after := snapshot(t, dir); len(after) != len(before) || after["vuka.toml"] != before["vuka.toml"] || after["components/ui/button/button.templ"] != before["components/ui/button/button.templ"] {
		t.Errorf("upgrade --check changed files")
	}

	out, err = runUI(t, dir, "upgrade")
	if err == nil || !strings.Contains(err.Error(), "widgets/dialog has conflicts") {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}
	for _, want := range []string{
		"wrote components/ui/button/button.templ: widgets@v1.1.0, merged with the app's changes",
		"wrote components/ui/button/group.templ: new in widgets@v1.1.0",
		"wrote components/ui/dialog/dialog.templ: 1 conflict to resolve",
		"wrote components/ui/utils/utils.go: widgets@v1.1.0\n",
		"wrote vuka.toml: [ui.sources]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("upgrade doesn't say %q:\n%s", want, out)
		}
	}
	if src := read(t, button); !strings.Contains(src, `VariantGhost   Variant = "ghost"`) || !strings.Contains(src, `data-slot="button"`) {
		t.Errorf("button.templ isn't merged:\n%s", src)
	}
	src := read(t, dialog)
	if want := "type Props struct {\n<<<<<<< yours\n\tID    string\n\tTitle string\n=======\n\tID    string\n\tClass string\n>>>>>>> widgets@v1.1.0\n}"; !strings.Contains(src, want) {
		t.Errorf("dialog.templ's conflict:\n%s", src)
	}
	toml := read(t, filepath.Join(dir, "vuka.toml"))
	for _, want := range []string{`button = "widgets@v1.1.0"`, `dialog = "widgets@v1.0.0"`, `utils = "widgets@v1.1.0"`, `shared = "widgets@v1.1.0"`, "[ui.upgrading]\ndialog = \"widgets@v1.1.0\"\n"} {
		if !strings.Contains(toml, want) {
			t.Errorf("vuka.toml hasn't %s:\n%s", want, toml)
		}
	}

	if _, err := runUI(t, dir, "upgrade"); err == nil || !strings.Contains(err.Error(), "widgets/dialog has conflicts") {
		t.Errorf("upgrade with the markers left: %v", err)
	}
	if out, _ := runUI(t, dir, "upgrade", "--check"); !strings.Contains(out, "widgets/dialog: v1.0.0 → v1.1.0, conflicts to resolve in dialog.templ") {
		t.Errorf("upgrade --check with the markers left:\n%s", out)
	}
	writeFile(t, dialog, strings.Replace(src, "<<<<<<< yours\n\tID    string\n\tTitle string\n=======\n\tID    string\n\tClass string\n>>>>>>> widgets@v1.1.0\n", "\tID    string\n\tTitle string\n\tClass string\n", 1))
	out = mustUI(t, dir, "upgrade")
	if !strings.Contains(out, "widgets/dialog: its conflicts are resolved") || strings.Contains(out, "wrote components") {
		t.Errorf("upgrade after resolving:\n%s", out)
	}
	if toml := read(t, filepath.Join(dir, "vuka.toml")); !strings.Contains(toml, `dialog = "widgets@v1.1.0"`) || strings.Contains(toml, "ui.upgrading") || strings.Contains(toml, "\n\n\n") {
		t.Errorf("vuka.toml:\n%s", toml)
	}
	if out := mustUI(t, dir, "upgrade", "--check"); !strings.Contains(out, "the app's library components are up to date") {
		t.Errorf("upgrade --check after:\n%s", out)
	}
}

// TestUIUpgradeErrors: what vuka ui upgrade says of components it can't
// take.
func TestUIUpgradeErrors(t *testing.T) {
	dir := widgetsApp(t)
	if _, err := runUI(t, dir, "upgrade"); err == nil || !strings.Contains(err.Error(), "vuka.toml has no [ui]") {
		t.Errorf("without [ui]: %v", err)
	}
	mustUI(t, dir, "add", "widgets/button")
	for args, want := range map[string]string{
		"widgets/buton": `widgets/buton came from no library (did you mean widgets/button?)`,
		"cn":            "the app's library components are widgets/button, widgets/utils (vuka's own components: vuka ui diff, then vuka ui add -f)",
	} {
		if _, err := runUI(t, dir, "upgrade", args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("upgrade %s: %v, want %q", args, err, want)
		}
	}
	if out := mustUI(t, dir, "upgrade"); !strings.Contains(out, "widgets/button, widgets/utils: read from a directory, with no version to merge from") {
		t.Errorf("a library read from a directory:\n%s", out)
	}
	if _, err := runUI(t, dir, "upgrade", "--force"); err == nil || !strings.Contains(err.Error(), "no flag --force") {
		t.Errorf("--force: %v", err)
	}
}
