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
		writeFile(t, filepath.Join(base, m, "go.mod"), "module github.com/vuka-lang/"+m+"/v2\n\ngo 1.23\n")
		writeFile(t, filepath.Join(base, m, m+".go"), "package "+m+"\n\nfunc Name() string { return \""+m+"\" }\n")
	}
	app := filepath.Join(base, "app")
	writeFile(t, filepath.Join(app, "go.mod"), "module example.com/shop\n\ngo 1.25.0\n\nrequire (\n"+
		"\tgithub.com/vuka-lang/ui/v2 "+uiV+"\n\tgithub.com/vuka-lang/vuka/v2 "+vukaV+"\n\tgithub.com/vuka-lang/web/v2 "+webV+"\n)\n\n"+
		"replace github.com/vuka-lang/vuka/v2 => "+repo+"\n\nreplace github.com/vuka-lang/ui/v2 => ../ui\n\nreplace github.com/vuka-lang/web/v2 => ../web\n")
	writeFile(t, filepath.Join(app, "main.go"), "package main\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/vuka-lang/ui/v2\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc main() { fmt.Println(ui.Name(), web.Name(), Add(1, 2)) }\n")
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
	app := setApp(t, "v2.0.1", "v2.0.0", "v2.0.2", nil)
	before := readTest(t, filepath.Join(app, "go.mod"))
	var out, errb bytes.Buffer
	if err := upgrade([]string{"-n", "v2.1.0"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	for _, want := range []string{
		"upgrade example.com/shop to the v2.1.0 set",
		"vuka  v2.0.1 → v2.1.0",
		"ui    v2.0.0 → v2.1.0",
		"web   v2.0.2 → v2.1.0",
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
		{"v2.0.0", []string{"before"}, "the app has it"},
		{"v2.1.0", []string{"between"}, "names change"},
		{"v2.2.0", []string{"after"}, "a later release"},
	}

	app := setApp(t, "v2.0.1", "v2.0.0", "v2.0.2", nil)
	var out, errb bytes.Buffer
	if err := upgrade([]string{"v2.1.0"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	mod := readTest(t, filepath.Join(app, "go.mod"))
	for _, m := range []string{"vuka", "ui", "web"} {
		if !strings.Contains(mod, "github.com/vuka-lang/"+m+"/v2 v2.1.0") {
			t.Errorf("go.mod doesn't require %s v2.1.0:\n%s", m, mod)
		}
	}
	if !strings.Contains(mod, "replace github.com/vuka-lang/ui/v2 => ../ui") {
		t.Errorf("go.mod lost its replaces:\n%s", mod)
	}
	if !ran["between"] || ran["before"] || ran["after"] {
		t.Errorf("fixers run %v, want only between", ran)
	}
	if !strings.Contains(out.String(), "v2.1.0: vuka fix between — names change") {
		t.Errorf("the plan doesn't name the step:\n%s", out.String())
	}
	for _, want := range []string{"is on the v2.1.0 set", "vet      ok", "test     ok", "backup   .vuka/upgrade (undo: mv .vuka/upgrade/go.mod go.mod"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if b := readTest(t, filepath.Join(app, ".vuka", "upgrade", "go.mod")); !strings.Contains(b, "github.com/vuka-lang/web/v2 v2.0.2") {
		t.Errorf("backup isn't the old go.mod:\n%s", b)
	}

	out.Reset()
	if err := upgrade([]string{"v2.1.0"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "on the v2.1.0 set already") {
		t.Errorf("a second upgrade did something:\n%s", out.String())
	}
}

// The orm moves with the command's own set to the version new apps use.
func TestUpgradeORM(t *testing.T) {
	app := setApp(t, "v2.0.1", "v2.0.0", "v2.0.2", nil)
	orm := filepath.Join(filepath.Dir(app), "orm")
	writeFile(t, filepath.Join(orm, "go.mod"), "module github.com/vuka-lang/orm/v2\n\ngo 1.23\n")
	writeFile(t, filepath.Join(orm, "orm.go"), "package orm\n")
	mod := readTest(t, filepath.Join(app, "go.mod"))
	mod = strings.Replace(mod, "require (\n", "require (\n\tgithub.com/vuka-lang/orm/v2 v2.0.0-rc.1\n", 1) + "\nreplace github.com/vuka-lang/orm/v2 => ../orm\n"
	writeFile(t, filepath.Join(app, "go.mod"), mod)

	var out, errb bytes.Buffer
	other := firstSetVersion
	if other == version {
		other = "v2.2.0"
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
	if want := "orm   v2.0.0-rc.1 → " + ormVersion; !strings.Contains(out.String(), want) {
		t.Errorf("missing %q in:\n%s", want, out.String())
	}
	if mod := readTest(t, filepath.Join(app, "go.mod")); !strings.Contains(mod, "github.com/vuka-lang/orm/v2 "+ormVersion) {
		t.Errorf("go.mod doesn't require orm %s:\n%s", ormVersion, mod)
	}
}

func TestUpgradeFailingTests(t *testing.T) {
	setApp(t, "v2.0.1", "v2.0.0", "v2.0.2", map[string]string{
		"fail_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) { t.Fatal(\"red\") }\n",
	})
	var out, errb bytes.Buffer
	err := upgrade([]string{"v2.1.0"}, &out, &errb)
	if err == nil || !strings.Contains(out.String(), "test     failed") {
		t.Fatalf("err %v, want a failed test in the summary:\n%s", err, out.String())
	}
}

func TestUpgradeLatest(t *testing.T) {
	old := latestSet
	t.Cleanup(func() { latestSet = old })
	var asked []string
	latestSet = func(dir string, mods []string) (string, error) { asked = mods; return "v2.3.1", nil }
	setApp(t, "v2.1.0", "v2.1.0", "v2.1.0", nil)
	var out, errb bytes.Buffer
	if err := upgrade([]string{"-n"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 || !strings.Contains(out.String(), "to the v2.3.1 set") {
		t.Errorf("asked %v:\n%s", asked, out.String())
	}
}

func TestUpgradeRefuses(t *testing.T) {
	setApp(t, "v2.0.1", "v2.0.0", "v2.0.2", nil)
	for _, args := range [][]string{{"v0.45.0"}, {"latest"}, {"v2.1.0", "v2.2.0"}} {
		var out bytes.Buffer
		if err := upgrade(args, &out, &out); err == nil {
			t.Errorf("upgrade %v: no error", args)
		}
	}
}

func TestStepsBetween(t *testing.T) {
	old := upgradeSteps
	t.Cleanup(func() { upgradeSteps = old })
	upgradeSteps = []upgradeStep{{"v2.3.0", []string{"c"}, ""}, {"v2.1.0", []string{"a"}, ""}, {"v2.2.0", []string{"b"}, ""}}
	var got []string
	for _, s := range stepsBetween("v2.1.0", "v2.3.0") {
		got = append(got, s.version)
	}
	if strings.Join(got, " ") != "v2.2.0 v2.3.0" {
		t.Errorf("steps %v", got)
	}
}

func TestVersionCmd(t *testing.T) {
	setApp(t, "v2.1.0", "v2.0.9", "v2.1.0", nil)
	var out, errb bytes.Buffer
	if err := versionCmd(nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); !strings.HasPrefix(first, "vuka "+version+" (") {
		t.Errorf("first line %q", first)
	}
	if !strings.Contains(out.String(), "github.com/vuka-lang/ui/v2    v2.0.9 => ../ui") {
		t.Errorf("app versions missing:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "is not one vuka set (vuka v2.1.0, ui v2.0.9, web v2.1.0)") {
		t.Errorf("no mixed-set warning: %q", errb.String())
	}
}

func TestOneSet(t *testing.T) {
	for _, c := range []struct {
		vs   map[string]string
		want bool
	}{
		{map[string]string{vukaModPath: "v2.1.0", uiModPath: "v2.1.0", webModPath: "v2.1.0"}, true},
		{map[string]string{vukaModPath: "v2.2.2"}, true},
		{map[string]string{vukaModPath: "v2.1.0", webModPath: "v2.1.1"}, false},
		{map[string]string{vukaModPath: "v0.32.0", uiModPath: "v0.32.0"}, false},
		{map[string]string{}, false},
	} {
		if _, ok := oneSet(c.vs); ok != c.want {
			t.Errorf("oneSet(%v) = %v", c.vs, ok)
		}
	}
}
