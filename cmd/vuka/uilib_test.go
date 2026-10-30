package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// widgetsApp is an app whose vuka.toml names the stand-in library
// (testdata/uilib/widgets) and requires templ directly, as vuka ui add
// leaves it, so nothing is fetched.
func widgetsApp(t *testing.T) string {
	t.Helper()
	dir := uiApp(t)
	lib, _ := filepath.Abs(filepath.Join("testdata", "uilib", "widgets"))
	writeFile(t, filepath.Join(dir, "go.mod"), uiGoMod+"\nrequire github.com/a-h/templ v0.3.1020\n")
	writeFile(t, filepath.Join(dir, "pages/_layout.vuka.html"), `<html lang="en" class="h-full bg-slate-50 antialiased dark:bg-slate-950">
  <body class="h-full font-sans text-slate-900 dark:text-slate-100">{Children}</body>
</html>
`)
	writeFile(t, filepath.Join(dir, "vuka.toml"), `[fmt]
tailwind = true

[ui.libraries.widgets]
dir = "`+filepath.ToSlash(lib)+`" # the library's source
scripts = ["shared"]
theme = "assets/css/theme.css"
css = ["assets/css/extras.css"]
style = "assets/css/style.css"
`)
	return dir
}

// TestUILibraryAdd: vuka ui add library/name in an app without [ui] runs
// init, writes the components, the packages they import and the shared
// scripts into components/ui with the library's imports rewritten, the
// theme into styles/widgets.css (imported once), and [ui.sources]; adding
// another later keeps what's there and rewrites the theme with its
// classes; vuka ui diff and list report them; --dry-run writes nothing.
func TestUILibraryAdd(t *testing.T) {
	dir := widgetsApp(t)
	before := snapshot(t, dir)
	out := mustUI(t, dir, "add", "--dry-run", "widgets/dialog")
	if !strings.Contains(out, "would write components/ui/dialog/dialog.templ") {
		t.Errorf("--dry-run:\n%s", out)
	}
	if after := snapshot(t, dir); len(after) != len(before) {
		t.Fatalf("--dry-run wrote files")
	}

	out = mustUI(t, dir, "add", "widgets/dialog")
	for _, want := range []string{"vuka.toml has no [ui]: vuka ui init", "wrote components/ui/dialog/dialog.templ", "wrote components/ui/dialog/dialog.js",
		"wrote components/ui/button/button.templ", "wrote components/ui/utils/utils.go", "wrote components/ui/shared/portal.js",
		"wrote styles/widgets.css", "wrote styles/app.css: imports widgets.css", "wrote vuka.toml: [ui.sources]"} {
		if !strings.Contains(out, want) {
			t.Errorf("vuka ui add doesn't say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Add components with vuka ui add") || strings.Contains(out, "go get") {
		t.Errorf("vuka ui add:\n%s", out)
	}
	files := snapshot(t, dir)
	if src := files["components/ui/dialog/dialog.templ"]; !strings.Contains(src, `"example.com/shop/components/ui/button"`) || strings.Contains(src, "example.com/widgets") {
		t.Errorf("dialog.templ:\n%s", src)
	}
	for _, f := range []string{"components/ui/dialog/dialog.min.js", "components/ui/dialog/dialog_templ.go", "components/ui/select/select.templ"} {
		if _, ok := files[f]; ok {
			t.Errorf("vuka ui add wrote %s", f)
		}
	}
	toml := files["vuka.toml"]
	if !strings.Contains(toml, "[ui.sources]\nbutton = \"widgets\"\ndialog = \"widgets\"\nshared = \"widgets\"\nutils = \"widgets\"\n") || !strings.Contains(toml, "# the library's source") {
		t.Errorf("vuka.toml:\n%s", toml)
	}
	if _, err := load.ParseCompile("vuka.toml", []byte(toml)); err != nil {
		t.Errorf("the compiler reads vuka.toml: %v", err)
	}
	if css := files["styles/app.css"]; strings.Count(css, `@import "./widgets.css";`) != 1 || !strings.Contains(css, tokensStart) {
		t.Errorf("styles/app.css:\n%s", css)
	}
	if l := files["pages/_layout.vuka.html"]; !strings.Contains(l, `class="h-full bg-background text-foreground antialiased"`) || !strings.Contains(l, `<body class="h-full font-sans">`) {
		t.Errorf("the layout's colours:\n%s", l)
	}
	theme := files["styles/widgets.css"]
	if !strings.Contains(theme, ".wd-dialog {") || !strings.Contains(theme, ".wd-button-outline {") || strings.Contains(theme, ".wd-unused") {
		t.Errorf("styles/widgets.css:\n%s", theme)
	}

	out = mustUI(t, dir, "add", "widgets/select")
	if !strings.Contains(out, "wrote components/ui/select/select.templ") || strings.Contains(out, "wrote components/ui/utils") || strings.Contains(out, "wrote styles/app.css") {
		t.Errorf("adding another:\n%s", out)
	}
	if s := read(t, filepath.Join(dir, "vuka.toml")); !strings.Contains(s, `select = "widgets"`) || strings.Count(s, "[ui.sources]") != 1 {
		t.Errorf("vuka.toml:\n%s", s)
	}
	if out := mustUI(t, dir, "add", "widgets/select"); !strings.Contains(out, "components/ui/select/select.templ is up to date") {
		t.Errorf("adding it again:\n%s", out)
	}

	writeFile(t, filepath.Join(dir, "components/ui/utils/utils.go"), files["components/ui/utils/utils.go"]+"\n// the app's\n")
	if out := mustUI(t, dir, "diff", "widgets/utils"); !strings.Contains(out, "+++ components/ui/utils/utils.go (vuka ui add)") || !strings.Contains(out, "vuka ui add -f widgets/utils") {
		t.Errorf("vuka ui diff widgets/utils:\n%s", out)
	}
	if out := mustUI(t, dir, "add", "widgets/dialog"); !strings.Contains(out, "components/ui/utils/utils.go is yours, differing from widgets's") {
		t.Errorf("a dependency the app changed:\n%s", out)
	}
	out = mustUI(t, dir, "diff")
	if !strings.Contains(out, "vuka ui add -f widgets/utils takes the libraries'; widgets/button, widgets/dialog, widgets/select, widgets/shared: the same") || !strings.Contains(out, "cn: the same as the registry's") {
		t.Errorf("vuka ui diff:\n%s", out)
	}
	if out := mustUI(t, dir, "list", "widgets"); !strings.Contains(out, "widgets/unused\n") || !strings.Contains(out, "widgets/select  (the app has it: widgets)") {
		t.Errorf("vuka ui list widgets:\n%s", out)
	}
	if out := mustUI(t, dir, "list", "--offline"); !strings.Contains(out, "From libraries (vuka.toml's [ui.sources]):") || !strings.Contains(out, "widgets/dialog") {
		t.Errorf("vuka ui list:\n%s", out)
	}

}

// TestUILibraryErrors: what vuka ui add says of a library or component it
// can't find.
func TestUILibraryErrors(t *testing.T) {
	dir := widgetsApp(t)
	for args, want := range map[string]string{
		"widgets/dailog":   `widgets has no component "dailog" (did you mean dialog?); its components are button, dialog, select, unused`,
		"widget/dialog":    `no library "widget" (did you mean widgets?)`,
		"widgets/":         "a library's component is library/name",
		"widgets/a/b":      "a library's component is library/name",
		"widgets/Dialog_x": `widgets has no component "dialog_x"`,
	} {
		if _, err := runUI(t, dir, "add", args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("vuka ui add %s: %v, want %q", args, err, want)
		}
	}
	writeFile(t, filepath.Join(dir, "vuka.toml"), "[ui.libraries.other]\nversoin = \"v1.0.0\"\n")
	if _, err := runUI(t, dir, "add", "other/x"); err == nil || !strings.Contains(err.Error(), "unknown key versoin in [ui.libraries.other] (did you mean version?)") {
		t.Errorf("a misspelt key: %v", err)
	}
	writeFile(t, filepath.Join(dir, "vuka.toml"), "[ui.libraries.other]\nmodule = \"example.com/other\"\n")
	if _, err := runUI(t, dir, "add", "other/x"); err == nil || !strings.Contains(err.Error(), "names the version of example.com/other") {
		t.Errorf("no version: %v", err)
	}
}

// TestLibraryScripts: the scripts of an app's library components are one
// classic module, the shared ones first, whose scope marks the components
// that have a script.
func TestLibraryScripts(t *testing.T) {
	dir := widgetsApp(t)
	mustUI(t, dir, "add", "widgets/dialog", "widgets/select")
	pa := &pageAssets{root: dir, modPath: "example.com/shop", global: &assetComp{}}
	if err := pa.libraryScripts(); err != nil {
		t.Fatal(err)
	}
	if len(pa.comps) != 1 {
		t.Fatalf("components: %d", len(pa.comps))
	}
	c := pa.comps[0]
	var labels []string
	for _, s := range c.scripts {
		labels = append(labels, s.label)
	}
	if got := strings.Join(labels, " "); got != "components/ui/shared/portal.js components/ui/dialog/dialog.js components/ui/select/select.js" {
		t.Errorf("scripts: %s", got)
	}
	if !c.classic || c.id != scopeID("example.com/shop/components/ui", "library:widgets") {
		t.Errorf("the component: %+v", c)
	}
	sc := pa.scopes()
	if sc[filepath.Join(dir, "components/ui/dialog")]["Content"] != c.id || sc[filepath.Join(dir, "components/ui/select")]["Item"] != c.id || sc[filepath.Join(dir, "components/ui/button")] != nil {
		t.Errorf("scopes: %v", sc)
	}
	b, err := bundleScripts(dir, c, false)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b.js)
	if i, j := strings.Index(js, "// components/ui/shared/portal.js"), strings.Index(js, "// components/ui/dialog/dialog.js"); i < 0 || j < i || strings.Contains(js, "minified copy") || strings.Contains(js, "export") {
		t.Errorf("the bundle:\n%s", js)
	}
}

func TestThemeAdditions(t *testing.T) {
	theme, _ := os.ReadFile(filepath.Join("testdata", "uilib", "widgets", "assets", "css", "theme.css"))
	app := "@theme inline {\n  --color-background: var(--background);\n}\n:root { --background: white; --radius: 1rem; }\n"
	got := themeAdditions(string(theme), app)
	want := `
/* The theme's tokens the app's don't have. */
@theme inline {
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-surface: var(--surface);
  --radius-2xl: calc(var(--radius) * 1.8);
}
:root {
  --popover: oklch(1 0 0);
  --popover-foreground: oklch(0.1 0 0);
  --surface: oklch(0.96 0.01 250);
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    --popover: oklch(0.2 0 0);
    --popover-foreground: oklch(0.98 0 0);
    --surface: oklch(0.25 0.02 250);
  }
}
:root[data-theme="dark"] {
  --popover: oklch(0.2 0 0);
  --popover-foreground: oklch(0.98 0 0);
  --surface: oklch(0.25 0.02 250);
}
`
	if got != want {
		t.Errorf("themeAdditions:\n%s\nwant:\n%s", got, want)
	}
	if got := themeAdditions(string(theme), string(theme)); got != "" {
		t.Errorf("a theme the app has: %q", got)
	}
}

func TestStyleRules(t *testing.T) {
	style := `/* c */
.style-x {
  /* MARK: a */
  .cn-a {
    @apply p-1;
  }

  .cn-a-size-sm,
  .cn-b:hover {
    @apply p-2 { }
  }

  .cn-c { color: "}"; }
}
`
	got := styleRules(style, map[string]bool{"cn-a-": true, "cn-b": true})
	want := "  .cn-a-size-sm,\n  .cn-b:hover {\n    @apply p-2 { }\n  }\n"
	if got != want {
		t.Errorf("styleRules:\n%q\nwant:\n%q", got, want)
	}
	if got := styleRules(".cn-a { x: y; }\n.cn-b { z: w; }\n", map[string]bool{"cn-b": true}); got != "  .cn-b { z: w; }\n" {
		t.Errorf("an unwrapped style: %q", got)
	}
}

func TestSetTOMLTable(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", "[ui.sources]\na = \"x\"\n"},
		{"[ui]\nstyle = \"default\"", "[ui]\nstyle = \"default\"\n\n[ui.sources]\na = \"x\"\n"},
		{"[ui.sources]\nb = \"y\"\n\n[fmt]\nwidth = 100\n", "[ui.sources]\na = \"x\"\n\n[fmt]\nwidth = 100\n"},
	} {
		if got := string(setTOMLTable([]byte(c.in), "ui.sources", []byte("[ui.sources]\na = \"x\"\n"))); got != c.want {
			t.Errorf("setTOMLTable(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
