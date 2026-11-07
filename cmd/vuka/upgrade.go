package main

import (
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"

	gomodule "golang.org/x/mod/module"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
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
	{"v2.16.0", []string{"explicit-live"}, "a struct live by a hook or an event embeds ui.Live; it no longer builds otherwise (E0169)"},
	{"v2.14.0", []string{"graphql-params", "graphql-args"}, "a GraphQL argument's attributes go before its parameter; an argument of a value type is required (Int!): the list says which to make pointers"},
	{"v2.10.0", []string{"orm-fields"}, "ORM lookup roots are XColumns, relation hops are methods, and the model-term interface is ModelAnnotation[T]"},
	{"v1.0.0", []string{"attributes", "hooks", "ui-structs", "queries"}, "struct tags and loaders found by name become attributes; the ui kit's components are struct literals; queries are the model's Manager's"},
}

// upgradeAlways are the fixers every upgrade runs.
var upgradeAlways = []string{"runtime", "db-schema", "templates-package"}

// stepsBetween are the steps of releases after from, up to and including to;
// to main, every one after from, a release main has a step for not yet
// tagged included.
func stepsBetween(from, to string) []upgradeStep {
	var out []upgradeStep
	for _, s := range upgradeSteps {
		if compareVersions(s.version, from) > 0 && (to == mainTarget || compareVersions(s.version, to) <= 0) {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b upgradeStep) int { return compareVersions(a.version, b.version) })
	return out
}

var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// directEnv fetches vuka's modules straight from their repositories, so a
// tag or a commit pushed minutes ago resolves.
func directEnv() []string {
	nosum := "github.com/vuka-lang"
	if v := os.Getenv("GONOSUMDB"); v != "" {
		nosum = v + "," + nosum
	}
	return append(os.Environ(), "GOPROXY=direct", "GONOSUMDB="+nosum)
}

// upgradeEnv is the go command's environment for fetching a set: fresh tags
// straight from their repositories, no workspace.
func upgradeEnv() []string {
	return append(directEnv(), "GOWORK=off", "GOFLAGS=-mod=mod")
}

const mainTarget = "main"

// mainSet is each of mods at its main branch's current commit, as a
// pseudo-version.
var mainSet = func(dir string, mods []string) (map[string]string, error) {
	args := []string{"list", "-m", "-json"}
	for _, p := range mods {
		args = append(args, p+"@"+mainTarget)
	}
	c := exec.Command("go", args...)
	c.Dir, c.Env = dir, upgradeEnv()
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("resolving main: %v\n%s", err, stderr.Bytes())
	}
	vs := map[string]string{}
	for d := json.NewDecoder(bytes.NewReader(out)); d.More(); {
		var m struct{ Path, Version string }
		if err := d.Decode(&m); err != nil {
			return nil, err
		}
		vs[m.Path] = m.Version
	}
	for _, p := range mods {
		if vs[p] == "" {
			return nil, fmt.Errorf("resolving main: no version of %s", p)
		}
	}
	return vs, nil
}

// unreleased reports whether a build's main-module version is not a
// release: go run …@main stamps a pseudo-version, a checkout's build a
// pseudo-version or (devel).
func unreleased(v string) bool {
	return v == "" || v == "(devel)" || gomodule.IsPseudoVersion(v)
}

// commandFromMain reports whether this command is built from unreleased
// source, and so carries main's upgrade steps.
var commandFromMain = func() bool {
	bi, ok := debug.ReadBuildInfo()
	return ok && unreleased(commandVersion(bi))
}

// commandVersion is the version of cmd/vuka bi was built from: the main
// module's, or the dependency's when built in another module, as runMain
// and -self main build it.
func commandVersion(bi *debug.BuildInfo) string {
	if bi.Main.Path != cmdModPath {
		for _, d := range bi.Deps {
			if d.Path == cmdModPath {
				return d.Version
			}
		}
	}
	return bi.Main.Version
}

// upgradeMainEnv marks the command vuka upgrade main runs from main, so
// that one never runs another.
const upgradeMainEnv = "VUKA_UPGRADE_MAIN"

// mainModuleCmds are the go commands that, in the empty directory dir, make
// a module requiring cmd/vuka and vuka at their main branches, so that main's
// command builds against main's vuka rather than the vuka its go.mod pins,
// then run go with last (build -o …, install) on the command.
func mainModuleCmds(dir string, last ...string) []*exec.Cmd {
	get := exec.Command("go", "get", cmdModPath+"@"+mainTarget, vukaModPath+"@"+mainTarget)
	build := exec.Command("go", append(last, cmdModPath)...)
	for _, c := range []*exec.Cmd{get, build} {
		c.Dir, c.Env = dir, upgradeEnv()
	}
	return []*exec.Cmd{get, build}
}

// writeMainModule writes the go.mod mainModuleCmds start from.
func writeMainModule(dir string) error {
	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module vuka-main\n"), 0o644)
}

