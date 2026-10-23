package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// vukaFmt formats .vuka and .vuka.html files, as gofmt does .go files: it
// prints them, writes them back (-w), lists those that differ (-l), shows
// the diff (-d), or fails listing them (-check, for CI). -rewrite writes a
// .vuka.html file in the Go-header form in the <script lang="go"> one
// first. A .vuka.html file is laid out by the [fmt] table of the project's
// vuka.toml (width, tailwind).
func vukaFmt(args []string, stdout, stderr io.Writer) error {
	fl := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fl.SetOutput(stderr)
	list := fl.Bool("l", false, "list files whose formatting differs")
	write := fl.Bool("w", false, "write the result to the file instead of printing it")
	diff := fl.Bool("d", false, "print a diff instead of the formatted file")
	check := fl.Bool("check", false, "list files whose formatting differs and fail if any does; write nothing")
	rewrite := fl.Bool("rewrite", false, "rewrite .vuka.html files in the Go-header form in the <script lang=\"go\"> form")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *check {
		*list, *write, *diff = true, false, false
	}
	unformatted := false
	paths := fl.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	failed := false
	report := func(err error) {
		fmt.Fprintln(stderr, err)
		failed = true
	}
	for _, p := range paths {
		files, err := vukaFiles(p)
		if err != nil {
			report(err)
			continue
		}
		for _, file := range files {
			same, err := fmtFile(file, fmtMode{list: *list, write: *write, diff: *diff, rewrite: *rewrite}, stdout)
			if err != nil {
				report(err)
			}
			unformatted = unformatted || !same
		}
	}
	if failed {
		return errors.New("vuka fmt: some files could not be formatted")
	}
	if *check && unformatted {
		return errors.New("vuka fmt: the files listed are not formatted: run vuka fmt -w")
	}
	return nil
}

// fmtMode is what vuka fmt does with a file.
type fmtMode struct{ list, write, diff, rewrite bool }

// vukaFiles lists the .vuka files at path: the file itself, or those under
// the directory, skipping hidden directories, node_modules, vendor and build
// modules vuka made.
func vukaFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != path && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, load.Marker)); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if (strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".vuka.html")) && !strings.HasPrefix(name, ".") {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

// fmtFile formats one file as mode says, and reports whether it was
// formatted already.
func fmtFile(path string, mode fmtMode, stdout io.Writer) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	var out []byte
	if strings.HasSuffix(path, ".vuka.html") {
		out, err = formatTemplateFile(path, src, mode.rewrite)
	} else {
		out, err = format.Source(src)
	}
	if err != nil {
		return true, fmt.Errorf("%s:%v", path, err)
	}
	same := bytes.Equal(src, out)
	if mode.list && !same {
		fmt.Fprintln(stdout, path)
	}
	if mode.write && !same {
		info, err := os.Stat(path)
		if err != nil {
			return same, err
		}
		if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
			return same, err
		}
	}
	if mode.diff && !same {
		d, err := unifiedDiff(path, src, out)
		if err != nil {
			return same, err
		}
		stdout.Write(d)
	}
	if !mode.list && !mode.write && !mode.diff {
		stdout.Write(out)
	}
	return same, nil
}

// formatTemplateFile formats a .vuka.html file by its project's options,
// rewritten in the script form first when rewrite is set.
func formatTemplateFile(path string, src []byte, rewrite bool) ([]byte, error) {
	o, err := format.LoadOptions(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if rewrite {
		src, _ = transpile.ToScriptForm(src)
	}
	return format.TemplateWith(src, format.DirPackage(filepath.Dir(path)), o)
}

// unifiedDiff runs diff -u over the original and the formatted file.
func unifiedDiff(path string, a, b []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "vuka-fmt-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	orig, formatted := filepath.Join(dir, "orig"), filepath.Join(dir, "formatted")
	if err := os.WriteFile(orig, a, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(formatted, b, 0o644); err != nil {
		return nil, err
	}
	out, err := exec.Command("diff", "-u", "--label", path+".orig", "--label", path, orig, formatted).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		err = nil
	}
	return out, err
}
