package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// fileLit is a string literal written where a decorator or typed attribute
// takes a vuka.File: embedded, and written as vuka.FileOf(key, data).
type fileLit struct {
	off, end int    // the literal in src
	rel      string // its path, slash-separated, relative to the package directory
	v        string // the variable holding the embedded content
	repl     string
	comps    []string // for a .templ file, its components
}

// copySrc copies text, taken from src at off, into w, writing each embedded
// file literal in it as its replacement.
func (f *fileState) copySrc(w *genWriter, text string, off int) {
	last := 0
	for _, l := range f.files {
		if l.off >= off+last && l.end <= off+len(text) {
			w.copy(text[last:l.off-off], off+last)
			w.gen(l.repl, l.off)
			last = l.end - off
		}
	}
	w.copy(text[last:], off+last)
}

// mayEmbed reports whether a decorator's or typed attribute's arguments hold a
// string literal, which might be a vuka.File.
func (f *fileState) mayEmbed() bool {
	for _, a := range f.attrs {
		if (a.kind == attrTyped || a.kind == attrDecorator) && strings.ContainsAny(a.Args, "\"`") {
			return true
		}
	}
	return false
}

// embedFiles finds the string literals in decorator and attribute arguments
// that Go converts to vuka.File, checks the files exist, and embeds them. It
// reports whether it found any; the caller renders f again.
func (e *engine) embedFiles(f *fileState) bool {
	changed := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || !isRuntimeNamed(e.info.Types[lit].Type, "File") {
			return true
		}
		src, ok := f.trailerSrc(f.off(lit.Pos()), len(lit.Value))
		if !ok || f.done[src] {
			return true
		}
		f.done[src] = true
		p, _ := strconv.Unquote(lit.Value)
		rel, comps, msg := e.resolveFile(p)
		if msg != "" {
			e.errs.add(f.at(src), "%s", msg)
			return true
		}
		l := &fileLit{off: src, end: src + len(lit.Value), rel: rel, comps: comps}
		for _, o := range f.files {
			if o.rel == rel {
				l.v = o.v
			}
		}
		if l.v == "" {
			l.v = "__vuka_file_" + itoa(e.nfiles)
			e.nfiles++
		}
		if len(f.files) == 0 {
			f.insert(f.pkgEnd, `; import _ "embed"`, 1)
		}
		l.repl = f.runtime() + ".FileOf(" + strconv.Quote(e.pkgPath()+":"+rel) + ", " + l.v + ")"
		f.files = append(f.files, l)
		sort.Slice(f.files, func(i, j int) bool { return f.files[i].off < f.files[j].off })
		changed = true
		return true
	})
	return changed
}

// trailerSrc is the src offset of text Vuka copied verbatim into the trailer
// (decorator and attribute arguments) at off.
func (f *fileState) trailerSrc(off, n int) (int, bool) {
	if off < f.body {
		return 0, false
	}
	rel := off - f.body
	for _, s := range f.tsegs {
		if s.copy && rel >= s.gen && rel+n <= s.gen+s.genLen {
			return s.src + rel - s.gen, true
		}
	}
	return 0, false
}

// resolveFile checks a vuka.File path: relative, inside the package
// directory (all go:embed reaches), existing; a .templ file in this package.
func (e *engine) resolveFile(p string) (rel string, comps []string, msg string) {
	slash := filepath.ToSlash(p)
	switch rel = path.Clean(slash); {
	case p == "":
		return "", nil, "a vuka.File needs a file name"
	case filepath.IsAbs(p) || strings.HasPrefix(slash, "/"):
		return "", nil, "the vuka.File " + strconv.Quote(p) + " is absolute; name it relative to the package's directory"
	case rel == ".." || strings.HasPrefix(rel, "../"):
		return "", nil, "the vuka.File " + strconv.Quote(p) + " is outside the package's directory; a file is embedded from the directory or below it"
	}
	if e.dir != "" {
		info, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			return "", nil, "no file " + strconv.Quote(p) + " in the package's directory"
		case info.IsDir():
			return "", nil, strconv.Quote(p) + " is a directory; a vuka.File is a file"
		}
	}
	if !strings.HasSuffix(rel, ".templ") {
		return rel, nil, ""
	}
	goName := strings.TrimSuffix(rel, ".templ") + "_templ.go"
	for _, f := range e.files {
		if f.vuka || f.name != goName {
			continue
		}
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Type.TypeParams != nil {
				continue
			}
			if fn, ok := e.info.Defs[fd.Name].(*types.Func); ok {
				if r := fn.Type().(*types.Signature).Results(); r.Len() == 1 && isNamed(r.At(0).Type(), templPath, "Component") {
					comps = append(comps, fd.Name.Name)
				}
			}
		}
		return rel, comps, ""
	}
	return "", nil, "the .templ file " + strconv.Quote(p) + " must be in this package"
}

func (e *engine) pkgPath() string {
	if e.importPath != "" {
		return e.importPath
	}
	return e.pkgName()
}

// renderFiles declares the embedded files' variables and registers the
// components of .templ files, before any declarer runs.
func (e *engine) renderFiles(f *fileState) {
	w, seen := &f.deco, map[string]bool{}
	for _, l := range f.files {
		if seen[l.v] {
			continue
		}
		seen[l.v] = true
		w.gen("\n//go:embed "+strconv.Quote(l.rel)+"\nvar "+l.v+" string\n", l.off)
		if strings.HasSuffix(l.rel, ".templ") {
			comps := make([]string, len(l.comps))
			for i, c := range l.comps {
				comps[i] = strconv.Quote(c) + ": " + c
			}
			w.gen("\nfunc init() {\n\t"+f.runtime()+".RegisterTempl("+strconv.Quote(e.pkgPath()+":"+l.rel)+
				", map[string]any{"+strings.Join(comps, ", ")+"})\n}\n", l.off)
		}
	}
}

func isRuntimeNamed(t types.Type, name string) bool { return isNamed(t, RuntimePath, name) }

func isNamed(t types.Type, pkg, name string) bool {
	if t == nil {
		return false
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}
