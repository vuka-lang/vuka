package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
)

// An upgradeStep is what moving an app past a release takes beyond go.mod:
// the vuka fix rewrites for that release's breaking changes. A breaking
// change adds its fixer to fixers (fix.go) and a step here naming it.
type upgradeStep struct {
	version string
	fixers  []string
	note    string
}

var upgradeSteps = []upgradeStep{
	{"v1.0.0", []string{"attributes", "hooks", "ui-structs", "queries"}, "struct tags and loaders found by name become attributes; the ui kit's components are struct literals; queries are the model's Manager's"},
}

// upgradeAlways are the fixers every upgrade runs.
var upgradeAlways = []string{"runtime", "db-schema", "templates-package"}

// stepsBetween are the steps of releases after from, up to and including to.
func stepsBetween(from, to string) []upgradeStep {
	var out []upgradeStep
	for _, s := range upgradeSteps {
		if compareVersions(s.version, from) > 0 && compareVersions(s.version, to) <= 0 {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b upgradeStep) int { return compareVersions(a.version, b.version) })
	return out
}

var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// upgradeEnv is the go command's environment for fetching a set: fresh tags
// straight from their repositories, no workspace.
func upgradeEnv() []string {
	nosum := "github.com/vuka-lang"
	if v := os.Getenv("GONOSUMDB"); v != "" {
		nosum = v + "," + nosum
	}
	return append(os.Environ(), "GOPROXY=direct", "GONOSUMDB="+nosum, "GOWORK=off", "GOFLAGS=-mod=mod")
}

// latestSet is the newest version every one of mods is released at.
var latestSet = func(dir string, mods []string) (string, error) {
	var common []string
	for i, p := range mods {
		c := exec.Command("go", "list", "-m", "-versions", "-json", p)
		c.Dir, c.Env = dir, upgradeEnv()
		var stderr bytes.Buffer
		c.Stderr = &stderr
		out, err := c.Output()
		if err != nil {
			return "", fmt.Errorf("listing %s's versions: %v\n%s", p, err, stderr.Bytes())
		}
		var m struct{ Versions []string }
		if err := json.Unmarshal(out, &m); err != nil {
			return "", err
		}
		var vs []string
		for _, v := range m.Versions {
			if releaseVersion.MatchString(v) && compareVersions(v, firstSetVersion) >= 0 && (i == 0 || slices.Contains(common, v)) {
				vs = append(vs, v)
			}
		}
		common = vs
	}
	if len(common) == 0 {
		return "", errors.New("no release of the set found (a replaced module lists none); name the version: vuka upgrade " + firstSetVersion)
	}
	slices.SortFunc(common, compareVersions)
	return common[len(common)-1], nil
}

