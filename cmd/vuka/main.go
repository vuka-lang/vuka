// Command vuka transpiles Vuka source to Go and runs the go command over it.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
)

const version = "v0.1.0-dev"

const usage = `vuka is Go with overloading and attributes.

Usage:
	vuka new <dir> [module path]
	        start a project: go.mod and a main.vuka, ready for vuka run
	vuka build|run|test|vet|install [go flags] [packages]
	        transpile the module's .vuka files into an overlay and run the go command
	vuka gen [-check] [dir | dir/...]
	        write the generated Go beside each .vuka file (default ./...)
	vuka lsp [-gopls path] [-log file]
	        language server for .vuka files (gopls behind a proxy), over stdio
	vuka version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	cmd := os.Args[1]
	switch cmd {
	case "build", "run", "test", "vet", "install":
		err = runGo(cmd, os.Args[2:], os.Stdout, os.Stderr)
	case "new":
		err = newProject(os.Args[2:], os.Stdout)
	case "gen":
		err = gen(os.Args[2:], os.Stdout)
	case "lsp":
		err = lsp(os.Args[2:])
	case "version":
		fmt.Println("vuka", version)
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "vuka: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runGo transpiles every Vuka package in the current module and runs
// `go <cmd> -overlay=…` so the generated files exist only for the compiler.
func runGo(cmd string, args []string, stdout, stderr io.Writer) error {
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return err
	}
	goArgs := []string{cmd}
	if len(pkgs) > 0 {
		tmp, err := os.MkdirTemp("", "vuka-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		_, overlay, err := load.Transpile(pkgs, tmp, load.Options{})
		if err != nil {
			return err
		}
		goArgs = append(goArgs, "-overlay="+overlay)
	}
	c := exec.Command("go", append(goArgs, args...)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, stdout, stderr
	return c.Run()
}

// gen writes generated Go beside the .vuka files, for builds and tools that run
// without vuka. With -check it only reports files that are out of date.
func gen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	check := fs.Bool("check", false, "report generated files that are missing or stale, write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	var pkgs []*load.Package
	for _, p := range patterns {
		dir, recursive := strings.CutSuffix(p, "/...")
		if dir == "" || dir == "." && recursive {
			dir = "."
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		found, err := load.Discover(root, modPath, dir, recursive, nil)
		if err != nil {
			return err
		}
		pkgs = append(pkgs, found...)
	}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	files, _, err := load.Transpile(pkgs, tmp, load.Options{})
	if err != nil {
		return err
	}
	stale := 0
	for _, f := range files {
		old, err := os.ReadFile(f.Target)
		if err == nil && bytes.Equal(old, f.Src) {
			continue
		}
		rel, _ := filepath.Rel(root, f.Target)
		if *check {
			fmt.Fprintln(stdout, "stale:", rel)
			stale++
			continue
		}
		if err := os.WriteFile(f.Target, f.Src, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "wrote", rel)
	}
	if stale > 0 {
		return fmt.Errorf("%d generated file(s) out of date; run vuka gen", stale)
	}
	return nil
}
