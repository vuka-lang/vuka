package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// A fixer finds one kind of problem in a module and how to fix it.
type fixer struct {
	name, doc string
	find      func(m *fixModule) ([]fixChange, error)
}

type fixChange struct {
	what  string
	apply func() error
}

// fixModule is what fixers look at: the module, its Vuka packages, and their
// generated Go (when it transpiles).
type fixModule struct {
	root, modPath string
	pkgs          []*load.Package
	gens          []load.Generated
	genErr        error
}

var fixers = []fixer{
	{"runtime", "require the Vuka runtime version this vuka generates code for", fixRuntime},
	{"static-names", "write statics as Type.Member in .vuka files, not by their Go names (User_Table)", fixStaticNames},
	{"orphans", "remove files vuka gen -inplace wrote for .vuka files that are gone", fixOrphans},
}

func fix(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	dry := fs.Bool("n", false, "only report what would change")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: vuka fix [-n] [fixer…]\n\nfixers:")
		for _, f := range fixers {
			fmt.Fprintf(fs.Output(), "  %-13s %s\n", f.name, f.doc)
		}
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, name := range fs.Args() {
		known := false
		for _, f := range fixers {
			known = known || f.name == name
		}
		if !known {
			fs.Usage()
			return fmt.Errorf("unknown fixer %q", name)
		}
		selected[name] = true
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r // Discover reports real paths
	}
	m := &fixModule{root: root, modPath: modPath}
	if m.pkgs, err = load.Discover(root, modPath, root, true, nil); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m.gens, _, m.genErr = load.Transpile(m.pkgs, tmp, load.Options{Bare: true})

	n := 0
	for _, f := range fixers {
		if len(selected) > 0 && !selected[f.name] {
			continue
		}
		changes, err := f.find(m)
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		for _, c := range changes {
			n++
			if *dry {
				fmt.Fprintf(stdout, "%s (not applied): %s\n", f.name, c.what)
				continue
			}
			if err := c.apply(); err != nil {
				return fmt.Errorf("%s: %s: %w", f.name, c.what, err)
			}
			fmt.Fprintf(stdout, "%s: %s\n", f.name, c.what)
		}
	}
	if n == 0 {
		fmt.Fprintln(stdout, "nothing to fix")
	}
	return nil
}

var requireRuntime = regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(transpile.RuntimePath) + `\s+(v\S+)`)

// fixRuntime upgrades the runtime when the module's generated code needs a
// newer one than go.mod requires, or needs it and go.mod doesn't have it.
func fixRuntime(m *fixModule) ([]fixChange, error) {
	if m.modPath == transpile.RuntimePath {
		return nil, nil // the runtime's own module
	}
	needed := m.genErr != nil && strings.Contains(m.genErr.Error(), transpile.RuntimePath)
	for _, g := range m.gens {
		needed = needed || bytes.Contains(g.Src, []byte(strconv.Quote(transpile.RuntimePath)))
	}
	if !needed {
		return nil, nil
	}
	mod, err := os.ReadFile(filepath.Join(m.root, "go.mod"))
	if err != nil {
		return nil, err
	}
	have := ""
	if g := requireRuntime.FindSubmatch(mod); g != nil {
		have = string(g[1])
	}
	if have != "" && compareVersions(have, transpile.RuntimeVersion) >= 0 {
		return nil, nil
	}
	what := "require " + transpile.RuntimePath + " " + transpile.RuntimeVersion
	if have != "" {
		what = "upgrade " + transpile.RuntimePath + " " + have + " → " + transpile.RuntimeVersion
	}
	return []fixChange{{what, func() error {
		c := exec.Command("go", "get", transpile.RuntimePath+"@"+transpile.RuntimeVersion)
		c.Dir = m.root
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("%v\n%s", err, out)
		}
		return nil
	}}}, nil
}

// compareVersions orders vMAJOR.MINOR.PATCH versions; a pre-release or
// pseudo-version comes before its release.
func compareVersions(a, b string) int {
	parse := func(v string) (nums [3]int, pre bool) {
		v = strings.TrimPrefix(v, "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v, pre = v[:i], true
		}
		for i, part := range strings.SplitN(v, ".", 3) {
			nums[i], _ = strconv.Atoi(part)
		}
		return nums, pre
	}
	an, ap := parse(a)
	bn, bp := parse(b)
	for i := range an {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case ap && !bp:
		return -1
	case !ap && bp:
		return 1
	}
	return 0
}

