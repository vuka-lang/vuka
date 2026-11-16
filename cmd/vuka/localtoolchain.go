package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"

	"golang.org/x/mod/modfile"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// compilerModules are the modules whose edits only reach an app through
// this command: the transpiler and the command itself. ui and web are left
// out — a local copy of those reaches the app through its own build.
var compilerModules = []string{vukaModPath, cmdModPath}

// localCompilerModules are the compiler's own modules the workspace (go.work
// use directives and directory replaces) or the module at root (directory
// replaces in its go.mod) points at local copies of.
func localCompilerModules(workFile, root string) []string {
	own := map[string]bool{}
	for _, m := range compilerModules {
		own[m] = true
	}
	var found []string
	seen := map[string]bool{}
	add := func(m string) {
		if own[m] && !seen[m] {
			seen[m] = true
			found = append(found, m)
		}
	}
	if workFile != "" {
		if data, err := os.ReadFile(workFile); err == nil {
			if w, err := modfile.ParseWork(workFile, data, nil); err == nil {
				for _, u := range w.Use {
					dir := u.Path
					if !filepath.IsAbs(dir) {
						dir = filepath.Join(filepath.Dir(workFile), dir)
					}
					add(moduleAt(dir))
				}
				for _, r := range w.Replace {
					if r.New.Version == "" {
						add(r.Old.Path)
					}
				}
			}
		}
	}
	if root != "" {
		if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
			if f, err := modfile.Parse("go.mod", data, nil); err == nil {
				for _, r := range f.Replace {
					if r.New.Version == "" {
						add(r.Old.Path)
					}
				}
			}
		}
	}
	return found
}

// moduleAt is the module path of the go.mod in dir, "" when there is none.
func moduleAt(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil || f.Module == nil {
		return ""
	}
	return f.Module.Mod.Path
}

// staleToolchain is the warning for running a command built with a published
// compiler module while the surrounding workspace or module points at a
// local copy of it, else "": edits to that copy aren't in this command.
func staleToolchain(bi *debug.BuildInfo, workFile, root string) string {
	if bi == nil {
		return ""
	}
	for _, m := range localCompilerModules(workFile, root) {
		v := depVersion(bi, m)
		if v == "" {
			continue
		}
		return fmt.Sprintf("warning: this vuka command was built with the published %s %s, not the local copy this workspace points at; its edits aren't in this run: go run %s from here (or rebuild the command from the local source)\n", m, v, cmdModPath)
	}
	return ""
}

var staleToolchainOnce sync.Once

// warnStaleToolchain prints, once a run, the warning for a workspace with
// local compiler modules this command was not built from.
func warnStaleToolchain(stderr io.Writer) {
	staleToolchainOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		root, _, err := load.ModuleRoot(".")
		if err != nil {
			root = ""
		}
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		fmt.Fprint(stderr, staleToolchain(bi, goWorkFile(dir, os.Environ()), root))
	})
}
