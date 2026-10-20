package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/internal/load"
)

// A releaseRepo is one repository of the set and the module it releases.
type releaseRepo struct {
	name, dir, module string
	tagPrefix         string // "", "cmd/vuka/" or "cmd/vukavet/"
	modDir            string // the module's directory in dir
	released          bool   // its tag is there already (a resumed release)
}

func (r *releaseRepo) tag(v string) string { return r.tagPrefix + v }

type releaseStep struct {
	what string
	run  func() error
}

// vukavetModPath is the analyzer, released with the command: tagged on the
// command's release commit, it requires none of the set.
const vukavetModPath = "github.com/vuka-lang/vuka/cmd/vukavet"

// release releases vuka, ui, web, cmd/vuka and cmd/vukavet under one
// version: checks, then tags in dependency order, each module requiring the
// set's others.
func release(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "do it (without, only check and print the plan)")
	noTest := fs.Bool("no-test", false, "skip go vet and go test in each module")
	security := fs.Bool("security", false, "a security fix: may follow the last patch release within the week")
	uiDir := fs.String("ui", "", "the ui checkout (default: vuka-ui beside the vuka checkout)")
	webDir := fs.String("web", "", "the web checkout (default: vuka-web beside the vuka checkout)")
	remote := fs.String("remote", "origin", "the remote to push to")
	branch := fs.String("branch", "main", "the branch releases are cut from")
	proxyWait := fs.Duration("proxy-wait", 10*time.Minute, "how long to wait for proxy.golang.org to serve the set (0: don't ask it)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: vuka release [-yes] [-no-test] [-security] [flags] vX.Y.Z\n\nrun in a vuka checkout; ui and web are its siblings vuka-ui and vuka-web.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("vuka release takes the version")
	}
	v := fs.Arg(0)
	if !releaseVersion.MatchString(v) {
		return fmt.Errorf("%q is not a release version (vX.Y.Z)", v)
	}
	if compareVersions(v, firstSetVersion) < 0 {
		return fmt.Errorf("set versions start at %s", firstSetVersion)
	}
	if major := strings.SplitN(v[1:], ".", 2)[0]; major != "0" && major != "1" {
		return fmt.Errorf("%s needs /v%s module paths; the set's modules have none", v, major)
	}

	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	if modPath == cmdModPath {
		root = filepath.Dir(filepath.Dir(root))
	} else if modPath != vukaModPath {
		return fmt.Errorf("run vuka release in a vuka checkout (this is %s)", modPath)
	}
	if *uiDir == "" {
		*uiDir = filepath.Join(filepath.Dir(root), "vuka-ui")
	}
	if *webDir == "" {
		*webDir = filepath.Join(filepath.Dir(root), "vuka-web")
	}
	vuka := &releaseRepo{name: "vuka", dir: root, module: vukaModPath, modDir: root}
	ui := &releaseRepo{name: "ui", dir: *uiDir, module: uiModPath, modDir: *uiDir}
	web := &releaseRepo{name: "web", dir: *webDir, module: webModPath, modDir: *webDir}
	cmd := &releaseRepo{name: "cmd/vuka", dir: root, module: cmdModPath, tagPrefix: "cmd/vuka/", modDir: filepath.Join(root, "cmd", "vuka")}
	vet := &releaseRepo{name: "cmd/vukavet", dir: root, module: vukavetModPath, tagPrefix: "cmd/vukavet/", modDir: filepath.Join(root, "cmd", "vukavet")}
	repos := []*releaseRepo{vuka, ui, web, cmd, vet}

	fmt.Fprintf(stdout, "release the %s set\n\nchecks:\n", v)
	var problems []string
	check := func(what string, err error) {
		if err != nil {
			problems = append(problems, what+": "+err.Error())
			fmt.Fprintf(stdout, "  FAIL %s: %v\n", what, err)
			return
		}
		fmt.Fprintf(stdout, "  ok   %s\n", what)
	}
	var latest string // the newest tag of any of the set's modules
	for _, r := range repos {
		check(r.name+" is "+r.module, checkModule(r))
		if r.dir != root || r == vuka {
			check(r.name+" is clean, on "+*branch+", level with "+*remote, checkRepoState(r.dir, *remote, *branch))
		}
		have, err := releaseTags(r.dir, *remote, r.tagPrefix)
		if err != nil {
			check(r.name+" tags", err)
			continue
		}
		for _, t := range have {
			if t == v {
				continue
			}
			if latest == "" || compareVersions(t, latest) > 0 {
				latest = t
			}
		}
		if slicesContains(have, v) {
			err := run(r.dir, nil, "git", "merge-base", "--is-ancestor", r.tag(v), "HEAD")
			if err != nil {
				err = fmt.Errorf("%s exists and isn't on %s", r.tag(v), *branch)
			}
			r.released = err == nil
			check(r.name+" "+r.tag(v)+" released already (resuming)", err)
		}
	}
	if latest != "" {
		var err error
		if compareVersions(v, latest) <= 0 {
			err = fmt.Errorf("%s is not after %s", v, latest)
		}
		check(v+" follows every tag of the set ("+latest+")", err)
	}
	check("cmd/vuka/toolchain.go has the version constants", checkToolchain(filepath.Join(cmd.modDir, "toolchain.go")))
	notes, err := checkReleaseNotes(filepath.Join(root, "docs", "releases.md"), v)
	if err == nil && notes != "" && *yes {
		err = errors.New(notes)
	} else if notes != "" {
		fmt.Fprintf(stdout, "  warn %s\n", notes)
	}
	check("docs/releases.md has a section for "+v, err)
	check("the patch train", checkTrain(root, *remote, v, *security))
	checkouts := map[string]string{vukaModPath: vuka.dir, uiModPath: ui.dir, webModPath: web.dir}
	if !*noTest {
		env := []string{"GOWORK=off", "VUKA_UI=" + ui.dir, "VUKA_WEB=" + web.dir}
		for _, r := range repos {
			check(r.name+": go vet, go test", vetTestModule(r.modDir, setReplaces(r, checkouts), env))
		}
	}

	steps := releaseSteps(v, vuka, ui, web, cmd, vet, *remote, *branch, *proxyWait, stdout)
	fmt.Fprintln(stdout, "\nplan:")
	for i, s := range steps {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, s.what)
	}
	if len(problems) > 0 {
		return fmt.Errorf("vuka release: %d check(s) failed", len(problems))
	}
	if !*yes {
		fmt.Fprintln(stdout, "\ndry run: nothing changed; vuka release -yes "+v+" does it")
		return nil
	}
	fmt.Fprintln(stdout)
	for i, s := range steps {
		fmt.Fprintf(stdout, "%d. %s\n", i+1, s.what)
		if err := s.run(); err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, s.what, err)
		}
	}
	fmt.Fprintf(stdout, "\nreleased the %s set\n", v)
	return nil
}