// goCmd runs a go command vuka upgrade -self or main needs; tests replace it.
var goCmd = func(c *exec.Cmd, stdout, stderr io.Writer) error {
	c.Stdout, c.Stderr = stdout, stderr
	return c.Run()
}

// runQuiet runs c with goCmd, its output shown only when it fails.
func runQuiet(c *exec.Cmd) error {
	var out bytes.Buffer
	if err := goCmd(c, &out, &out); err != nil {
		return fmt.Errorf("%s: %v\n%s", strings.Join(c.Args, " "), err, out.Bytes())
	}
	return nil
}

// mainCommand is vuka upgrade args run by exe, main's command, in dir.
func mainCommand(dir, exe string, args []string) *exec.Cmd {
	c := exec.Command(exe, append([]string{"upgrade"}, args...)...)
	c.Dir, c.Env = dir, append(directEnv(), upgradeMainEnv+"=1")
	return c
}

// runMain runs vuka upgrade args with main's command, built against main's
// vuka, its output and status being this one's.
var runMain = func(dir string, args []string, stdout, stderr io.Writer) error {
	tmp, err := os.MkdirTemp("", "vuka-main-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := writeMainModule(tmp); err != nil {
		return err
	}
	bin := exe(filepath.Join(tmp, "vuka"))
	for _, c := range mainModuleCmds(tmp, "build", "-o", bin) {
		if err := runQuiet(c); err != nil {
			return fmt.Errorf("building main's command: %v", err)
		}
	}
	c := mainCommand(dir, bin, args)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, stdout, stderr
	return c.Run()
}

// selfCmds are the go commands that install the command at target into
// GOBIN: a release's with go install, its pins being the release's; main's
// built against vuka's main in the temporary module at dir.
func selfCmds(dir, target string) []*exec.Cmd {
	if target == mainTarget {
		return mainModuleCmds(dir, "install")
	}
	c := exec.Command("go", "install", cmdModPath+"@"+target)
	c.Dir, c.Env = dir, append(directEnv(), "GOWORK=off")
	return []*exec.Cmd{c}
}

// installedPath is where go install puts the command: GOBIN, else the
// first GOPATH's bin.
func installedPath() (string, error) {
	out, err := exec.Command("go", "env", "GOBIN", "GOPATH").Output()
	if err != nil {
		return "", fmt.Errorf("go env: %v", err)
	}
	gobin, gopath, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if gobin = strings.TrimSpace(gobin); gobin == "" {
		gobin = filepath.Join(filepath.SplitList(strings.TrimSpace(gopath))[0], "bin")
	}
	return exe(filepath.Join(gobin, "vuka")), nil
}

// installedInfo is the build information of the binary at path.
var installedInfo = buildinfo.ReadFile

// upgradeSelf installs the command at target (the newest release when "")
// and says what it installed and the vuka it compiles with.
func upgradeSelf(target string, dry bool, stdout, stderr io.Writer) error {
	tmp, err := os.MkdirTemp("", "vuka-self-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := writeMainModule(tmp); err != nil {
		return err
	}
	if target == "" {
		if target, err = latestSet(tmp, []string{cmdModPath}); err != nil {
			return err
		}
	}
	if target != mainTarget && !releaseVersion.MatchString(target) {
		return fmt.Errorf("%q is not a release version (vX.Y.Z) or main", target)
	}
	path, err := installedPath()
	if err != nil {
		return err
	}
	cmds := selfCmds(tmp, target)
	what := "the " + target + " command"
	if target == mainTarget {
		what = "main's command, built against vuka's main"
	}
	if dry {
		fmt.Fprintf(stdout, "install %s into %s:\n", what, path)
		for _, c := range cmds {
			fmt.Fprintf(stdout, "  %s\n", strings.Join(c.Args, " "))
		}
		fmt.Fprintln(stdout, "dry run: nothing changed")
		return nil
	}
	fmt.Fprintf(stderr, "vuka upgrade -self: installing %s\n", what)
	for _, c := range cmds {
		if err := runQuiet(c); err != nil {
			return err
		}
	}
	bi, err := installedInfo(path)
	if err != nil {
		return fmt.Errorf("reading the installed command: %v", err)
	}
	linked := depVersion(bi, vukaModPath)
	if linked == "" {
		linked = "an unknown version"
	}
	fmt.Fprintf(stdout, "installed %s: cmd/vuka %s, compiling with vuka %s\n", path, commandVersion(bi), linked)
	return nil
}

// moduleLabel is how the plan names module p: vuka, ui, cmd/vuka, ….
func moduleLabel(p string) string {
	return strings.TrimPrefix(shortModule(p), "vuka/")
}

// commit is the revision a pseudo-version names, else the version.
func commit(v string) string {
	if rev, err := gomodule.PseudoVersionRev(v); err == nil {
		return rev
	}
	return v
}