var (
	staticVar  = regexp.MustCompile(`(?m)^(?:var|const) (_?[A-Za-z][A-Za-z0-9]*_[A-Za-z]\w*)\b`)
	staticFunc = regexp.MustCompile(`(?m)^func (_?[A-Za-z][A-Za-z0-9]*_[A-Za-z]\w*)\(`)
	staticHead = regexp.MustCompile(`(?m)^func\s+([A-Za-z]\w*)(?:\[[^\]]*\])?\.([A-Za-z]\w*)\s*\(`)
)

// fixStaticNames rewrites Go names of statics (User_Table, _User_count,
// User_New) in .vuka files to the way Vuka writes them (User.Table, …).
func fixStaticNames(m *fixModule) ([]fixChange, error) {
	vukaName := map[string]string{}
	add := func(goName string) {
		name := strings.TrimPrefix(goName, "_")
		typ, member, ok := strings.Cut(name, "_")
		if ok && !strings.HasPrefix(goName, "__") && typ != "" && member != "" {
			vukaName[goName] = typ + "." + member
		}
	}
	for _, g := range m.gens {
		for _, re := range []*regexp.Regexp{staticVar, staticFunc} {
			for _, x := range re.FindAllSubmatch(g.Src, -1) {
				add(string(x[1]))
			}
		}
	}
	for _, p := range m.pkgs {
		for _, f := range p.Files {
			if f.IsVuka() {
				for _, x := range staticHead.FindAllSubmatch(f.Src, -1) {
					member := string(x[2])
					if token.IsExported(member) {
						add(string(x[1]) + "_" + member)
					} else {
						add("_" + string(x[1]) + "_" + member)
					}
				}
			}
		}
	}
	if len(vukaName) == 0 {
		return nil, nil
	}
	var changes []fixChange
	for _, p := range m.pkgs {
		for _, f := range p.Files {
			if !f.IsVuka() {
				continue
			}
			path := filepath.Join(p.Dir, f.Name)
			out, renames := renameIdents(f.Src, vukaName)
			if len(renames) == 0 {
				continue
			}
			rel, _ := filepath.Rel(m.root, path)
			out2 := out
			changes = append(changes, fixChange{
				what:  fmt.Sprintf("in %s, %s", rel, strings.Join(renames, ", ")),
				apply: func() error { return os.WriteFile(path, out2, 0o644) },
			})
		}
	}
	return changes, nil
}

// renameIdents replaces identifiers by name, outside strings and comments.
func renameIdents(src []byte, names map[string]string) ([]byte, []string) {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, 0)
	var b bytes.Buffer
	var renames []string
	last := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.IDENT {
			continue
		}
		to, ok := names[lit]
		if !ok {
			continue
		}
		off := file.Offset(pos)
		b.Write(src[last:off])
		b.WriteString(to)
		last = off + len(lit)
		renames = append(renames, fmt.Sprintf("line %d %s → %s", file.Line(pos), lit, to))
	}
	b.Write(src[last:])
	return b.Bytes(), renames
}

var generatedFrom = regexp.MustCompile(`^// Code generated by vuka from (\S+\.vuka)\. DO NOT EDIT\.`)

// fixOrphans removes files vuka gen -inplace wrote whose .vuka is gone.
func fixOrphans(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	err := filepath.WalkDir(m.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != m.root && (strings.HasPrefix(name, ".") || name == "node_modules" ||
				name == "vendor" || fileExists(filepath.Join(path, "go.mod")) || fileExists(filepath.Join(path, load.Marker))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_vuka.go") && !strings.HasSuffix(path, "_vuka_test.go") {
			return nil
		}
		head := make([]byte, 200)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		n, _ := f.Read(head)
		f.Close()
		g := generatedFrom.FindSubmatch(head[:n])
		if g == nil || fileExists(filepath.Join(filepath.Dir(path), string(g[1]))) {
			return nil
		}
		rel, _ := filepath.Rel(m.root, path)
		changes = append(changes, fixChange{
			what:  fmt.Sprintf("remove %s (%s is gone)", rel, g[1]),
			apply: func() error { return os.Remove(path) },
		})
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return changes, nil
}
