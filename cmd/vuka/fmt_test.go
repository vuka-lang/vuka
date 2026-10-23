package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

func TestFmt(t *testing.T) {
	const messy = "package main\n\nfunc main( ) {\n\tx:=f()?\n\t_ = <p   a=\"b\">hi</p >\n}\n"
	const tidy = "package main\n\nfunc main() {\n\tx := f()?\n\t_ = <p a=\"b\">hi</p>\n}\n"
	dir := t.TempDir()
	files := map[string]string{
		"main.vuka":                 messy,
		"ok.vuka":                   tidy,
		"sub/more.vuka":             messy,
		"sub/skip.go":               "package  sub\n",
		".hidden/x.vuka":            messy,
		"node_modules/m/x.vuka":     messy,
		"vendor/v/x.vuka":           messy,
		"build/main.vuka":           messy,
		"build/" + load.Marker:      "",
		"notbuild/build/other.vuka": messy,
	}
	for name, src := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(src), 0o644)
	}
	chdir(t, dir)

	var out, errs bytes.Buffer
	if err := vukaFmt([]string{"-l"}, &out, &errs); err != nil {
		t.Fatal(err, errs.String())
	}
	want := []string{"main.vuka", filepath.Join("notbuild", "build", "other.vuka"), filepath.Join("sub", "more.vuka")}
	if got := strings.Fields(out.String()); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("-l listed %q, want %q", got, want)
	}

	out.Reset()
	if err := vukaFmt([]string{"main.vuka"}, &out, &errs); err != nil || out.String() != tidy {
		t.Errorf("printed %q (%v)", out.String(), err)
	}
	if src, _ := os.ReadFile("main.vuka"); string(src) != messy {
		t.Error("printing changed the file")
	}

	out.Reset()
	if err := vukaFmt([]string{"-d", "main.vuka"}, &out, &errs); err != nil || !strings.Contains(out.String(), "+\tx := f()?") {
		t.Errorf("-d printed %q (%v)", out.String(), err)
	}

	out.Reset()
	if err := vukaFmt([]string{"-w", "."}, &out, &errs); err != nil {
		t.Fatal(err, errs.String())
	}
	for _, name := range []string{"main.vuka", "sub/more.vuka", "notbuild/build/other.vuka"} {
		if src, _ := os.ReadFile(name); string(src) != tidy {
			t.Errorf("%s not written: %q", name, src)
		}
	}
	for _, name := range []string{".hidden/x.vuka", "node_modules/m/x.vuka", "vendor/v/x.vuka", "build/main.vuka"} {
		if src, _ := os.ReadFile(name); string(src) != messy {
			t.Errorf("%s was formatted", name)
		}
	}
	out.Reset()
	if vukaFmt([]string{"-l"}, &out, &errs); out.Len() != 0 {
		t.Errorf("-l after -w listed %q", out.String())
	}

	os.WriteFile("bad.vuka", []byte("package main\n\nfunc main() {\n\t_ = <p>\n}\n"), 0o644)
	errs.Reset()
	if err := vukaFmt([]string{"bad.vuka"}, &out, &errs); err == nil || !strings.Contains(errs.String(), "bad.vuka:5:1: unexpected }") {
		t.Errorf("bad file: %v %q", err, errs.String())
	}
}

func TestFmtTemplate(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "layouts"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "vuka.toml"), []byte("[fmt]\nwidth = 40\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "layouts", "nav.go"), []byte("package layouts\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "layouts", "nav.vuka.html"), []byte("// Nav is the menu.\ntype Nav struct{ Items []string }\n<nav>\nfor _, i := range Items {\n<a href={i} class=\"link link-muted underline\">{i}</a>\n}\n</nav>"), 0o644)
	chdir(t, dir)
	var out, errs bytes.Buffer
	if err := vukaFmt([]string{"-check", "."}, &out, &errs); err == nil || out.String() != filepath.Join("layouts", "nav.vuka.html")+"\n" {
		t.Fatalf("-check: %v %q", err, out.String())
	}
	if err := vukaFmt([]string{"-w", "-rewrite", "."}, &out, &errs); err != nil {
		t.Fatal(err, errs.String())
	}
	want := "<script lang=\"go\">\npackage layouts\n\n// Nav is the menu.\ntype Nav struct{ Items []string }\n</script>\n\n<nav>\n  for _, i := range Items {\n    <a\n      href={i}\n      class=\"link link-muted underline\"\n    >{i}</a>\n  }\n</nav>\n"
	if src, _ := os.ReadFile(filepath.Join("layouts", "nav.vuka.html")); string(src) != want {
		t.Errorf("got\n%s\nwant\n%s", src, want)
	}
	out.Reset()
	if err := vukaFmt([]string{"-check", "."}, &out, &errs); err != nil || out.Len() != 0 {
		t.Errorf("-check after -w: %v %q", err, out.String())
	}
}