func releaseSteps(v string, vuka, ui, web, cmd, vet *releaseRepo, remote, branch string, proxyWait time.Duration, stdout io.Writer) []releaseStep {
	var steps []releaseStep
	tagPush := func(r *releaseRepo) {
		steps = append(steps,
			releaseStep{"tag " + r.name + " " + r.tag(v), func() error {
				return run(r.dir, nil, "git", "tag", "-a", r.tag(v), "-m", r.name+" "+v)
			}},
			releaseStep{"push " + r.name + " " + branch + " and " + r.tag(v), func() error {
				return run(r.dir, nil, "git", "push", remote, "HEAD:refs/heads/"+branch, "refs/tags/"+r.tag(v))
			}})
	}
	bump := func(r *releaseRepo, mods []string, edit func() error) {
		var gets []string
		for _, m := range mods {
			gets = append(gets, m+"@"+v)
		}
		what := r.name + ": go get " + strings.Join(gets, " ")
		if edit != nil {
			what += "; the toolchain constants to " + v
		}
		steps = append(steps,
			releaseStep{what + "; go build; commit", func() error {
				if edit != nil {
					if err := edit(); err != nil {
						return err
					}
				}
				if err := run(r.modDir, releaseGoEnv(), "go", append([]string{"get"}, gets...)...); err != nil {
					return err
				}
				if err := run(r.modDir, releaseGoEnv(), "go", "build", "./..."); err != nil {
					return err
				}
				if err := run(r.dir, nil, "git", "add", "-A", "."); err != nil {
					return err
				}
				var names []string
				for _, m := range mods {
					names = append(names, shortModule(m))
				}
				msg := fmt.Sprintf("%s %s: require %s %s", r.name, v, strings.Join(names, ", "), v)
				return run(r.dir, nil, "git", "commit", "-q", "-m", msg)
			}})
	}
	if !vuka.released {
		tagPush(vuka)
	}
	if !ui.released {
		bump(ui, []string{vukaModPath}, nil)
		tagPush(ui)
	}
	if !web.released {
		bump(web, []string{vukaModPath, uiModPath}, nil)
		tagPush(web)
	}
	if !cmd.released {
		bump(cmd, cmdRequires(cmd.modDir), func() error { return setToolchain(filepath.Join(cmd.modDir, "toolchain.go"), v) })
		tagPush(cmd)
	}
	if !vet.released {
		steps = append(steps,
			releaseStep{"tag " + vet.name + " " + vet.tag(v) + " on " + cmd.tag(v), func() error {
				return run(vet.dir, nil, "git", "tag", "-a", vet.tag(v), "-m", vet.name+" "+v, cmd.tag(v)+"^{commit}")
			}},
			releaseStep{"push " + vet.tag(v), func() error {
				return run(vet.dir, nil, "git", "push", remote, "refs/tags/"+vet.tag(v))
			}})
	}
	if proxyWait > 0 {
		steps = append(steps, releaseStep{"wait (up to " + proxyWait.String() + ") for proxy.golang.org to serve the set", func() error {
			for _, m := range []string{vukaModPath, uiModPath, webModPath, cmdModPath, vukavetModPath} {
				if err := waitProxy(m, v, proxyWait); err != nil {
					fmt.Fprintf(stdout, "   warning: %v (GOPROXY=direct installs it meanwhile)\n", err)
				}
			}
			return nil
		}})
	}
	steps = append(steps, releaseStep{"verify: GOPROXY=direct go install " + cmdModPath + "@" + v + " and " + vukavetModPath + "@" + v + ", vuka version", func() error {
		return verifyInstall(v)
	}})
	return steps
}

