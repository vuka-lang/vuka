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

	"github.com/vuka-lang/vuka/internal/format"
	"github.com/vuka-lang/vuka/internal/load"
)

// vukaFmt formats .vuka files, as gofmt does .go files: it prints them,
// writes them back (-w), lists those that differ (-l) or shows the diff (-d).
func vukaFmt(args []string, stdout, stderr io.Writer) error {
	fl := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fl.SetOutput(stderr)
	list := fl.Bool("l", false, "list files whose formatting differs")
	write := fl.Bool("w", false, "write the result to the file instead of printing it")
	diff := fl.Bool("d", false, "print a diff instead of the formatted file")
	if err := fl.Parse(args); err != nil {
		return err
	}
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
			if err := fmtFile(file, *list, *write, *diff, stdout); err != nil {
				report(err)
			}
		}
	}
	if failed {
		return errors.New("vuka fmt: some files could not be formatted")
	}
	return nil
}

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
		if strings.HasSuffix(name, ".vuka") && !strings.HasPrefix(name, ".") {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

func fmtFile(path string, list, write, diff bool, stdout io.Writer) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := format.Source(src)
	if err != nil {
		return fmt.Errorf("%s:%v", path, err)
	}
	same := bytes.Equal(src, out)
	if list && !same {
		fmt.Fprintln(stdout, path)
	}
	if write && !same {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
			return err
		}
	}
	if diff && !same {
		d, err := unifiedDiff(path, src, out)
		if err != nil {
			return err
		}
		stdout.Write(d)
	}
	if !list && !write && !diff {
		stdout.Write(out)
	}
	return nil
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
