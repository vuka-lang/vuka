package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testPkgDir, _ = os.Getwd()

// iconsApp is widgetsApp with the stand-in library copied and given an
// icon package (lucide's shape: var X = Icon("x")) its dialog and button
// draw icons from, [ui.libraries.widgets] naming it, and [ui] icons the
// app's set.
func iconsApp(t *testing.T, set string) string {
	t.Helper()
	t.Chdir(testPkgDir) // widgetsApp finds the library from there
	dir := widgetsApp(t)
	src, _ := filepath.Abs(filepath.Join("testdata", "uilib", "widgets"))
	lib := t.TempDir()
	if err := os.CopyFS(lib, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lib, "components/icon/icon.go"), `package icon

import "github.com/a-h/templ"

type Props struct{ Class string }

func Icon(name string) func(...Props) templ.Component { return func(...Props) templ.Component { return templ.NopComponent } }

var X = Icon("x")
var ChevronDown = Icon("chevron-down")
`)
	replaceIn(t, filepath.Join(lib, "components/dialog/dialog.templ"), "\t\"example.com/widgets/components/button\"\n", "\t\"example.com/widgets/components/button\"\n\t\"example.com/widgets/components/icon\"\n")
	replaceIn(t, filepath.Join(lib, "components/dialog/dialog.templ"), "\t\t\t\t{ children... }\n", "\t\t\t\t@icon.X(icon.Props{Class: \"close\"})\n\t\t\t\t@icon.ChevronDown()\n\t\t\t\t{ children... }\n")
	replaceIn(t, filepath.Join(lib, "components/button/button.templ"), "import \"example.com/widgets/utils\"\n", "import \"example.com/widgets/utils\"\n\nimport \"example.com/widgets/components/icon\"\n")
	replaceIn(t, filepath.Join(lib, "components/button/button.templ"), "\t\t{ children... }\n", "\t\t@icon.X()\n\t\t{ children... }\n")
	toml := read(t, filepath.Join(dir, "vuka.toml"))
	toml = strings.Replace(toml, filepath.ToSlash(src), filepath.ToSlash(lib), 1) + "icons = \"icon\"\n\n[ui.icons.app]\npackage = \"components/icons\"\nclose = \"Close\"\n"
	writeFile(t, filepath.Join(dir, "vuka.toml"), toml)
	mustUI(t, dir, "init")
	toml = read(t, filepath.Join(dir, "vuka.toml"))
	writeFile(t, filepath.Join(dir, "vuka.toml"), strings.Replace(toml, "css = \"styles/app.css\"\n", "css = \"styles/app.css\"\nicons = \""+set+"\"\n", 1))
	return dir
}

// TestUIIcons: with [ui] icons naming a set of the app's, vuka ui add
// writes a library's components calling the set's icon for each role the
// set maps (the library's x is the close role), importing the set's
// package and no longer the library's icons where nothing else uses
// them; a role the set hasn't keeps the library's icon, with a note; vuka
// ui diff compares with the library's files as add writes them.
func TestUIIcons(t *testing.T) {
	dir, typo, none := iconsApp(t, "app"), iconsApp(t, "ap"), iconsApp(t, "")
	out := mustUI(t, dir, "add", "widgets/dialog")
	if !strings.Contains(out, "the icon set app has no chevron-down icon: kept widgets's (icon.ChevronDown)") {
		t.Errorf("vuka ui add doesn't note the role the set hasn't:\n%s", out)
	}
	dialog := read(t, filepath.Join(dir, "components/ui/dialog/dialog.templ"))
	for _, want := range []string{"@icons.Close(icon.Props{Class: \"close\"})", "@icon.ChevronDown()", "\t\"example.com/shop/components/icons\"\n", "\t\"example.com/shop/components/ui/icon\"\n"} {
		if !strings.Contains(dialog, want) {
			t.Errorf("dialog.templ hasn't %q:\n%s", want, dialog)
		}
	}
	button := read(t, filepath.Join(dir, "components/ui/button/button.templ"))
	if !strings.Contains(button, "@icons.Close()") || strings.Contains(button, "components/ui/icon\"") || !strings.Contains(button, "import \"example.com/shop/components/icons\"\n") {
		t.Errorf("button.templ:\n%s", button)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "components/ui/icon/icon.go")), `var X = Icon("x")`) {
		t.Errorf("the library's icon package isn't the library's")
	}
	if out := mustUI(t, dir, "diff", "widgets/dialog", "widgets/button"); !strings.Contains(out, "widgets/dialog, widgets/button: the same as their libraries'") {
		t.Errorf("vuka ui diff:\n%s", out)
	}

	if _, err := runUI(t, typo, "add", "widgets/dialog"); err == nil || !strings.Contains(err.Error(), `[ui] icons = "ap": no [ui.icons.ap] maps its icons (did you mean app?) (the sets are app)`) {
		t.Errorf("icons = \"ap\": %v", err)
	}
	if out := mustUI(t, none, "add", "widgets/button"); strings.Contains(out, "icon set") || strings.Contains(out, "components/icons") {
		t.Errorf("no set:\n%s", out)
	}
}

func TestUIIconsConfig(t *testing.T) {
	dir := widgetsApp(t)
	writeFile(t, filepath.Join(dir, "vuka.toml"), "[ui.icons.app]\nclose = \"close\"\n")
	if _, err := runUI(t, dir, "add", "widgets/button"); err == nil || !strings.Contains(err.Error(), `[ui.icons.app] close = "close": the name of an exported function of the set's package (Close)`) {
		t.Errorf("a function's name in lower case: %v", err)
	}
}