// releaseGoEnv fetches fresh tags straight from their repositories.
func releaseGoEnv() []string {
	return []string{"GOPROXY=direct", "GOSUMDB=off", "GOWORK=off", "GOFLAGS=-mod=mod"}
}

func run(dir string, env []string, name string, args ...string) error {
	_, err := output(dir, env, name, args...)
	return err
}

func output(dir string, env []string, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func checkModule(r *releaseRepo) error {
	_, p, err := load.ModuleRoot(r.modDir)
	if err != nil {
		return err
	}
	if p != r.module {
		return fmt.Errorf("%s is module %s", r.modDir, p)
	}
	return nil
}

func checkRepoState(dir, remote, branch string) error {
	b, err := output(dir, nil, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	if b = strings.TrimSpace(b); b != branch {
		return fmt.Errorf("on %s, not %s", b, branch)
	}
	st, err := output(dir, nil, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if st = strings.TrimSpace(st); st != "" {
		return fmt.Errorf("uncommitted changes:\n%s", st)
	}
	if err := run(dir, nil, "git", "fetch", "-q", "--tags", remote, branch); err != nil {
		return err
	}
	head, err := output(dir, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	theirs, err := output(dir, nil, "git", "rev-parse", remote+"/"+branch)
	if err != nil {
		return err
	}
	if head != theirs {
		return fmt.Errorf("HEAD %.12s, %s/%s %.12s", head, remote, branch, theirs)
	}
	return nil
}

// releaseTags are the release versions tagged with prefix, here or on remote.
func releaseTags(dir, remote, prefix string) ([]string, error) {
	local, err := output(dir, nil, "git", "tag", "-l", prefix+"v*")
	if err != nil {
		return nil, err
	}
	names := strings.Fields(local)
	if out, err := output(dir, nil, "git", "ls-remote", "--tags", remote); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) == 2 {
				names = append(names, strings.TrimSuffix(strings.TrimPrefix(f[1], "refs/tags/"), "^{}"))
			}
		}
	}
	var vs []string
	for _, n := range names {
		v, ok := strings.CutPrefix(n, prefix)
		if ok && releaseVersion.MatchString(v) && !slicesContains(vs, v) {
			vs = append(vs, v)
		}
	}
	return vs, nil
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// checkReleaseNotes finds v's section in releases.md; notes is a warning
// when the section is still marked unreleased.
func checkReleaseNotes(path, v string) (notes string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	head := regexp.MustCompile(`(?m)^##\s.*\b` + regexp.QuoteMeta(v) + `\b.*$`).Find(data)
	if head == nil {
		return "", fmt.Errorf("no \"## %s — <date>\" heading", v)
	}
	if bytes.Contains(bytes.ToLower(head), []byte("unreleased")) {
		return fmt.Sprintf("the %s section is marked unreleased; give it the date", v), nil
	}
	return "", nil
}

// checkTrain refuses a patch release within a week of the set's last one,
// unless it is a security fix.
func checkTrain(root, remote, v string, security bool) error {
	if strings.HasSuffix(v, ".0") {
		return nil
	}
	tags, err := releaseTags(root, remote, "")
	if err != nil {
		return err
	}
	prev := ""
	for _, t := range tags {
		if compareVersions(t, firstSetVersion) >= 0 && compareVersions(t, v) < 0 && (prev == "" || compareVersions(t, prev) > 0) {
			prev = t
		}
	}
	if prev == "" {
		return nil
	}
	out, err := output(root, nil, "git", "log", "-1", "--format=%ct", prev)
	if err != nil {
		return nil // a tag only on the remote: nothing to date it by
	}
	sec, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	age := time.Since(time.Unix(sec, 0))
	if age < 7*24*time.Hour && !security {
		return fmt.Errorf("%s was released %s ago; patches ship weekly (hold the fix for the next train, or pass -security)", prev, age.Round(time.Hour))
	}
	return nil
}

// cmdRequires are the set's libraries the command requires: vuka always,
// ui and web when its go.mod names them.
func cmdRequires(dir string) []string {
	mods := []string{vukaModPath}
	m, err := readGoMod(dir)
	if err != nil {
		return mods
	}
	have := m.setVersions()
	for _, p := range setModules[1:] {
		if _, ok := have[p]; ok {
			mods = append(mods, p)
		}
	}
	return mods
}

// setReplaces points the set's modules r requires at the checkouts being
// released, so its tests run against what the release tags rather than
// against the versions its go.mod requires today.
func setReplaces(r *releaseRepo, checkouts map[string]string) map[string]string {
	m, err := readGoMod(r.modDir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for p := range m.setVersions() {
		if dir, ok := checkouts[p]; ok && p != r.module {
			out[p] = dir
		}
	}
	return out
}

// vetTestModule runs go vet and go test in dir, with replace directives for
// the run (go.mod and go.sum are put back after).
func vetTestModule(dir string, replace map[string]string, env []string) (err error) {
	if len(replace) > 0 {
		saved := map[string][]byte{}
		for _, name := range []string{"go.mod", "go.sum"} {
			if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				saved[name] = b
			}
		}
		defer func() {
			for name, b := range saved {
				if werr := os.WriteFile(filepath.Join(dir, name), b, 0o644); werr != nil && err == nil {
					err = werr
				}
			}
		}()
		args := []string{"mod", "edit"}
		for _, p := range setModules {
			if d, ok := replace[p]; ok {
				args = append(args, "-replace", p+"="+d)
			}
		}
		if err := run(dir, env, "go", args...); err != nil {
			return err
		}
	}
	if err := run(dir, env, "go", "vet", "./..."); err != nil {
		return err
	}
	return run(dir, env, "go", "test", "./...")
}

var toolchainConst = regexp.MustCompile(`(?m)^(\s*(?:version|vukaVersion|uiVersion|webVersion)\s*=\s*)"[^"]*"`)

func checkToolchain(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if n := len(toolchainConst.FindAll(src, -1)); n != 4 {
		return fmt.Errorf("%s: found %d of the 4 version constants", path, n)
	}
	return nil
}

// setToolchain writes v as every version constant of toolchain.go.
func setToolchain(path, v string) error {
	if err := checkToolchain(path); err != nil {
		return err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, toolchainConst.ReplaceAll(src, []byte(`${1}"`+v+`"`)), 0o644)
}

var proxyURL = "https://proxy.golang.org"

func waitProxy(module, v string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	url := proxyURL + "/" + module + "/@v/" + v + ".info"
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("proxy.golang.org doesn't serve %s@%s yet", module, v)
		}
		time.Sleep(15 * time.Second)
	}
}

// verifyInstall installs the released command straight from its repository
// and checks it reports the set.
func verifyInstall(v string) error {
	bin, err := os.MkdirTemp("", "vuka-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(bin)
	for _, m := range []string{cmdModPath, vukavetModPath} {
		if err := run(bin, append(releaseGoEnv(), "GOBIN="+bin, "GOFLAGS="), "go", "install", m+"@"+v); err != nil {
			return err
		}
	}
	out, err := output(bin, nil, filepath.Join(bin, "vuka"), "version")
	if err != nil {
		return err
	}
	want := fmt.Sprintf("vuka %s (vuka, ui, web %s)", v, v)
	if first, _, _ := strings.Cut(out, "\n"); first != want {
		return fmt.Errorf("vuka version says %q, want %q", first, want)
	}
	return nil
}
