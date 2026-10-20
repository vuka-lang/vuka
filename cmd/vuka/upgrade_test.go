package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setApp writes an app requiring vuka, ui and web at these versions, each
// replaced by a local directory: vuka by this repository, ui and web by
// stand-ins, so go get resolves any version offline.
func setApp(t *testing.T, vukaV, uiV, webV string, files map[string]string) string {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	repo, _ := filepath.Abs("../..")
	for _, m := range []string{"ui", "web"} {
		writeFile(t, filepath.Join(base, m, "go.mod"), "module github.com/vuka-lang/"+m+"\n\ngo 1.23\n")
		writeFile(t, filepath.Join(base, m, m+".go"), "package "+m+"\n\nfunc Name() string { return \""+m+"\" }\n")
	}
	app := filepath.Join(base, "app")
	writeFile(t, filepath.Join(app, "go.mod"), "module example.com/shop\n\ngo 1.25.0\n\nrequire (\n"+
		"\tgithub.com/vuka-lang/ui "+uiV+"\n\tgithub.com/vuka-lang/vuka "+vukaV+"\n\tgithub.com/vuka-lang/web "+webV+"\n)\n\n"+
		"replace github.com/vuka-lang/vuka => "+repo+"\n\nreplace github.com/vuka-lang/ui => ../ui\n\nreplace github.com/vuka-lang/web => ../web\n")
	writeFile(t, filepath.Join(app, "main.go"), "package main\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/vuka-lang/ui\"\n\t\"github.com/vuka-lang/web\"\n)\n\nfunc main() { fmt.Println(ui.Name(), web.Name(), Add(1, 2)) }\n")
	writeFile(t, filepath.Join(app, "add.vuka"), "package main\n\nfunc Add(a, b int) int { return a + b }\n")
	writeFile(t, filepath.Join(app, "add_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"add\")\n\t}\n}\n")
	for name, src := range files {
		writeFile(t, filepath.Join(app, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(app)
	return app
}

func readTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgradeDryRun(t *testing.T) {
	app := setApp(t, "v0.32.0", "v0.30.0", "v0.36.3", nil)
	before := readTest(t, filepath.Join(app, "go.mod"))
	var out, errb bytes.Buffer
	if err := upgrade([]string{"-n", "v0.50.0"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	for _, want := range []string{
		"upgrade example.com/shop to the v0.50.0 set",
		"vuka  v0.32.0 → v0.50.0",
		"ui    v0.30.0 → v0.50.0",
		"web   v0.36.3 → v0.50.0",
		"then vuka vet ./..., vuka test ./...",
		"dry run: nothing changed",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if after := readTest(t, filepath.Join(app, "go.mod")); after != before {
		t.Errorf("a dry run changed go.mod:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(app, ".vuka", "upgrade")); err == nil {
		t.Error("a dry run backed go.mod up")
	}
}

func TestUpgrade(t *testing.T) {
	ran := map[string]bool{}
	mk := func(name string) fixer {
		return fixer{name, "test", func(m *fixModule) ([]fixChange, error) {
			return []fixChange{{"rewrite " + name, func() error { ran[name] = true; return nil }}}, nil
		}}
	}
	oldFixers, oldSteps := fixers, upgradeSteps
	t.Cleanup(func() { fixers, upgradeSteps = oldFixers, oldSteps })
	fixers = append(append([]fixer{}, fixers...), mk("before"), mk("between"), mk("after"))
	upgradeSteps = []upgradeStep{
		{"v0.30.0", []string{"before"}, "the app has it"},
		{"v0.50.0", []string{"between"}, "names change"},
		{"v0.51.0", []string{"after"}, "a later release"},
	}

	app := setApp(t, "v0.32.0", "v0.30.0", "v0.36.3", nil)
	var out, errb bytes.Buffer
	if err := upgrade([]string{"v0.50.0"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	mod := readTest(t, filepath.Join(app, "go.mod"))
	for _, m := range []string{"vuka", "ui", "web"} {
		if !strings.Contains(mod, "github.com/vuka-lang/"+m+" v0.50.0") {
			t.Errorf("go.mod doesn't require %s v0.50.0:\n%s", m, mod)
		}
	}
	if !strings.Contains(mod, "replace github.com/vuka-lang/ui => ../ui") {
		t.Errorf("go.mod lost its replaces:\n%s", mod)
	}
	if !ran["between"] || ran["before"] || ran["after"] {
		t.Errorf("fixers run %v, want only between", ran)
	}
	if !strings.Contains(out.String(), "v0.50.0: vuka fix between — names change") {
		t.Errorf("the plan doesn't name the step:\n%s", out.String())
	}
	for _, want := range []string{"is on the v0.50.0 set", "vet      ok", "test     ok", "backup   .vuka/upgrade (undo: mv .vuka/upgrade/go.mod go.mod"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if b := readTest(t, filepath.Join(app, ".vuka", "upgrade", "go.mod")); !strings.Contains(b, "github.com/vuka-lang/web v0.36.3") {
		t.Errorf("backup isn't the old go.mod:\n%s", b)
	}

	out.Reset()
	if err := upgrade([]string{"v0.50.0"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "on the v0.50.0 set already") {
		t.Errorf("a second upgrade did something:\n%s", out.String())
	}
}

// The orm moves with the command's own set to the version new apps use.
func TestUpgradeORM(t *testing.T) {
	app := setApp(t, "v0.32.0", "v0.30.0", "v0.36.3", nil)
	orm := filepath.Join(filepath.Dir(app), "orm")
	writeFile(t, filepath.Join(orm, "go.mod"), "module github.com/vuka-lang/orm\n\ngo 1.23\n")
	writeFile(t, filepath.Join(orm, "orm.go"), "package orm\n")
	mod := readTest(t, filepath.Join(app, "go.mod"))
	mod = strings.Replace(mod, "require (\n", "require (\n\tgithub.com/vuka-lang/orm v0.9.1\n", 1) + "\nreplace github.com/vuka-lang/orm => ../orm\n"
	writeFile(t, filepath.Join(app, "go.mod"), mod)

	var out, errb bytes.Buffer
	other := firstSetVersion
	if other == version {
		other = "v0.51.0"
	}
	if err := upgrade([]string{"-n", other}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	if strings.Contains(out.String(), "orm ") {
		t.Errorf("an upgrade to another set than the command's moves the orm:\n%s", out.String())
	}
	out.Reset()
	if err := upgrade([]string{version}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	if want := "orm   v0.9.1 → " + ormVersion; !strings.Contains(out.String(), want) {
		t.Errorf("missing %q in:\n%s", want, out.String())
	}
	if mod := readTest(t, filepath.Join(app, "go.mod")); !strings.Contains(mod, "github.com/vuka-lang/orm "+ormVersion) {
		t.Errorf("go.mod doesn't require orm %s:\n%s", ormVersion, mod)
	}
}

func TestUpgradeFailingTests(t *testing.T) {
	setApp(t, "v0.32.0", "v0.30.0", "v0.36.3", map[string]string{
		"fail_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) { t.Fatal(\"red\") }\n",
	})
	var out, errb bytes.Buffer
	err := upgrade([]string{"v0.50.0"}, &out, &errb)
	if err == nil || !strings.Contains(out.String(), "test     failed") {
		t.Fatalf("err %v, want a failed test in the summary:\n%s", err, out.String())
	}
}

func TestUpgradeLatest(t *testing.T) {
	old := latestSet
	t.Cleanup(func() { latestSet = old })
	var asked []string
	latestSet = func(dir string, mods []string) (string, error) { asked = mods; return "v0.52.1", nil }
	setApp(t, "v0.50.0", "v0.50.0", "v0.50.0", nil)
	var out, errb bytes.Buffer
	if err := upgrade([]string{"-n"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 || !strings.Contains(out.String(), "to the v0.52.1 set") {
		t.Errorf("asked %v:\n%s", asked, out.String())
	}
}

func TestUpgradeRefuses(t *testing.T) {
	setApp(t, "v0.32.0", "v0.30.0", "v0.36.3", nil)
	for _, args := range [][]string{{"v0.45.0"}, {"latest"}, {"v0.50.0", "v0.51.0"}} {
		var out bytes.Buffer
		if err := upgrade(args, &out, &out); err == nil {
			t.Errorf("upgrade %v: no error", args)
		}
	}
}

func TestStepsBetween(t *testing.T) {
	old := upgradeSteps
	t.Cleanup(func() { upgradeSteps = old })
	upgradeSteps = []upgradeStep{{"v0.52.0", []string{"c"}, ""}, {"v0.50.0", []string{"a"}, ""}, {"v0.51.0", []string{"b"}, ""}}
	var got []string
	for _, s := range stepsBetween("v0.50.0", "v0.52.0") {
		got = append(got, s.version)
	}
	if strings.Join(got, " ") != "v0.51.0 v0.52.0" {
		t.Errorf("steps %v", got)
	}
}

func TestVersionCmd(t *testing.T) {
	setApp(t, "v0.50.0", "v0.49.0", "v0.50.0", nil)
	var out, errb bytes.Buffer
	if err := versionCmd(nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); !strings.HasPrefix(first, "vuka "+version+" (") {
		t.Errorf("first line %q", first)
	}
	if !strings.Contains(out.String(), "github.com/vuka-lang/ui    v0.49.0 => ../ui") {
		t.Errorf("app versions missing:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "is not one vuka set (vuka v0.50.0, ui v0.49.0, web v0.50.0)") {
		t.Errorf("no mixed-set warning: %q", errb.String())
	}
}

func TestOneSet(t *testing.T) {
	for _, c := range []struct {
		vs   map[string]string
		want bool
	}{
		{map[string]string{vukaModPath: "v0.50.0", uiModPath: "v0.50.0", webModPath: "v0.50.0"}, true},
		{map[string]string{vukaModPath: "v0.51.2"}, true},
		{map[string]string{vukaModPath: "v0.50.0", webModPath: "v0.50.1"}, false},
		{map[string]string{vukaModPath: "v0.32.0", uiModPath: "v0.32.0"}, false},
		{map[string]string{}, false},
	} {
		if _, ok := oneSet(c.vs); ok != c.want {
			t.Errorf("oneSet(%v) = %v", c.vs, ok)
		}
	}
}