// onMain reports whether the set's modules the app requires are where
// vuka upgrade main puts them: at unreleased commits, but for a module
// whose main is a release's commit, which resolves to the release.
func onMain(have map[string]string) bool {
	pseudo := false
	for _, v := range have {
		switch {
		case gomodule.IsPseudoVersion(v):
			pseudo = true
		case !releaseVersion.MatchString(v):
			return false
		}
	}
	return pseudo
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
	self := fs.Bool("self", false, "install the command (the newest release, a version or main) into GOBIN; no app changes")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: vuka upgrade [-n] [-no-test] [version | main]\n       vuka upgrade -self [-n] [version | main]\n\nmoves the app's vuka, ui and web to one set (the latest by default) and,\nto this command's set, its orm to the version new apps use; runs the\nvuka fix rewrites for the releases in between, then vuka vet and\nvuka test.\n\nmain moves vuka, ui, web and, when the app requires them, the orm and\nthe cmd/vuka and cmd/vukavet tools to their main branches' current\ncommits, and runs every rewrite after the app's version, main's own\nincluded; a command not built from main runs main's to do it\n(built against vuka's main in a temporary module).\n\n-self installs the command into GOBIN (or GOPATH/bin): a release's as\nreleased, main's built against vuka's main; it changes no app.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return errors.New("vuka upgrade takes one version")
	}
	if *self {
		return upgradeSelf(fs.Arg(0), *dry, stdout, stderr)
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
	toMain := target == mainTarget
	if toMain && !commandFromMain() {
		if os.Getenv(upgradeMainEnv) != "" {
			return errors.New("vuka upgrade main: the command run from main is not built from main")
		}
		fmt.Fprintln(stderr, "vuka upgrade main: running main's command, built against vuka's main")
		return runMain(".", args, stdout, stderr)
	}
	type move struct{ mod, from, to string }
	var moves []move
	if toMain {
		want := slices.Clone(mods)
		for _, p := range []string{ormModule, cmdModPath, vukavetModPath} {
			if m.requires(p) != "" {
				want = append(want, p)
			}
		}
		pins, err := mainSet(root, want)
		if err != nil {
			return err
		}
		for _, p := range want {
			moves = append(moves, move{p, m.requires(p), pins[p]})
		}
	} else {
		if target == "" {
			if target, err = latestSet(root, mods); err != nil {
				return err
			}
		}
		if !releaseVersion.MatchString(target) {
			return fmt.Errorf("%q is not a release version (vX.Y.Z) or main", target)
		}
		if compareVersions(target, firstSetVersion) < 0 {
			return fmt.Errorf("%s predates set versions; the first set is %s", target, firstSetVersion)
		}
		for _, p := range mods {
			moves = append(moves, move{p, have[p], target})
		}
		// The orm moves with the command's own set to the version its new
		// apps use, when the app requires an older one.
		if ormHave := m.requires(ormModule); ormHave != "" && target == version && compareVersions(ormHave, ormVersion) < 0 {
			moves = append(moves, move{ormModule, ormHave, ormVersion})
		}
	}
	setName := "the " + target + " set"
	if toMain {
		setName = mainTarget
	}
	if !slices.ContainsFunc(moves, func(mv move) bool { return mv.from != mv.to }) {
		fmt.Fprintf(stdout, "%s is on %s already\n", modPath, setName)
		return nil
	}
	from := ""
	for _, p := range mods {
		if from == "" || compareVersions(have[p], from) < 0 {
			from = have[p]
		}
	}
	steps := stepsBetween(from, target)

	fmt.Fprintf(stdout, "upgrade %s to %s\n", modPath, setName)
	width := 5
	for _, mv := range moves {
		width = max(width, len(moduleLabel(mv.mod)))
	}
	for _, mv := range moves {
		note := ""
		if compareVersions(mv.to, mv.from) < 0 {
			note = " (downgrade)"
		}
		fmt.Fprintf(stdout, "  %-*s %s → %s%s\n", width, moduleLabel(mv.mod), mv.from, mv.to, note)
	}
	if toMain {
		fmt.Fprintln(stdout, "  main's commits are not a set tested together")
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
	for _, mv := range moves {
		get = append(get, mv.mod+"@"+mv.to)
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
	fmt.Fprintf(stdout, "\n%s is on %s\n", modPath, setName)
	if toMain {
		var commits []string
		for _, mv := range moves {
			commits = append(commits, moduleLabel(mv.mod)+" "+commit(mv.to))
		}
		fmt.Fprintf(stdout, "  go.mod   %s\n", strings.Join(commits, ", "))
		fmt.Fprintln(stdout, "  note     main's commits are not a set tested together; vuka upgrade moves back to a release")
	} else {
		fmt.Fprintf(stdout, "  go.mod   %s\n", strings.Join(mods, ", "))
	}
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
	return filepath.ToSlash(r) // the hint reads alike on every system
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
