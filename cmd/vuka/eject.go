package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

const ejectUsage = `usage: vuka eject [-rm] [file.vuka… | dir | dir/...]

Eject writes the Go generated for .vuka files beside them, as vuka gen
-inplace does (the module by default). With -rm the Go becomes the source:
each .vuka file, its earlier forms rewritten first as vuka fix attributes,
queries and db-schema do, is deleted and its Go kept as x.go (x_vuka.go
when x.go is taken), without the generated-code header or line directives;
its .vuka.html files stay, compiled to x_html.go, so the app builds with go
build and no vuka command.
`

func eject(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("eject", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), ejectUsage) }
	rm := fs.Bool("rm", false, "delete each .vuka file, keeping its Go as source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var patterns, extra []string
	files := map[string]bool{}
	for _, a := range fs.Args() {
		if !strings.HasSuffix(a, ".vuka") {
			patterns = append(patterns, a)
			continue
		}
		abs, err := filepath.Abs(a)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return err
		}
		files[realPathOf(abs)] = true
		if dir := filepath.Dir(abs); !slices.Contains(extra, dir) {
			extra = append(extra, dir)
		}
	}
	if len(patterns) == 0 && len(files) == 0 {
		patterns = []string{"./..."}
	}
	extra = slices.DeleteFunc(extra, func(dir string) bool {
		return slices.ContainsFunc(patterns, func(p string) bool { return inPattern(dir, p) })
	})
	if *rm {
		if err := ejectFixes(patterns, extra, files); err != nil {
			return err
		}
	}
	root, gens, err := inPlace(patterns, extra, load.Options{})
	if err != nil {
		return err
	}
	for _, g := range gens {
		vuka := strings.HasSuffix(g.Source, ".vuka")
		if !files[realPathOf(g.Source)] && !slices.ContainsFunc(patterns, func(p string) bool { return inPattern(filepath.Dir(g.Target), p) }) {
			continue
		}
		target, src := g.Target, g.Src
		if *rm && vuka {
			target, src = ejectedName(g.Source), ejected(g.Src)
			if filepath.Dir(g.Target) != filepath.Dir(g.Source) || load.Bracketed(strings.TrimSuffix(filepath.Base(g.Source), ".vuka")) {
				// A [id] folder's or file's Go goes where the go command
				// sees it: param_id/x.go, and [id].vuka param_id.go.
				target = filepath.Join(filepath.Dir(g.Target), strings.TrimSuffix(filepath.Base(g.Target), "_vuka.go")+".go")
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if old, err := os.ReadFile(target); err != nil || !bytes.Equal(old, src) {
			if err := os.WriteFile(target, src, 0o644); err != nil {
				return err
			}
		}
		rel := relPath(root, target)
		if *rm && vuka && routeBundle.Match(src) {
			fmt.Fprintf(stdout, "warning: %s: a vuka.Compose of route decorators registers nothing in plain Go; pass its options to web.Routes.Get, Live… where it is used\n", rel)
		}
		if !*rm || !vuka {
			fmt.Fprintln(stdout, "wrote", rel)
			continue
		}
		if target != g.Target {
			if err := os.Remove(g.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := os.Remove(g.Source); err != nil {
			return err
		}
		from := relPath(root, g.Source)
		fmt.Fprintf(stdout, "ejected %s: %s\n", from, rel)
	}
	if *rm {
		if err := linkEjected(gens, stdout); err != nil {
			return err
		}
		// What vuka still generates (the link file, pages/route.go, the
		// templates' Go) is written again from the Go the sources became.
		return gen(append([]string{"-inplace"}, append(patterns, extra...)...), io.Discard)
	}
	return nil
}

// linkEjected imports, in each main package of the module, the packages
// just ejected that vuka linked into it implicitly (load.AutoLink): plain
// go build links only what main imports, and a registering package it
// doesn't would serve nothing.
func linkEjected(gens []load.Generated, stdout io.Writer) error {
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	var ejected []load.Generated
	for _, g := range gens {
		if _, err := os.Stat(g.Source); strings.HasSuffix(g.Source, ".vuka") && errors.Is(err, os.ErrNotExist) {
			ejected = append(ejected, g)
		}
	}
	if len(ejected) == 0 {
		return nil
	}
	var pkgs []*load.Package
	for _, g := range ejected {
		dir := filepath.Dir(g.Target)
		rel, err := filepath.Rel(realPathOf(root), realPathOf(dir))
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), strings.TrimSuffix(g.Source, ".vuka")+".go", nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		pkgs = append(pkgs, &load.Package{Dir: dir, ImportPath: path.Join(modPath, filepath.ToSlash(rel)), Name: f.Name.Name})
	}
	_, mains, err := load.AutoLink(root, modPath, pkgs, ejected)
	if err != nil {
		return err
	}
	for _, m := range mains {
		file, src, err := mainSource(m.Dir)
		if err != nil || file == "" {
			continue
		}
		out, added := addBlankImports(src, m.Imports)
		if len(added) == 0 {
			continue
		}
		if strings.HasSuffix(file, ".go") {
			if f, err := format.Source(out); err == nil {
				out = f
			}
		}
		if err := os.WriteFile(file, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "linked into %s: %s\n", relPath(root, file), strings.Join(added, ", "))
	}
	return nil
}

// mainSource is the file of the main package in dir declaring func main
// (.go, else .vuka), and its source.
func mainSource(dir string) (string, []byte, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	for _, ext := range []string{".go", ".vuka"} {
		for _, e := range ents {
			name := e.Name()
			if !strings.HasSuffix(name, ext) || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return "", nil, err
			}
			if mainFunc.Match(src) {
				return filepath.Join(dir, name), src, nil
			}
		}
	}
	return "", nil, nil
}

var mainFunc = regexp.MustCompile(`(?m)^func main\(\)`)

// addBlankImports adds _ "path" for each of paths src doesn't import, to
// its last parenthesized import block (else a block of its own after the
// package clause); added are the paths it added.
func addBlankImports(src []byte, paths []string) (out []byte, added []string) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly)
	if err != nil {
		return src, nil
	}
	have := map[string]bool{}
	for _, imp := range f.Imports {
		have[strings.Trim(imp.Path.Value, `"`)] = true
	}
	var lines strings.Builder
	for _, p := range paths {
		if !have[p] {
			added = append(added, p)
			fmt.Fprintf(&lines, "\t_ %q\n", p)
		}
	}
	if len(added) == 0 {
		return src, nil
	}
	fset := token.NewFileSet()
	f, _ = parser.ParseFile(fset, "", src, parser.ImportsOnly)
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	for i := len(f.Decls) - 1; i >= 0; i-- {
		if gd, ok := f.Decls[i].(*ast.GenDecl); ok && gd.Tok == token.IMPORT && gd.Rparen.IsValid() {
			at := off(gd.Rparen)
			return []byte(string(src[:at]) + "\n" + lines.String() + string(src[at:])), added
		}
	}
	at := off(f.Name.End())
	if len(f.Decls) > 0 {
		at = off(f.Decls[len(f.Decls)-1].End())
	}
	return []byte(string(src[:at]) + "\n\nimport (\n" + lines.String() + ")" + string(src[at:])), added
}

