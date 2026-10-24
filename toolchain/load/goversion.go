package load

import (
	"bytes"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// vukaGo is the Go that built this vuka, whose standard library's importer
// reads the export data the go command writes for the module's imports.
var vukaGo = runtime.Version

// moduleGo is the Go that builds the module at dir: go env GOVERSION
// there, which follows its go.mod's go and toolchain lines and GOTOOLCHAIN.
var moduleGo = func(dir string) (string, error) {
	c := exec.Command("go", "env", "GOVERSION")
	c.Dir = dir
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOVERSION: %v\n%s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return strings.TrimSpace(string(out)), nil
}

var goVersions sync.Map // goVersionKey → goVersionResult

type goVersionResult struct {
	v   string
	err error
}

// goVersionKey is what the Go that builds the module at dir follows: the
// directory, its go.mod as it is now, and GOTOOLCHAIN.
func goVersionKey(dir string) string {
	key := dir + "\x00" + os.Getenv("GOTOOLCHAIN")
	if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		key += fmt.Sprintf("\x00%d %d", fi.Size(), fi.ModTime().UnixNano())
	}
	return key
}

// ModuleGoVersion is the Go that builds the module at dir (go1.27.1), asked
// of the go command once a process, and again when its go.mod changes.
func ModuleGoVersion(dir string) (string, error) {
	key := goVersionKey(dir)
	if r, ok := goVersions.Load(key); ok {
		return r.(goVersionResult).v, r.(goVersionResult).err
	}
	v, err := moduleGo(dir)
	goVersions.Store(key, goVersionResult{v, err})
	return v, err
}

// VukaGoVersion is the Go that built this vuka.
func VukaGoVersion() string { return vukaGo() }

// GoVersionError is a module built by a newer Go than vuka's: the go
// command writes export data in that Go's format, which the importer vuka
// was built with may not read. Go changes the format even in point
// releases (go1.27.2 writes a version go1.27.1 can't read).
type GoVersionError struct {
	Vuka   string // the Go that built vuka
	Module string // the Go that builds the module
	Path   string // the import whose export data couldn't be read; "" when found before reading any
	Detail string // what the importer said
}

func (e *GoVersionError) Error() string {
	var b strings.Builder
	if e.Path != "" {
		fmt.Fprintf(&b, "cannot read the export data of %s (%s): ", e.Path, e.Detail)
	}
	fmt.Fprintf(&b, "vuka was built with %s, but this module builds with %s (its go.mod's go or toolchain line, or GOTOOLCHAIN), whose export data an older Go may not read; reinstall vuka with that Go: %s",
		e.Vuka, e.Module, ReinstallCommand(e.Module))
	return b.String()
}

// ReinstallCommand is the command that installs this vuka's version, and
// vukavet's, with the Go goVersion.
func ReinstallCommand(goVersion string) string {
	v := installedVersion()
	cmds := []string{
		"go install github.com/vuka-lang/vuka/cmd/vuka/v2@" + v,
		"go install github.com/vuka-lang/vuka/cmd/vukavet/v2@" + v,
	}
	if runtime.GOOS == "windows" {
		return `$env:GOTOOLCHAIN="` + goVersion + `"; ` + strings.Join(cmds, "; ")
	}
	return "GOTOOLCHAIN=" + goVersion + " " + cmds[0] + " && GOTOOLCHAIN=" + goVersion + " " + cmds[1]
}

// installedVersion is the version this vuka was installed at, or latest
// when it was built from a checkout.
func installedVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Path != "github.com/vuka-lang/vuka/cmd/vuka/v2" {
		return "latest"
	}
	v := bi.Main.Version
	if v == "" || v == "(devel)" || strings.Contains(v, "+") {
		return "latest"
	}
	return v
}

// goRelease is the release of a Go version string: go1.27.1 of
// "go1.27.1 X:jsonv2"; "" for a development build.
func goRelease(v string) string {
	v, _, _ = strings.Cut(strings.TrimSpace(v), " ")
	if !version.IsValid(v) {
		return ""
	}
	return v
}

// CheckGoVersions is a *GoVersionError when the module's Go is newer than
// vuka's; nil when it isn't or either is unknown (a development build).
func CheckGoVersions(vuka, module string) error {
	v, m := goRelease(vuka), goRelease(module)
	if v == "" || m == "" || version.Compare(m, v) <= 0 {
		return nil
	}
	return &GoVersionError{Vuka: vuka, Module: module}
}

// CheckGoVersion is CheckGoVersions for this vuka and the module at dir;
// nil when the go command can't say (its own error shows when it runs).
func CheckGoVersion(dir string) error {
	m, err := ModuleGoVersion(dir)
	if err != nil {
		return nil
	}
	return CheckGoVersions(vukaGo(), m)
}
