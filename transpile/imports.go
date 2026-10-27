package transpile

import (
	"bytes"
	"go/parser"
	"go/scanner"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// A file that doesn't parse says so once a declaration: an import with no
// path is one error at the import, naming the import the file's code needs,
// and each top-level declaration is parsed on its own, so a broken one is
// one error and the declarations after it parse as they are, rather than
// the parser reporting what it lost track of.

// maxSyntaxErrors is how many syntax errors a file reports at most.
const maxSyntaxErrors = 10

// syntaxErrors adds the syntax errors of this round's text, which doesn't
// parse (err): one for each import without a path, then the first of each
// top-level declaration that doesn't parse on its own.
func (f *fileState) syntaxErrors(err error, errs *ErrorList) {
	list, ok := err.(scanner.ErrorList)
	if !ok || len(list) == 0 {
		errs.add("E0003", f.at(0), "%v", err)
		return
	}
	text := bytes.Clone(f.text)
	bads := badImports(text)
	for _, b := range bads {
		blankOut(text, b.at, b.end)
	}
	for _, b := range bads {
		e := errs.add("E0003", f.pos(b.at), "%s", b.msg)
		e.Len = len("import")
		e.Help = f.importHelp(text, b)
	}
	if len(bads) > 0 {
		if _, err := parser.ParseFile(token.NewFileSet(), f.path, text, parser.ParseComments); err == nil {
			return
		}
	}
	n := len(bads)
	starts := declStarts(text)
	bounds := append([]int{0}, starts...)
	bounds = append(bounds, len(text))
	for i := 0; i+1 < len(bounds) && n < maxSyntaxErrors; i++ {
		from, to := bounds[i], bounds[i+1]
		if from == to {
			continue
		}
		alone := bytes.Clone(text)
		if len(starts) > 0 {
			// The package clause and imports stay; the other declarations go.
			blankOut(alone, starts[0], from)
			blankOut(alone, max(to, starts[0]), len(alone))
		}
		_, err := parser.ParseFile(token.NewFileSet(), f.path, alone, parser.ParseComments)
		el, ok := err.(scanner.ErrorList)
		if !ok || len(el) == 0 {
			continue
		}
		off, msg := el[0].Pos.Offset, el[0].Msg
		if end := len(bytes.TrimRight(alone[:to], " \t\r\n")); off >= end && end > from {
			// The declaration ends before it's complete.
			off, msg = end, strings.ReplaceAll(msg, "'EOF'", "the end of the declaration")
		}
		errs.add("E0003", f.pos(off), "%s", msg)
		n++
	}
	if n == 0 {
		// No declaration fails alone: the parser's first error says it.
		errs.add("E0003", f.pos(list[0].Pos.Offset), "%s", list[0].Msg)
	}
}

// blankOut turns text[from:to] into spaces, keeping its lines.
func blankOut(text []byte, from, to int) {
	for i := from; i < to && i < len(text); i++ {
		if text[i] != '\n' {
			text[i] = ' '
		}
	}
}

// declStarts are the offsets of text's top-level declarations after its
// imports: a type, func, var or const keyword starting a line, or an
// attribute; a keyword in a string or comment doesn't count.
func declStarts(text []byte) []int {
	fset := token.NewFileSet()
	tf := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(tf, text, func(token.Position, string) {}, 0)
	var out []int
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			return out
		}
		off := tf.Offset(p)
		if off > 0 && text[off-1] != '\n' {
			continue
		}
		switch {
		case t == token.TYPE || t == token.FUNC || t == token.VAR || t == token.CONST:
			out = append(out, off)
		case t == token.ILLEGAL && lit == "@" && len(out) > 0:
			out = append(out, off)
		}
	}
}

// badImport is an import declaration that doesn't parse.
type badImport struct {
	at, end int    // the import keyword; the next declaration
	msg     string // what is wrong
	help    string // what to do, when it isn't to add a path
}