// relPath is path relative to root, through symbolic links.
func relPath(root, path string) string {
	rel, err := filepath.Rel(realPathOf(root), filepath.Join(realPathOf(filepath.Dir(path)), filepath.Base(path)))
	if err != nil {
		return path
	}
	return rel
}

// ejectedName is the Go file an ejected .vuka file becomes: x.go, or its
// generated file's name when x.go is taken.
func ejectedName(source string) string {
	name := strings.TrimSuffix(source, ".vuka") + ".go"
	if _, err := os.Stat(name); err == nil {
		return filepath.Join(filepath.Dir(source), transpile.GoName(filepath.Base(source)))
	}
	return name
}

var (
	lineComment   = regexp.MustCompile(`/\*line [^*]*\*/`)
	generatedHead = regexp.MustCompile(`^// Code generated by vuka from \S+\. DO NOT EDIT\.\n\n`)
)

// ejected is generated Go as source of its own: without the generated-code
// header, and without line directives, which name a .vuka file no longer
// there (with the empty comment line gofmt puts before them in a doc).
func ejected(src []byte) []byte {
	src = generatedHead.ReplaceAll(src, nil)
	src = lineComment.ReplaceAll(src, nil)
	lines := strings.SplitAfter(string(src), "\n")
	var out []string
	for i, l := range lines {
		if strings.HasPrefix(l, "//line ") {
			continue
		}
		if strings.TrimSpace(l) == "//" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "//line ") && i > 0 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "//") {
			continue
		}
		out = append(out, l)
	}
	res := arrange([]byte(strings.Join(out, "")))
	if f, err := format.Source(res); err == nil {
		return f
	}
	return res
}

