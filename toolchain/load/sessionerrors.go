package load

import (
	"cmp"
	"errors"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
)

// goDiag is one error go/parser or go/types reports in a package's Go.
type goDiag struct {
	pos token.Position
	msg string
}

// compileError is a package of the module whose Go doesn't compile, by its
// own files' errors or an import's, as a session type-checks it. It says so
// as Transpile's importer says it from the go command's report.
type compileError struct {
	path      string   // the package that can't be imported
	culprit   string   // the package whose errors diags are: path, or one it imports
	diags     []goDiag // all of culprit's errors, in the order they were reported
	generated bool     // an error is in the transpiler's own output (generatedAt)
	dir       string   // the go command's directory, for spell
	msg       string
}

func (e *compileError) Error() string { return e.msg }

// ownCompileError reports whether err is a package of the module that
// doesn't compile because of Go the user wrote — by hand, or in a .vuka
// file whose error only the Go compiler reports: what a run without the
// session would report the same, so the session keeps the run.
func ownCompileError(err error) bool {
	var ce *compileError
	return errors.As(err, &ce) && !ce.generated
}

// compileError is the error of c, whose Go has diags.
func (im *Importer) compileError(c *checkedPkg, diags []goDiag) *compileError {
	e := &compileError{path: c.path, culprit: c.path, diags: diags, generated: c.generatedAt(diags), dir: im.Dir}
	e.msg = im.failure(c.path, e.goError(), nil)
	return e
}

// generatedAt reports whether one of diags is in the transpiler's own
// output: at a position in a file the run generated that no line directive
// maps to the source the user wrote. An error the directives place in a
// .vuka file is the user's own Go there.
func (c *checkedPkg) generatedAt(diags []goDiag) bool {
	if len(c.genFiles) == 0 {
		return false
	}
	for _, d := range diags {
		if c.genFiles[d.pos.Filename] {
			return true
		}
	}
	return false
}

// dependentError is the error of path, importing the package dep is about.
func (im *Importer) dependentError(path string, dep *compileError) *compileError {
	e := &compileError{path: path, culprit: dep.culprit, diags: dep.diags, generated: dep.generated, dir: dep.dir}
	e.msg = im.failure(path, nil, []*goError{dep.goError()})
	return e
}

// goError is e's errors as the go command reports them: the compiler's
// first ten, by position, then "too many errors" once it has ten.
func (e *compileError) goError() *goError {
	shown := slices.Clone(e.diags[:min(len(e.diags), 10)])
	slices.SortStableFunc(shown, func(a, b goDiag) int {
		return cmp.Or(cmp.Compare(a.pos.Filename, b.pos.Filename), cmp.Compare(a.pos.Line, b.pos.Line), cmp.Compare(a.pos.Column, b.pos.Column))
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", e.culprit)
	last := ""
	for _, d := range shown {
		line := fmt.Sprintf("%s: %s\n", e.spell(d.pos), d.msg)
		if line != last {
			b.WriteString(line)
		}
		last = line
	}
	if len(e.diags) >= 10 {
		fmt.Fprintf(&b, "%s: too many errors\n", e.spell(shown[len(shown)-1].pos))
	}
	return &goError{Err: b.String()}
}

// spell writes pos as the go command's report has it: the compiler
// relativizes a path under its working directory, and absPos restores
// only .go paths.
func (e *compileError) spell(pos token.Position) string {
	if e.dir != "" && filepath.IsAbs(pos.Filename) && !strings.HasSuffix(pos.Filename, ".go") {
		if rel, err := filepath.Rel(e.dir, pos.Filename); err == nil && !strings.HasPrefix(rel, "..") {
			pos.Filename = rel
		}
	}
	return pos.String()
}
