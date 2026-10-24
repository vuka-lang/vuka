package main

import (
	"bytes"
	"debug/buildinfo"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vukavetEnv names the vukavet vuka vet runs; off runs go vet's own
// analyzers only.
const vukavetEnv = "VUKA_VUKAVET"

// vukaExecutable is this vuka's path, where a vukavet beside it is looked
// for (tests point it elsewhere).
var vukaExecutable = os.Executable

// installVukavet builds vukavet at version v into dir.
var installVukavet = func(dir, v string) ([]byte, error) {
	c := exec.Command("go", "install", vukavetModPath+"@"+v)
	c.Dir = dir
	c.Env = append(withoutModFlag(os.Environ()), "GOBIN="+dir, "GOWORK=off")
	return c.CombinedOutput()
}

// hasVettool reports whether the go flags name a -vettool of their own.
func hasVettool(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if strings.HasPrefix(a, "-") && name == "vettool" {
			return true
		}
	}
	return false
}

// vetTool is the vukavet vuka vet hands go vet as -vettool, so its checks
// run beside vet's own: VUKA_VUKAVET's; the app's go tool vukavet when
// its go.mod declares the tool; one beside this vuka or on PATH of this
// vuka's version; else one built at this vuka's version into the user's
// cache. "" with a note on stderr when there is none (VUKA_VUKAVET=off
// says nothing): go vet runs its own analyzers then.
func vetTool(root string, stderr io.Writer) (string, error) {
	switch env := os.Getenv(vukavetEnv); env {
	case "off":
		return "", nil
	case "":
	default:
		if fi, err := os.Stat(env); err != nil || fi.IsDir() {
			return "", fmt.Errorf("%s=%s: no such program", vukavetEnv, env)
		}
		return env, nil
	}
	if p, ok, err := toolVukavet(root); ok {
		if err != nil {
			return "", fmt.Errorf("vuka vet: go.mod declares the vukavet tool, but go tool -n %s fails: %v", vukavetModPath, err)
		}
		return p, nil
	}
	var stale []string
	var candidates []string
	if exe, err := vukaExecutable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "vukavet"+exeSuffix()))
	}
	if p, err := exec.LookPath("vukavet"); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		switch v, ok := vukavetVersion(p); {
		case ok:
			return p, nil
		case v != "":
			stale = append(stale, p+" is "+v)
		}
	}
	dir, ok := load.CacheDir()
	if !ok {
		d, err := os.UserCacheDir()
		if err != nil {
			return "", noVukavet(stderr, stale, "no user cache directory to build it into: "+err.Error())
		}
		dir = filepath.Join(d, "vuka")
	}
	dir = filepath.Join(dir, "vukavet", version)
	built := filepath.Join(dir, "vukavet"+exeSuffix())
	if _, err := os.Stat(built); err == nil {
		return built, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", noVukavet(stderr, stale, err.Error())
	}
	fmt.Fprintf(stderr, "vuka vet: building vukavet %s (once, into %s)\n", version, dir)
	if out, err := installVukavet(dir, version); err != nil {
		return "", noVukavet(stderr, stale, fmt.Sprintf("go install %s@%s: %v\n%s", vukavetModPath, version, err, bytes.TrimSpace(out)))
	}
	return built, nil
}

// noVukavet says vuka vet runs without vukavet, and why.
func noVukavet(stderr io.Writer, stale []string, why string) error {
	fmt.Fprintf(stderr, "warning: vuka vet runs go vet's own analyzers only, without vukavet's checks: %s\n", why)
	for _, s := range stale {
		fmt.Fprintf(stderr, "\t(%s, not %s)\n", s, version)
	}
	fmt.Fprintf(stderr, "\tdeclare it in the app: go get -tool %s@%s; or name one: %s=/path/to/vukavet or vuka vet -vettool=…\n", vukavetModPath, version, vukavetEnv)
	return nil
}

// toolVukavet is the vukavet the app at root declares as a tool (go get
// -tool), built by go tool -n; ok is false when it declares none.
func toolVukavet(root string) (path string, ok bool, err error) {
	m, err := readGoMod(root)
	if err != nil {
		return "", false, nil
	}
	tool := ""
	for _, t := range m.Tool {
		if t.Path == vukavetModPath || strings.HasPrefix(t.Path, "github.com/vuka-lang/vuka/cmd/vukavet") {
			tool = t.Path
		}
	}
	if tool == "" {
		return "", false, nil
	}
	c := exec.Command("go", "tool", "-n", tool)
	c.Dir = root
	var errOut bytes.Buffer
	c.Stderr = &errOut
	out, err := c.Output()
	if err != nil {
		return "", true, fmt.Errorf("%v\n%s", err, bytes.TrimSpace(errOut.Bytes()))
	}
	return strings.TrimSpace(string(out)), true, nil
}

// vukavetVersion is the version of the vukavet at p and whether it is
// this vuka's: built at the same version, or from a checkout ("(devel)"),
// or any when this vuka was itself built from a checkout. "" when p isn't
// a vukavet.
func vukavetVersion(p string) (string, bool) {
	bi, err := buildinfo.ReadFile(p)
	if err != nil || bi.Main.Path != vukavetModPath {
		return "", false
	}
	v := bi.Main.Version
	return v, v == version || v == "(devel)" || v == "" || selfDevel()
}

// selfDevel reports whether this vuka was built from a checkout.
func selfDevel() bool {
	bi, ok := debug.ReadBuildInfo()
	return ok && (bi.Main.Version == "(devel)" || bi.Main.Version == "")
}

// withoutModFlag is env with -mod=… taken out of GOFLAGS: go install
// pkg@version refuses it.
func withoutModFlag(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GOFLAGS="); ok {
			var keep []string
			for _, f := range strings.Fields(v) {
				if !strings.HasPrefix(f, "-mod=") {
					keep = append(keep, f)
				}
			}
			kv = "GOFLAGS=" + strings.Join(keep, " ")
		}
		out = append(out, kv)
	}
	return out
}
