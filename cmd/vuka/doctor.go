package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
)

// doctor runs `vuka doctor`: what the module's islands build with — the
// in-process esbuild (.ts, .tsx), the project's Vite, or Node's esbuild —
// and what is missing for one to build.
func doctor(args []string, stdout io.Writer) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	pa, err := moduleAssets(root)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "go\t%s\n", runtime.Version())
	node := "none: .vue and .svelte islands can't build (.ts and .tsx islands need no Node)"
	if p, err := exec.LookPath("node"); err == nil {
		v, _ := exec.Command(p, "--version").Output()
		node = strings.TrimSpace(string(v)) + " (" + p + ")"
	}
	fmt.Fprintf(w, "node\t%s\n", node)
	fr := root
	if pa != nil && pa.frontendRoot != "" {
		fr = pa.frontendRoot
	}
	npmFailing := doctorNPM(w, root, fr)
	if pa == nil || len(pa.islands) == 0 {
		fmt.Fprintln(w, "islands\tnone")
		if err := w.Flush(); err != nil {
			return err
		}
		if npmFailing {
			return errors.New("vuka doctor: npm is too old to install the frontend")
		}
		return nil
	}
	vite := hasViteConfig(fr)
	where := rel(root, fr)
	if vite {
		where += " (a Vite project)"
	}
	fmt.Fprintf(w, "frontend\t%s\n", where)
	problems := 0
	for _, ic := range pa.islands {
		mode, problem := islandMode(ic, fr, vite)
		if d := ic.diagnostic(); d != nil {
			problem = d.Message
		}
		line := fmt.Sprintf("island %s\t%s\t%s\t%s", ic.name, ic.rel, ic.framework, mode)
		if problem != "" {
			problems++
			line += "\t" + problem
		}
		fmt.Fprintln(w, line)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if problems > 0 {
		return fmt.Errorf("vuka doctor: %d island(s) can't build", problems)
	}
	if npmFailing {
		return errors.New("vuka doctor: npm is too old to install the frontend")
	}
	return nil
}

// doctorNPM writes npm's line: its version, and when it is older than
// npmMinMajor what that breaks. It reports whether that fails an install
// vuka runs: a frontend (the islands' root fr, or the app's Vite project)
// npm installs without a lockfile.
func doctorNPM(w io.Writer, root, fr string) bool {
	p, err := exec.LookPath("npm")
	if err != nil {
		return false
	}
	v := npmVersion()
	if !npmTooOld(v) {
		fmt.Fprintf(w, "npm\t%s (%s)\n", v, p)
		return false
	}
	failing := false
	for _, dir := range []string{fr, frontendDir(root)} {
		if dir != "" && fileExists(filepath.Join(dir, "package.json")) && strings.Join(installCommand(dir), " ") == "npm install" {
			failing = true
		}
	}
	what := "installs from a lockfile (npm ci) work; installing without one, or adding packages, fails"
	if failing {
		what = "the frontend has no package-lock.json, and npm 10 fails installing it"
	}
	fmt.Fprintf(w, "npm\t%s (%s)\tolder than %d: %s; %s\n", v, p, npmMinMajor, what, npmFix)
	return failing
}

// islandMode is what an island builds with, and what is missing for it.
func islandMode(ic *islandComp, frontendRoot string, vite bool) (mode, problem string) {
	_, nodeErr := exec.LookPath("node")
	switch {
	case ic.framework == "vanilla":
		return "in-process esbuild", ""
	case vite:
		if nodeErr != nil {
			return "Vite", "needs Node: install Node.js 20+"
		}
		return "Vite", ""
	case ic.framework == "react":
		if rt := reactRuntime(frontendRoot); rt != "" {
			return "in-process esbuild (" + rt + ")", ""
		}
		return "in-process esbuild", "needs react and react-dom (or preact) in " + filepath.Join(frontendRoot, "node_modules")
	}
	if nodeErr != nil {
		return "Node esbuild", "needs Node: write it as a .tsx or .ts island, or install Node.js 20+"
	}
	for _, dep := range frameworkDeps[ic.framework] {
		if fi, err := os.Stat(filepath.Join(frontendRoot, "node_modules", dep)); err != nil || !fi.IsDir() {
			return "Node esbuild", "needs " + strings.Join(frameworkDeps[ic.framework], ", ") + " in node_modules"
		}
	}
	return "Node esbuild", ""
}