func upgrade(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dry := fs.Bool("n", false, "only say what would be done")
	fs.BoolVar(dry, "dry-run", false, "same as -n")
	noTest := fs.Bool("no-test", false, "don't run vuka test after the upgrade")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: vuka upgrade [-n] [-no-test] [version]\n\nmoves the app's vuka, ui and web to one set (the latest by default) and,\nto this command's set, its orm to the version new apps use; runs the\nvuka fix rewrites for the releases in between, then vuka vet and\nvuka test.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return errors.New("vuka upgrade takes one version")
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	if modPath == cmdModPath || slices.Contains(setModules, modPath) {
		return fmt.Errorf("%s is part of the toolchain set; vuka release moves it", modPath)
	}
	m, err := readGoMod(root)
	if err != nil {
		return err
	}
	have := m.setVersions()
	var mods []string
	for _, p := range setModules {
		if _, ok := have[p]; ok {
			mods = append(mods, p)
		}
	}
	if len(mods) == 0 {
		return fmt.Errorf("%s requires none of %s", modPath, strings.Join(setModules, ", "))
	}
	target := fs.Arg(0)
	if target == "" {
		if target, err = latestSet(root, mods); err != nil {
			return err
		}
	}
	if !releaseVersion.MatchString(target) {
		return fmt.Errorf("%q is not a release version (vX.Y.Z)", target)
	}
	if compareVersions(target, firstSetVersion) < 0 {
		return fmt.Errorf("%s predates set versions; the first set is %s", target, firstSetVersion)
	}
	from := ""
	for _, p := range mods {
		if from == "" || compareVersions(have[p], from) < 0 {
			from = have[p]
		}
	}
	// The orm moves with the command's own set to the version its new apps
	// use, when the app requires an older one.
	ormHave, ormTo := m.requires(ormModule), ""
	if ormHave != "" && target == version && compareVersions(ormHave, ormVersion) < 0 {
		ormTo = ormVersion
	}
	if v, ok := oneSet(have); ok && v == target && ormTo == "" {
		fmt.Fprintf(stdout, "%s is on the %s set already\n", modPath, target)
		return nil
	}
	steps := stepsBetween(from, target)

	fmt.Fprintf(stdout, "upgrade %s to the %s set\n", modPath, target)
	for _, p := range mods {
		note := ""
		if compareVersions(target, have[p]) < 0 {
			note = " (downgrade)"
		}
		fmt.Fprintf(stdout, "  %-5s %s → %s%s\n", shortModule(p), have[p], target, note)
	}
	if ormTo != "" {
		fmt.Fprintf(stdout, "  %-5s %s → %s\n", "orm", ormHave, ormTo)
	}
	fixNames := slices.Clone(upgradeAlways)
	if len(steps) == 0 {
		fmt.Fprintln(stdout, "  no rewrites registered between these versions")
	}
	for _, s := range steps {
		fmt.Fprintf(stdout, "  %s: vuka fix %s — %s\n", s.version, strings.Join(s.fixers, " "), s.note)
		for _, f := range s.fixers {
			if !slices.Contains(fixNames, f) {
				fixNames = append(fixNames, f)
			}
		}
	}
	then := "vuka vet ./..., vuka test ./..."
	if *noTest {
		then = "vuka vet ./..."
	}
	fmt.Fprintln(stdout, "  then", then)
	if *dry {
		fmt.Fprintln(stdout, "dry run: nothing changed")
		return nil
	}

	backup, err := backupGoMod(root)
	if err != nil {
		return err
	}
	get := []string{"get"}
	for _, p := range mods {
		get = append(get, p+"@"+target)
	}
	if ormTo != "" {
		get = append(get, ormModule+"@"+ormTo)
	}
	c := exec.Command("go", get...)
	c.Dir, c.Env, c.Stdout, c.Stderr = root, upgradeEnv(), stdout, stderr
	if err := c.Run(); err != nil {
		if rerr := backup.restore(); rerr != nil {
			return fmt.Errorf("go get: %v; restoring go.mod: %v", err, rerr)
		}
		return fmt.Errorf("go get: %v (go.mod and go.sum restored)", err)
	}

	selected := map[string]bool{}
	for _, f := range fixNames {
		selected[f] = true
	}
	var fixed int
	var fixErr error
	if fixed, fixErr = applyFixers(selected, false, stdout); fixErr != nil {
		fmt.Fprintln(stderr, "vuka fix:", fixErr)
	}
	vetErr := runGo("vet", []string{"./..."}, stdout, stderr)
	var testErr error
	if !*noTest {
		testErr = runGo("test", []string{"./..."}, stdout, stderr)
	}

	status := func(err error) string {
		if err != nil {
			return "failed"
		}
		return "ok"
	}
	fmt.Fprintf(stdout, "\n%s is on the %s set\n", modPath, target)
	fmt.Fprintf(stdout, "  go.mod   %s\n", strings.Join(mods, ", "))
	fmt.Fprintf(stdout, "  fixes    %d change(s), %s\n", fixed, status(fixErr))
	fmt.Fprintf(stdout, "  vet      %s\n", status(vetErr))
	if *noTest {
		fmt.Fprintln(stdout, "  test     skipped")
	} else {
		fmt.Fprintf(stdout, "  test     %s\n", status(testErr))
	}
	fmt.Fprintf(stdout, "  backup   %s (undo: mv %s go.mod; mv %s go.sum)\n", backup.rel(""), backup.rel("go.mod"), backup.rel("go.sum"))
	if err := errors.Join(fixErr, vetErr, testErr); err != nil {
		return errors.New("vuka upgrade: the app needs attention (see above)")
	}
	return nil
}

// A modBackup holds go.mod and go.sum as they were before an upgrade.
type modBackup struct {
	root, dir string
	files     map[string]bool // name → it existed
}

func backupGoMod(root string) (*modBackup, error) {
	dir := filepath.Join(root, ".vuka", "upgrade")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	b := &modBackup{root: root, dir: dir, files: map[string]bool{}}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return nil, err
		}
		b.files[name] = true
	}
	return b, nil
}

func (b *modBackup) rel(name string) string {
	r, err := filepath.Rel(b.root, filepath.Join(b.dir, name))
	if err != nil {
		return filepath.Join(b.dir, name)
	}
	return r
}

func (b *modBackup) restore() error {
	for _, name := range []string{"go.mod", "go.sum"} {
		if !b.files[name] {
			os.Remove(filepath.Join(b.root, name))
			continue
		}
		data, err := os.ReadFile(filepath.Join(b.dir, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(b.root, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