// arrange puts what Vuka wrote for a type where a person writes it: its
// constructor and schema method right after the type. Generated code keeps
// its line directives (vuka gen): a moved function takes its own, and the
// code after either seam gets one when it stops following the source.
func arrange(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	// at is the source position each line of src stands for, by its line
	// directives: "file:line", "" before the first.
	var at []string
	file, line := "", 0
	for _, l := range strings.SplitAfter(string(src), "\n") {
		if d, ok := strings.CutPrefix(strings.TrimRight(l, "\n"), "//line "); ok {
			at = append(at, "")
			// file:line or file:line:col
			parts := strings.Split(d, ":")
			if len(parts) >= 3 {
				if _, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
					if _, err := strconv.Atoi(parts[len(parts)-2]); err == nil {
						parts = parts[:len(parts)-1]
					}
				}
			}
			if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil && len(parts) > 1 {
				file, line = strings.Join(parts[:len(parts)-1], ":"), n-1
			}
			continue
		}
		line++
		if file == "" {
			at = append(at, "")
		} else {
			at = append(at, file+":"+strconv.Itoa(line))
		}
	}
	lineOf := func(o int) int { return bytes.Count(src[:o], []byte("\n")) }
	// resume is a directive for the line starting at o, when src has them.
	resume := func(o int) string {
		if n := lineOf(o); file != "" && n < len(at) && at[n] != "" && !bytes.HasPrefix(src[o:], []byte("//line ")) {
			return "//line " + at[n] + "\n"
		}
		return ""
	}
	directives := func(cg *ast.CommentGroup) bool {
		if cg == nil {
			return false
		}
		for _, c := range cg.List {
			if !strings.HasPrefix(c.Text, "//line ") {
				return false
			}
		}
		return true
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	cut := func(fd *ast.FuncDecl) string {
		start, end := off(fd.Pos()), off(fd.End())
		if fd.Doc != nil {
			start = off(fd.Doc.Pos())
		}
		for end < len(src) && src[end] == '\n' {
			end++
		}
		resumed := ""
		if end < len(src) {
			// a directive for the code after it; none at the end of the file
			resumed = resume(end)
		}
		edits = append(edits, edit{start, end, resumed})
		return string(src[start:off(fd.End())])
	}
	typeEnd := map[string]int{}
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE && len(gd.Specs) == 1 {
			typeEnd[gd.Specs[0].(*ast.TypeSpec).Name.Name] = off(gd.End())
		}
	}
	follow := map[string][]string{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Doc != nil && !directives(fd.Doc) {
			continue
		}
		typ := ""
		if fd.Recv == nil {
			for t := range typeEnd {
				if fd.Name.Name == "New"+t || fd.Name.Name == "new"+strings.ToUpper(t[:1])+t[1:] {
					typ = t
				}
			}
		} else if fd.Name.Name == "Schema" || fd.Name.Name == "LiveSchema" {
			typ = recvName(fd)
		}
		if end, ok := typeEnd[typ]; ok && off(fd.Pos()) > end {
			// the constructor first
			if fd.Recv == nil {
				follow[typ] = append([]string{cut(fd)}, follow[typ]...)
			} else {
				follow[typ] = append(follow[typ], cut(fd))
			}
		}
	}
	for t, fns := range follow {
		edits = append(edits, edit{typeEnd[t], typeEnd[t], "\n\n" + strings.Join(fns, "\n\n")})
		next := lineEnd(src, typeEnd[t]) + 1
		for next < len(src) && src[next] == '\n' {
			next++
		}
		if r := resume(next); r != "" && !slices.ContainsFunc(edits, func(e edit) bool { return e.start <= next && next < e.end }) {
			edits = append(edits, edit{next, next, r})
		}
	}
	slices.SortStableFunc(edits, func(a, b edit) int { return a.start - b.start })
	var b bytes.Buffer
	pos := 0
	for _, e := range edits {
		if e.start < pos {
			return src
		}
		b.Write(src[pos:e.start])
		b.WriteString(e.text)
		pos = e.end
	}
	b.Write(src[pos:])
	// a function moved from the end leaves the blank lines before it
	return append(bytes.TrimRight(b.Bytes(), "\n"), '\n')
}

// routeBundle is a bundle of route decorators, a .vuka decorator alias:
// plain Go passes route options instead.
var routeBundle = regexp.MustCompile(`vuka\.Compose\([^\n]*\bweb\.(Get|Post|Put|Patch|Delete|Head|Handle|Live|Controller)\(`)

// lineEnd is the offset of the newline ending the line holding o.
func lineEnd(src []byte, o int) int {
	if i := bytes.IndexByte(src[o:], '\n'); i >= 0 {
		return o + i
	}
	return len(src) - 1
}