// badImports are text's import declarations that don't parse: an import
// with no path, or a path that isn't a closed string, or a block without
// its ")".
func badImports(text []byte) []badImport {
	fset := token.NewFileSet()
	tf := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	var scanErrs []int // offsets of the scanner's errors
	sc.Init(tf, text, func(p token.Position, _ string) { scanErrs = append(scanErrs, p.Offset) }, 0)
	type tok struct {
		off int
		tok token.Token
		lit string
	}
	var toks []tok
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			toks = append(toks, tok{tf.Offset(p), t, lit})
			break
		}
		toks = append(toks, tok{tf.Offset(p), t, lit})
	}
	i := 0
	next := func() tok {
		t := toks[min(i, len(toks)-1)]
		i++
		return t
	}
	// package name ;
	if next().tok != token.PACKAGE {
		return nil
	}
	next()
	next()
	broken := func(off, end int) bool {
		for _, e := range scanErrs {
			if e >= off && e < end {
				return true
			}
		}
		return false
	}
	var out []badImport
	for {
		kw := next()
		if kw.tok != token.IMPORT {
			return out
		}
		bad := func(msg, help string) {
			// It runs to the next declaration, or import.
			end := len(text)
			for _, d := range declStarts(text) {
				if d > kw.off {
					end = d
					break
				}
			}
			for _, t := range toks {
				if t.tok == token.IMPORT && t.off > kw.off && t.off < end && text[t.off-1] == '\n' {
					end = t.off
					break
				}
			}
			out = append(out, badImport{at: kw.off, end: end, msg: msg, help: help})
			for i < len(toks) && toks[i].off < end {
				i++
			}
		}
		// spec reads one import spec, reporting what is wrong with it and
		// what to do, when it isn't to add a path.
		spec := func() (string, string) {
			t := next()
			if t.tok == token.IDENT || t.tok == token.PERIOD {
				t = next()
			}
			switch {
			case t.tok != token.STRING:
				i--
				return "no path", ""
			case broken(t.off, t.off+len(t.lit)+1) || !strings.HasSuffix(t.lit, `"`) && !strings.HasSuffix(t.lit, "`") || len(t.lit) < 2:
				return "a path without its closing quote", "close the quote: " + strconv.Quote(strings.Trim(t.lit, "\"` \t\r"))
			}
			if s := next(); s.tok != token.SEMICOLON && s.tok != token.RPAREN {
				i--
				return "something after its path", "write each import on a line of its own: a name, then the path"
			}
			if toks[i-1].tok == token.RPAREN {
				i--
			}
			return "", ""
		}
		if toks[min(i, len(toks)-1)].tok != token.LPAREN {
			if why, help := spec(); why != "" {
				bad("import has "+why, help)
			}
			continue
		}
		next() // (
		for {
			t := toks[min(i, len(toks)-1)]
			if t.tok == token.RPAREN {
				next()
				if s := toks[min(i, len(toks)-1)]; s.tok == token.SEMICOLON {
					next()
				}
				break
			}
			if t.tok == token.EOF || t.tok.IsKeyword() || t.tok == token.ILLEGAL && t.lit == "@" {
				bad("import ( has no closing )", `close the block: add ")" after its last import`)
				break
			}
			if t.tok == token.SEMICOLON {
				next()
				continue
			}
			if why, help := spec(); why != "" {
				bad("an import in this block has "+why, help)
				break
			}
		}
	}
}

// importHelp is what to do about the import b: the imports of the
// packages the file uses without one, when vuka can tell them.
func (f *fileState) importHelp(text []byte, b badImport) string {
	if b.help != "" {
		return b.help
	}
	names := f.unimportedNames(text)
	var paths []string
	var unknown []string
	for _, n := range names {
		if p := f.eng.pathFor(f, n); p != "" {
			paths = append(paths, strconv.Quote(p))
		} else {
			unknown = append(unknown, n)
		}
	}
	uses := "the file uses " + andList(names)
	switch {
	case len(names) == 0:
		return `give it the path of the package to import, in quotes: import "…"; or delete it`
	case len(unknown) > 0 && len(paths) == 0:
		return "add the path of the package the file uses as " + andList(unknown) + `, in quotes: import "…"`
	case len(paths) == 1 && len(unknown) == 0:
		return "add the path: import " + paths[0] + " (" + uses + ")"
	case len(unknown) == 0:
		return "add the paths: import (" + strings.Join(paths, "; ") + ") (" + uses + ")"
	}
	return "add the paths: import (" + strings.Join(paths, "; ") + "; …) (" + uses + ")"
}

func andList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// unimportedNames are the package names text uses — in a qualified
// attribute, or a selector a package of the file's can be — that the
// file's imports that parse don't provide, in the order the file uses them.
func (f *fileState) unimportedNames(text []byte) []string {
	have := map[string]bool{}
	if af, _ := parser.ParseFile(token.NewFileSet(), "", text, parser.ImportsOnly); af != nil {
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if imp.Name != nil {
				have[imp.Name.Name] = true
			} else {
				have[importName(p)] = true
			}
		}
	}
	var names []string
	add := func(n string) {
		if !have[n] && n != "vuka" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	type use struct {
		off  int
		name string
	}
	var uses []use
	for _, a := range f.attrs {
		if pkg, _, ok := strings.Cut(a.Name, "."); ok {
			uses = append(uses, use{f.cur.fromOrig(a.start), pkg})
		}
	}
	fset := token.NewFileSet()
	tf := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(tf, text, nil, 0)
	var prev [2]token.Token
	var prevLit string
	var prevOff int
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			break
		}
		if t == token.IDENT && prev[0] == token.PERIOD && prev[1] == token.IDENT {
			uses = append(uses, use{prevOff, prevLit})
		}
		if t == token.IDENT && prev[0] != token.PERIOD {
			prevLit, prevOff = lit, tf.Offset(p)
		}
		prev[1], prev[0] = prev[0], t
	}
	slices.SortStableFunc(uses, func(a, b use) int { return a.off - b.off })
	attrs := map[string]bool{}
	for _, a := range f.attrs {
		if pkg, _, ok := strings.Cut(a.Name, "."); ok {
			attrs[pkg] = true
		}
	}
	for _, u := range uses {
		if attrs[u.name] || !f.eng.declaredName(u.name) && f.eng.pathFor(f, u.name) != "" {
			add(u.name)
		}
	}
	return names
}

// declaredName reports whether name is declared at the package level in
// one of the package's files, as far as reading them tells.
func (e *engine) declaredName(name string) bool {
	return e.selfNames[name] || e.declared[name]
}

// pathFor is the one package named name the file f can mean, as
// ImportScope finds it from the package's other files and the module; ""
// when none or several.
func (e *engine) pathFor(f *fileState, name string) string {
	key := f.name + " " + name
	if p, ok := e.importPaths[key]; ok {
		return p
	}
	scope := ImportScope{ModuleRoot: e.moduleRoot}
	for _, src := range e.srcs {
		if src.Name != f.name {
			scope.Others = append(scope.Others, src.Src)
		}
	}
	p := scope.ImportFor(name)
	if e.importPaths == nil {
		e.importPaths = map[string]string{}
	}
	e.importPaths[key] = p
	return p
}
