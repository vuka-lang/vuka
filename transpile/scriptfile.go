package transpile

import (
	"go/token"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// A template of markup alone has its script in a file beside it, named as
// it is: orders.vuka.html's is orders.go (plain Go, which gopls reads as it
// is) or orders.vuka. The script file declares the package, the struct the
// markup renders and its methods; the markup sees them, and the script
// file's imports, as it sees an inline script's.

// scriptFile is the file of others holding the script of the template
// name: <stem>.go or <stem>.vuka in its directory, when the template is
// markup alone — no package clause, no Go header; the imports only its
// markup uses may come first. nil otherwise.
func scriptFile(name string, src []byte, head TemplateHead, others []File) *File {
	if head.Package != "" {
		return nil
	}
	lines := strings.SplitAfter(string(src), "\n")
	if isShape(lines, head.Top) {
		return nil
	}
	for _, l := range lines[min(head.Top, len(lines)):] {
		if funcLine.MatchString(l) {
			return nil // a template func: E0153
		}
	}
	return siblingScript(name, others)
}

// siblingScript is the .go or .vuka file of others named as the template
// name is, in its directory.
func siblingScript(name string, others []File) *File {
	slash := filepath.ToSlash(name)
	dir, stem := path.Dir(slash), strings.TrimSuffix(path.Base(slash), ".vuka.html")
	for _, ext := range []string{".go", ".vuka"} {
		for i, o := range others {
			n := filepath.ToSlash(o.Name)
			if path.Dir(n) == dir && path.Base(n) == stem+ext {
				return &others[i]
			}
		}
	}
	return nil
}

// scriptShape is the struct the script file declares, as the template
// name's markup, src, renders it.
func scriptShape(name string, src []byte, script File) (*Shape, *Error) {
	view := HeaderView(script.Src)
	text := string(view)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	scriptLines := strings.Count(text, "\n")
	text += string(src)
	lines := strings.SplitAfter(text, "\n")
	sh, err := parseShape(path.Base(filepath.ToSlash(name)), []byte(text), lines, parseTemplateHead([]byte(text)))
	if err != nil {
		if err.Pos.Line > scriptLines {
			err.Pos.Line -= scriptLines
			return nil, err
		}
		err.Pos.Filename = script.Name
		err.Msg = path.Base(filepath.ToSlash(script.Name)) + ", the script of " + path.Base(filepath.ToSlash(name)) + ": " + err.Msg
		return nil, err
	}
	sh.Markup -= scriptLines
	sh.Header = 0
	sh.File = script.Name
	sh.open = -1
	return sh, nil
}

// declaredTwice is E0174 for a template whose script declares its struct
// when the script file beside it declares it too.
func declaredTwice(name string, sh *Shape, others []File) *Error {
	script := siblingScript(name, others)
	if script == nil {
		return nil
	}
	decl := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(sh.Type) + `\b`)
	if !decl.Match(script.Src) {
		return nil
	}
	base := path.Base(filepath.ToSlash(script.Name))
	return (&Error{Code: "E0174", Pos: token.Position{Line: sh.TypeLine, Column: 1},
		Msg: sh.Type + " is declared twice: in this template's script and in " + base + ", the script file beside it"}).
		help("keep one: move the script into %s and leave the template its markup, or delete %s", base, base)
}

// ParseShapeScript is ParseShapeFile for a template of markup alone, its
// struct declared by script, the file beside it (siblingScript): nil when
// the template has a Go header of its own or script declares no struct.
func ParseShapeScript(name string, src []byte, script File) (*Shape, *Error) {
	view := HeaderView(src)
	if s := scriptFile(name, view, parseTemplateHead(view), []File{script}); s == nil {
		return nil, nil
	}
	return scriptShape(name, view, script)
}

// ScriptFileNames are the names of the files that may hold the script of the
// template name, in the order the compiler looks: <stem>.go, <stem>.vuka.
func ScriptFileNames(name string) []string {
	stem := strings.TrimSuffix(name, ".vuka.html")
	return []string{stem + ".go", stem + ".vuka"}
}
