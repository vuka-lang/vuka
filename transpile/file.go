package transpile

import (
	"go/ast"
	"go/parser"
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
	key      string // the File: "importpath:path" of the package the file belongs to
	v        string // the variable holding the embedded content
	repl     string
	tpl      *templRef // for a .templ file, its components
}

// templRef is a .templ file's components, in the package itself (pkg "") or
// in the package of a subdirectory.
type templRef struct {
	pkg   string
	comps []templComp
}

type templComp struct {
	name   string
	params []string
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
		l, msg := e.resolveFile(p)
		if msg != "" {
			e.errs.add(f.at(src), "%s", msg)
			return true
		}
		l.off, l.end = src, src+len(lit.Value)
		for _, o := range f.files {
			if o.rel == l.rel {
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
		if l.tpl != nil && l.tpl.pkg != "" {
			f.importAs(l.tpl.pkg, e.tplImport(l.tpl.pkg))
		}
		l.repl = f.runtime() + ".FileOf(" + strconv.Quote(l.key) + ", " + l.v + ")"
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
// directory (all go:embed reaches), existing. A .templ file is one of this
// package, or of the package of the subdirectory it is in; its File is then
// keyed by that package, which the generated code imports.
func (e *engine) resolveFile(p string) (l *fileLit, msg string) {
	slash := filepath.ToSlash(p)
	rel := path.Clean(slash)
	switch {
	case p == "":
		return nil, "a vuka.File needs a file name"
	case filepath.IsAbs(p) || strings.HasPrefix(slash, "/"):
		return nil, "the vuka.File " + strconv.Quote(p) + " is absolute; name it relative to the package's directory"
	case rel == ".." || strings.HasPrefix(rel, "../"):
		return nil, "the vuka.File " + strconv.Quote(p) + " is outside the package's directory; a file is embedded from the directory or below it"
	}
	if e.dir != "" {
		info, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			return nil, "no file " + strconv.Quote(p) + " in the package's directory"
		case info.IsDir():
			return nil, strconv.Quote(p) + " is a directory; a vuka.File is a file"
		}
	}
	l = &fileLit{rel: rel, key: e.pkgPath() + ":" + rel}
	if !strings.HasSuffix(rel, ".templ") {
		return l, ""
	}
	if dir, base := path.Split(rel); dir != "" {
		return e.subTempl(l, p, strings.TrimSuffix(dir, "/"), base)
	}
	l.tpl = &templRef{}
	for _, f := range e.files {
		if f.vuka || f.name != strings.TrimSuffix(rel, ".templ")+"_templ.go" {
			continue
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if fn, ok := e.info.Defs[fd.Name].(*types.Func); ok && fd.Recv == nil && fd.Type.TypeParams == nil {
					if sig := fn.Type().(*types.Signature); sig.Results().Len() == 1 && isNamed(sig.Results().At(0).Type(), templPath, "Component") {
						c := templComp{name: fd.Name.Name}
						for i := range sig.Params().Len() {
							c.params = append(c.params, sig.Params().At(i).Name())
						}
						l.tpl.comps = append(l.tpl.comps, c)
					}
				}
			}
		}
		return l, ""
	}
	return nil, "the .templ file " + strconv.Quote(p) + " is not in this package: its package clause differs from the package's"
}

// subTempl resolves a .templ file in a subdirectory, a package of its own:
// its exported components, read from the Go templ makes of it.
func (e *engine) subTempl(l *fileLit, p, dir, base string) (*fileLit, string) {
	if e.templ == nil || e.dir == "" {
		return nil, "the .templ file " + strconv.Quote(p) + " is in a subdirectory, which only a module build can compile"
	}
	tf, err := e.templ(filepath.Join(e.dir, filepath.FromSlash(l.rel)))
	if err != nil {
		return nil, "the .templ file " + strconv.Quote(p) + ": " + err.Error()
	}
	file, err := parser.ParseFile(token.NewFileSet(), base, tf.Go, parser.SkipObjectResolution)
	if err != nil {
		return nil, "the .templ file " + strconv.Quote(p) + ": " + err.Error()
	}
	l.key = tf.ImportPath + ":" + base
	l.tpl = &templRef{pkg: tf.ImportPath}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Type.TypeParams != nil || !fd.Name.IsExported() || fd.Type.Results == nil || len(fd.Type.Results.List) != 1 {
			continue
		}
		if sel, ok := fd.Type.Results.List[0].Type.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Component" {
			continue
		}
		c := templComp{name: fd.Name.Name}
		for _, field := range fd.Type.Params.List {
			if len(field.Names) == 0 {
				c.params = append(c.params, "")
			}
			for _, n := range field.Names {
				c.params = append(c.params, n.Name)
			}
		}
		l.tpl.comps = append(l.tpl.comps, c)
	}
	return l, ""
}

// fileRefs are f's vuka.File literals, for tools.
func (e *engine) fileRefs(f *fileState) []FileRef {
	var refs []FileRef
	for _, l := range f.files {
		r := FileRef{Off: l.off, End: l.end, Path: filepath.Join(e.dir, filepath.FromSlash(l.rel))}
		if l.tpl != nil {
			for _, c := range l.tpl.comps {
				r.Components = append(r.Components, TemplComponent{Name: c.name, Params: c.params})
			}
		}
		refs = append(refs, r)
	}
	return refs
}

// tplImport names the package of a .templ file in a subdirectory in the
// generated code.
func (e *engine) tplImport(pkg string) string {
	if e.tplImports == nil {
		e.tplImports = map[string]string{}
	}
	if _, ok := e.tplImports[pkg]; !ok {
		e.tplImports[pkg] = "__vuka_templ" + itoa(len(e.tplImports))
	}
	return e.tplImports[pkg]
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
		if l.tpl != nil {
			rt, qual := f.runtime(), ""
			if l.tpl.pkg != "" {
				qual = e.tplImport(l.tpl.pkg) + "."
			}
			var b strings.Builder
			b.WriteString("\nfunc init() {\n\t" + rt + ".RegisterTempl(" + strconv.Quote(l.key))
			for _, c := range l.tpl.comps {
				params := make([]string, len(c.params))
				for i, p := range c.params {
					params[i] = strconv.Quote(p)
				}
				b.WriteString(", " + rt + ".TemplComponent{Name: " + strconv.Quote(c.name) + ", Func: " + qual + c.name +
					", Params: []string{" + strings.Join(params, ", ") + "}}")
			}
			b.WriteString(")\n}\n")
			w.gen(b.String(), l.off)
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
