package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
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

// TestVersionCmdGo: vuka version names the Go that built it and the Go that
// builds the module, warning when the module's is newer.
func TestVersionCmdGo(t *testing.T) {
	setApp(t, version, version, version, nil)
	oldVuka, oldModule := vukaGoVersion, moduleGoVersion
	t.Cleanup(func() { vukaGoVersion, moduleGoVersion = oldVuka, oldModule })
	vukaGoVersion = func() string { return "go1.25.3" }
	for _, c := range []struct{ module, warning string }{
		{"go1.25.3", ""},
		{"go1.27.1", "vuka was built with go1.25.3, but this module builds with go1.27.1"},
	} {
		moduleGoVersion = func(string) (string, error) { return c.module, nil }
		var out, errb bytes.Buffer
		if err := versionCmd(nil, &out, &errb); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\nbuilt with go1.25.3\n") || !strings.Contains(out.String(), " builds with "+c.module+"\n") {
			t.Errorf("module %s:\n%s", c.module, out.String())
		}
		switch {
		case c.warning == "" && errb.Len() > 0:
			t.Errorf("module %s: warned %q", c.module, errb.String())
		case c.warning != "" && (!strings.Contains(errb.String(), c.warning) || !strings.Contains(errb.String(), "GOTOOLCHAIN=go1.27.1 go install github.com/vuka-lang/vuka/cmd/vuka/v2@")):
			t.Errorf("module %s: warning %q", c.module, errb.String())
		}
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

// Crossing v2.16.0 embeds ui.Live, which E0169 now requires.
func TestUpgradeExplicitLive(t *testing.T) {
	for _, s := range stepsBetween("v2.15.0", "v2.16.0") {
		if slices.Contains(s.fixers, "explicit-live") {
			return
		}
	}
	t.Error("no explicit-live step between v2.15.0 and v2.16.0")
}

const (
	vukaMain = "v2.17.1-0.20261107120000-0123456789ab"
	uiMain   = "v2.17.1-0.20261107110000-123456789abc"
	webMain  = "v2.17.1-0.20261107100000-23456789abcd"
	ormMain  = "v2.17.1-0.20261107090000-3456789abcde"
	cmdMain  = "v2.17.1-0.20261107120000-0123456789ab"
)

// fakeMain makes this command main's and its lookup of main answer the
// pins above, recording what it was asked.
func fakeMain(t *testing.T) *[]string {
	t.Helper()
	oldFrom, oldSet := commandFromMain, mainSet
	t.Cleanup(func() { commandFromMain, mainSet = oldFrom, oldSet })
	commandFromMain = func() bool { return true }
	asked := new([]string)
	pins := map[string]string{vukaModPath: vukaMain, uiModPath: uiMain, webModPath: webMain, ormModule: ormMain, cmdModPath: cmdMain, vukavetModPath: cmdMain}
	mainSet = func(dir string, mods []string) (map[string]string, error) {
		*asked = mods
		out := map[string]string{}
		for _, p := range mods {
			out[p] = pins[p]
		}
		return out, nil
	}
	return asked
}

// mainApp is a set app that also requires the orm, cmd/vuka and
// cmd/vukavet, each replaced by a stand-in (not declared tools, so vet
// doesn't run the stand-in vukavet).
func mainApp(t *testing.T) string {
	t.Helper()
	app := setApp(t, "v2.17.0", "v2.17.0", "v2.17.0", nil)
	base := filepath.Dir(app)
	mod := readTest(t, filepath.Join(app, "go.mod"))
	var req, rep string
	for _, s := range []struct{ path, dir string }{{ormModule, "orm"}, {cmdModPath, "cmdvuka"}, {vukavetModPath, "cmdvukavet"}} {
		writeFile(t, filepath.Join(base, s.dir, "go.mod"), "module "+s.path+"\n\ngo 1.23\n")
		writeFile(t, filepath.Join(base, s.dir, "doc.go"), "package main\n\nfunc main() {}\n")
		req += "\t" + s.path + " v2.17.0\n"
		rep += "\nreplace " + s.path + " => ../" + s.dir + "\n"
	}
	writeFile(t, filepath.Join(app, "go.mod"), strings.Replace(mod, "require (\n", "require (\n"+req, 1)+rep)
	return app
}

func TestUpgradeMain(t *testing.T) {
	asked := fakeMain(t)
	ran := map[string]bool{}
	mk := func(name string) fixer {
		return fixer{name, "test", func(m *fixModule) ([]fixChange, error) {
			return []fixChange{{"rewrite " + name, func() error { ran[name] = true; return nil }}}, nil
		}}
	}
	oldFixers, oldSteps := fixers, upgradeSteps
	t.Cleanup(func() { fixers, upgradeSteps = oldFixers, oldSteps })
	fixers = append(append([]fixer{}, fixers...), mk("released"), mk("unreleased"))
	upgradeSteps = []upgradeStep{
		{"v2.17.0", []string{"released"}, "the app has it"},
		{"v2.18.0", []string{"unreleased"}, "on main, not yet tagged"},
	}

	app := mainApp(t)
	var out, errb bytes.Buffer
	if err := upgrade([]string{"main"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	if want := []string{vukaModPath, uiModPath, webModPath, ormModule, cmdModPath, vukavetModPath}; !slices.Equal(*asked, want) {
		t.Errorf("resolved %v, want %v", *asked, want)
	}
	mod := readTest(t, filepath.Join(app, "go.mod"))
	for p, v := range map[string]string{vukaModPath: vukaMain, uiModPath: uiMain, webModPath: webMain, ormModule: ormMain, cmdModPath: cmdMain, vukavetModPath: cmdMain} {
		if !strings.Contains(mod, p+" "+v) {
			t.Errorf("go.mod doesn't require %s %s:\n%s", p, v, mod)
		}
	}
	if !ran["unreleased"] || ran["released"] {
		t.Errorf("fixers run %v, want only unreleased", ran)
	}
	for _, want := range []string{
		"upgrade example.com/shop to main",
		"cmd/vukavet v2.17.0 → " + cmdMain,
		"vuka        v2.17.0 → " + vukaMain,
		"v2.18.0: vuka fix unreleased — on main, not yet tagged",
		"example.com/shop is on main",
		"go.mod   vuka 0123456789ab, ui 123456789abc, web 23456789abcd, orm 3456789abcde, cmd/vuka 0123456789ab, cmd/vukavet 0123456789ab",
		"not a set tested together",
		"vet      ok", "test     ok",
		"backup   .vuka/upgrade",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if b := readTest(t, filepath.Join(app, ".vuka", "upgrade", "go.mod")); !strings.Contains(b, uiModPath+" v2.17.0") {
		t.Errorf("backup isn't the old go.mod:\n%s", b)
	}

	out.Reset()
	if err := upgrade([]string{"main"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "is on main already") {
		t.Errorf("a second upgrade did something:\n%s", out.String())
	}
	out.Reset()
	if err := versionCmd(nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "example.com/shop is on main (vuka 0123456789ab, ui 123456789abc, web 23456789abcd)") || strings.Contains(errb.String(), "warning") {
		t.Errorf("version after upgrade main:\n%s%s", out.String(), errb.String())
	}
}

func TestUpgradeMainDryRun(t *testing.T) {
	asked := fakeMain(t)
	app := setApp(t, "v2.17.0", "v2.17.0", "v2.17.0", nil)
	before := readTest(t, filepath.Join(app, "go.mod"))
	var out, errb bytes.Buffer
	if err := upgrade([]string{"-n", "-no-test", "main"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s%s", err, out.String(), errb.String())
	}
	if len(*asked) != 3 {
		t.Errorf("resolved %v, want vuka, ui and web only", *asked)
	}
	for _, want := range []string{"upgrade example.com/shop to main", "web   v2.17.0 → " + webMain, "not a set tested together", "then vuka vet ./...\n", "dry run: nothing changed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if after := readTest(t, filepath.Join(app, "go.mod")); after != before {
		t.Errorf("a dry run changed go.mod:\n%s", after)
	}
}

// A command not built from main runs main's, with the same arguments.
func TestUpgradeMainRunsMainsCommand(t *testing.T) {
	oldFrom, oldRun := commandFromMain, runMain
	t.Cleanup(func() { commandFromMain, runMain = oldFrom, oldRun })
	commandFromMain = func() bool { return false }
	var got []string
	runMain = func(dir string, args []string, stdout, stderr io.Writer) error {
		got = args
		return errors.New("main's status")
	}
	setApp(t, "v2.17.0", "v2.17.0", "v2.17.0", nil)
	var out bytes.Buffer
	if err := upgrade([]string{"-n", "main"}, &out, &out); err == nil || err.Error() != "main's status" {
		t.Errorf("err %v, want main's command's", err)
	}
	if !slices.Equal(got, []string{"-n", "main"}) {
		t.Errorf("ran main's command with %v", got)
	}

	t.Setenv(upgradeMainEnv, "1")
	got = nil
	if err := upgrade([]string{"main"}, &out, &out); err == nil || got != nil {
		t.Errorf("main's command ran another (err %v, args %v)", err, got)
	}

	c := mainCommand("/apps/shop", "/tmp/vuka-main/vuka", []string{"-no-test", "main"})
	if want := []string{"/tmp/vuka-main/vuka", "upgrade", "-no-test", "main"}; !slices.Equal(c.Args, want) || c.Dir != "/apps/shop" {
		t.Errorf("command %v in %q", c.Args, c.Dir)
	}
	for _, want := range []string{"GOPROXY=direct", upgradeMainEnv + "=1"} {
		if !slices.Contains(c.Env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
}

func TestUnreleased(t *testing.T) {
	for v, want := range map[string]bool{
		"(devel)": true,
		"":        true,
		vukaMain:  true,
		"v2.17.1-0.20261107120000-0123456789ab+dirty": true,
		"v2.17.0":       false,
		"v2.17.0+dirty": false,
		"v2.18.0-rc.1":  false,
	} {
		if got := unreleased(v); got != want {
			t.Errorf("unreleased(%q) = %v", v, got)
		}
	}
}

func TestStepsBetweenMain(t *testing.T) {
	old := upgradeSteps
	t.Cleanup(func() { upgradeSteps = old })
	upgradeSteps = []upgradeStep{{"v2.18.0", []string{"c"}, ""}, {"v2.16.0", []string{"a"}, ""}, {"v2.17.0", []string{"b"}, ""}}
	var got []string
	for _, s := range stepsBetween("v2.16.0", mainTarget) {
		got = append(got, s.version)
	}
	if strings.Join(got, " ") != "v2.17.0 v2.18.0" {
		t.Errorf("steps %v", got)
	}
	got = nil
	for _, s := range stepsBetween(vukaMain, mainTarget) {
		got = append(got, s.version)
	}
	if strings.Join(got, " ") != "v2.18.0" {
		t.Errorf("steps from main %v", got)
	}
}

// A module whose main is a release's commit resolves to the release.
func TestVersionCmdMain(t *testing.T) {
	setApp(t, vukaMain, uiMain, "v2.17.0", nil)
	var out, errb bytes.Buffer
	if err := versionCmd(nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "example.com/shop is on main (vuka 0123456789ab, ui 123456789abc, web v2.17.0), not a release") || errb.Len() > 0 {
		t.Errorf("version on main:\n%s%s", out.String(), errb.String())
	}
}
