package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"

	"golang.org/x/mod/modfile"
	gomodule "golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// linkedVuka is the version of the vuka module (the compiler) this command
// links, or "" when it is a local copy, whose version says nothing.
var linkedVuka = func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return depVersion(bi, vukaModPath)
}

// depVersion is the version bi links module p at, or "" when it is a local
// copy (a workspace or a directory replace).
func depVersion(bi *debug.BuildInfo, p string) string {
	if bi.Main.Path == p {
		return validVersion(bi.Main.Version)
	}
	for _, d := range bi.Deps {
		if d.Path != p {
			continue
		}
		if d.Replace != nil {
			return validVersion(d.Replace.Version)
		}
		return validVersion(d.Version)
	}
	return ""
}

func validVersion(v string) string {
	if semver.IsValid(v) {
		return v
	}
	return ""
}

// appVukaRequirement is the vuka the module at root requires, after a
// replace by another version; "" when it requires none or replaces it with
// a directory.
func appVukaRequirement(root string) (modPath, v string) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", ""
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil || f.Module == nil {
		return "", ""
	}
	for _, r := range f.Require {
		if r.Mod.Path == vukaModPath {
			v = r.Mod.Version
		}
	}
	for _, r := range f.Replace {
		if r.Old.Path == vukaModPath && (r.Old.Version == "" || r.Old.Version == v) {
			v = r.New.Version
		}
	}
	return f.Module.Mod.Path, validVersion(v)
}

// compilerAge is the warning for an app at root requiring a newer vuka
// than the one this command compiles with, else "".
func compilerAge(root, linked string) string {
	modPath, want := appVukaRequirement(root)
	if linked == "" || want == "" || modPath == vukaModPath || semver.Compare(linked, want) >= 0 {
		return ""
	}
	self := want
	if gomodule.IsPseudoVersion(want) {
		self = mainTarget
	}
	return fmt.Sprintf("warning: this vuka command compiles with vuka %s, older than the %s %s requires;\n"+
		"         its fixes since aren't applied: vuka upgrade -self %s (or go tool vuka, which builds against the app's go.mod)\n",
		commit(linked), commit(want), modPath, self)
}

var compilerAgeOnce sync.Once

// warnCompilerAge prints, once a run, the warning for the app in the
// current directory when this command compiles with an older vuka.
func warnCompilerAge(stderr io.Writer) {
	compilerAgeOnce.Do(func() {
		root, _, err := load.ModuleRoot(".")
		if err != nil {
			return
		}
		fmt.Fprint(stderr, compilerAge(root, linkedVuka()))
	})
}
