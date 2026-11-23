package main

import (
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"golang.org/x/mod/modfile"
)

// The go shim: vuka run under the name go (vuka shim install links one on
// PATH). In a vuka app go build, test, vet and run are vuka's; everything
// else, and everything outside one, is the real go, untouched.

const (
	// shimOffEnv, "off", makes the shim run the real go for everything.
	// vuka sets it for every process it starts, so the go commands vuka
	// runs itself, reaching a shim on PATH, go straight to the real go.
	shimOffEnv = "VUKA_GO_SHIM"
	// shimDepthEnv counts the shims a go command has passed through: a
	// backstop for a PATH whose shims would hand it to each other.
	shimDepthEnv = "VUKA_GO_SHIM_DEPTH"
	// realGoEnv names the real go, instead of the next one on PATH.
	realGoEnv = "VUKA_REAL_GO"
	// shimDirEnv is the shim directory, instead of ~/.vuka/shims.
	shimDirEnv = "VUKA_SHIM_DIR"
	// maxShimDepth is how many shims a go command may pass through.
	maxShimDepth = 8
)

// invokedAsGo reports whether argv0 names the shim: go, or go.exe on
// Windows, where names are compared ignoring case.
func invokedAsGo(goos, argv0 string) bool {
	seps := "/"
	if goos == "windows" {
		seps = `/\`
	}
	base := argv0[strings.LastIndexAny(argv0, seps)+1:]
	if goos == "windows" {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	}
	return base == "go"
}

// shimRoutes reports whether go args, run in dir, is vuka's: build, test,
// vet or run in a module requiring vuka, with flags vuka passes on as
// they are. Anything else is the real go's.
func shimRoutes(args []string, dir string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "build", "test", "vet", "run":
	default:
		return false
	}
	for _, a := range args[1:] {
		if a == "-args" || a == "--" {
			break
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") {
			continue
		}
		switch name {
		// go's help, not vuka's; an overlay or a go.mod of the caller's
		// own, which vuka's would replace.
		case "h", "help", "overlay", "modfile":
			return false
		}
	}
	return isVukaApp(dir)
}

// isVukaApp reports whether dir is in a module whose go.mod requires vuka.
func isVukaApp(dir string) bool {
	root, _, err := load.ModuleRoot(dir)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	f, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		return false
	}
	for _, r := range f.Require {
		if isVukaModule(r.Mod.Path) {
			return true
		}
	}
	return false
}

// isVukaModule reports whether path is vuka's module, at any major version.
func isVukaModule(path string) bool {
	const vuka = "github.com/vuka-lang/vuka"
	if path == vuka {
		return true
	}
	rest, ok := strings.CutPrefix(path, vuka+"/v")
	if !ok || rest == "" {
		return false
	}
	_, err := strconv.Atoi(rest)
	return err == nil
}

// runGoShim is main for the shim: it returns to main, which runs vuka's
// command, only when args are vuka's; otherwise it runs the real go and
// exits with its status.
func runGoShim(args []string) {
	dir, _ := os.Getwd()
	if os.Getenv(shimOffEnv) != "off" && shimRoutes(args, dir) {
		// Plain go commands: a flag only vuka has is refused, not taken.
		if f := vukaOnlyFlag(args); f != "" {
			fmt.Fprintf(os.Stderr, "vuka: -%s is vuka %s's flag, not go's: run vuka %s -%s …\n", f, args[0], args[0], f)
			os.Exit(2)
		}
		// vuka's go commands go to the real go, past this shim.
		os.Setenv(shimOffEnv, "off")
		return
	}
	depth, _ := strconv.Atoi(os.Getenv(shimDepthEnv))
	if depth >= maxShimDepth {
		fmt.Fprintf(os.Stderr, "vuka: the go shim passed a go command through %d shims, a loop: set %s to the real go\n", depth, realGoEnv)
		os.Exit(1)
	}
	real, err := realGo(shimEnv(), runtime.GOOS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vuka:", err)
		os.Exit(1)
	}
	env := append(os.Environ(), shimDepthEnv+"="+strconv.Itoa(depth+1))
	os.Exit(execGo(real, args, env))
}

// shimLookup is what resolving the real go reads: the environment, the
// running binary and its build, and the shim directory.
type shimLookup struct {
	getenv   func(string) string
	self     string // the running executable
	selfPath string // its main package, from its build information
	dirs     []string
}

func shimEnv() shimLookup {
	l := shimLookup{getenv: os.Getenv}
	l.self, _ = os.Executable()
	if bi, ok := debug.ReadBuildInfo(); ok {
		l.selfPath = bi.Path
	}
	if d, err := shimDir(os.Getenv); err == nil {
		l.dirs = append(l.dirs, d)
	}
	if strings.ContainsAny(os.Args[0], `/\`) {
		l.dirs = append(l.dirs, filepath.Dir(os.Args[0]))
	}
	return l
}

// shimDir is $VUKA_SHIM_DIR, else ~/.vuka/shims.
func shimDir(getenv func(string) string) (string, error) {
	if d := getenv(shimDirEnv); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vuka", "shims"), nil
}

// realGo is $VUKA_REAL_GO, else the first go on PATH that isn't a shim:
// not in a shim directory, not the running binary (through any link), and
// no vuka (a copy of one).
func realGo(l shimLookup, goos string) (string, error) {
	if p := l.getenv(realGoEnv); p != "" {
		if l.isShim(p) {
			return "", fmt.Errorf("%s=%s is the go shim, not a go", realGoEnv, p)
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return "", fmt.Errorf("%s=%s: no such go", realGoEnv, p)
		}
		return p, nil
	}
	for _, dir := range splitPathList(l.getenv("PATH"), goos) {
		if !filepath.IsAbs(dir) || l.isShimDir(dir) {
			continue
		}
		for _, name := range goNames(goos, l.getenv("PATHEXT")) {
			p := filepath.Join(dir, name)
			fi, err := os.Stat(p)
			if err != nil || fi.IsDir() || (goos != "windows" && fi.Mode()&0o111 == 0) {
				continue
			}
			if l.isShim(p) {
				continue
			}
			return p, nil
		}
	}
	return "", fmt.Errorf("the go shim finds no go on PATH besides itself: install Go, or set %s to the go to run", realGoEnv)
}

func (l shimLookup) isShimDir(dir string) bool {
	for _, d := range l.dirs {
		if sameInode(d, dir) {
			return true
		}
	}
	return false
}

// isShim reports whether the executable at p is this vuka or another one.
func (l shimLookup) isShim(p string) bool {
	if l.self != "" && sameInode(p, l.self) {
		return true
	}
	return isVukaBinary(p, l.selfPath)
}

// isVukaBinary reports whether p is a Go binary of vuka's command, or of
// main package selfPath (the running one: a test binary in the tests).
func isVukaBinary(p, selfPath string) bool {
	bi, err := buildinfo.ReadFile(p)
	if err != nil {
		return false
	}
	return bi.Path == vukaMainPath || (selfPath != "" && bi.Path == selfPath)
}

const vukaMainPath = "github.com/vuka-lang/vuka/cmd/vuka/v2"

// sameInode reports whether a and b, through any links, are one file.
func sameInode(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// splitPathList splits a PATH value as goos does: ; on Windows, where an
// entry may be quoted, : elsewhere.
func splitPathList(path, goos string) []string {
	if path == "" {
		return nil
	}
	sep := ":"
	if goos == "windows" {
		sep = ";"
	}
	var dirs []string
	for _, d := range strings.Split(path, sep) {
		if goos == "windows" {
			d = strings.ReplaceAll(d, `"`, "")
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// goNames is the file names a go command has in a PATH directory: go, or
// on Windows go with each of PATHEXT's extensions (.exe first by default).
func goNames(goos, pathext string) []string {
	if goos != "windows" {
		return []string{"go"}
	}
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	var names []string
	for _, ext := range strings.Split(strings.ToLower(pathext), ";") {
		if ext != "" && strings.HasPrefix(ext, ".") {
			names = append(names, "go"+ext)
		}
	}
	return names
}

// goCommandPath is the go command vuka's go commands reach: $VUKA_REAL_GO
// when set (vuka sets it when a go shim is first on PATH), else the go on
// PATH. Cache keys stat it, so that they follow the Go, not the shim.
func goCommandPath() (string, error) {
	if p := os.Getenv(realGoEnv); p != "" {
		return p, nil
	}
	return exec.LookPath("go")
}

// shimGuard keeps the go commands vuka runs, and the processes they start,
// off its go shim: the shim passes them to the real go (shimOffEnv), and
// when the go on PATH is a shim, VUKA_REAL_GO names the real one, for the
// cache keys and so that a shim reached doesn't look it up again.
func shimGuard() {
	os.Setenv(shimOffEnv, "off")
	if os.Getenv(realGoEnv) != "" {
		return
	}
	p, err := exec.LookPath("go")
	if err != nil {
		return
	}
	l := shimEnv()
	if !l.isShim(p) {
		return
	}
	if real, err := realGo(l, runtime.GOOS); err == nil {
		os.Setenv(realGoEnv, real)
	}
}

// vukaOnlyFlag is the first flag in go args that vuka's command has and
// go's hasn't (run -watch, build -islands, -debug…), "" for none. For go
// run, the flags end at the package: what follows is the program's.
func vukaOnlyFlag(args []string) string {
	for _, a := range args[1:] {
		if a == "--" || a == "-args" || !strings.HasPrefix(a, "-") {
			if args[0] == "run" || a == "--" || a == "-args" {
				break
			}
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch name {
		case "watch", "islands", "debug", "fresh", "keep-cache", "embed", "diag":
			return name
		}
	}
	return ""
}
