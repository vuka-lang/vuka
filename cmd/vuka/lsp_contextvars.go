package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// The context processors' variables a file reads by name (user, request,
// me): hover names the processor and the variable's type, definition goes
// to the processor and its entry in main's web.ContextProcessors, and
// completion offers the variables the file may read.

// varAt is the processor variable read at off in f's text.
func (f *vfile) varAt(off int) (transpile.VarRead, int, bool) {
	from := f.fromOff(off)
	for _, v := range f.vars {
		if from >= v.Off && from < v.Off+len(v.Name) && !f.goneAt(v.Off) {
			return v, f.curOff(v.Off), true
		}
	}
	return transpile.VarRead{}, 0, false
}

// contextVarRequest answers hover and definition on a processor variable;
// nil when off isn't on one. p.mu must be held.
func (p *proxy) contextVarRequest(method string, f *vfile, off int) func() any {
	if method != "textDocument/definition" && method != "textDocument/declaration" && method != "textDocument/hover" {
		return nil
	}
	v, start, ok := f.varAt(off)
	if !ok {
		return nil
	}
	text, modRoot, modPath := f.text(), p.modRoot, p.modPath
	return func() any {
		file, line, col, found := processorDecl(modRoot, modPath, v.Processor)
		if method == "textDocument/hover" {
			return markdownHover(varDoc(v, modRoot, file, found), text, start, start+len(v.Name))
		}
		var out []any
		if found {
			out = append(out, fileLocation(file, line, col, len(v.Processor.Func)))
		}
		if pos := v.Processor.Pos; pos.Filename != "" {
			n := v.Processor.End.Offset - pos.Offset
			if v.Processor.End.Line != pos.Line || n <= 0 {
				n = len(v.Processor.Display())
			}
			out = append(out, fileLocation(pos.Filename, pos.Line, pos.Column, n))
		}
		return out
	}
}

// varDoc is a variable's hover: its type, and the processor it comes from.
func varDoc(v transpile.VarRead, modRoot, file string, found bool) string {
	from := v.Processor.Display()
	if found {
		rel := file
		if r, err := filepath.Rel(modRoot, file); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		} else {
			rel = filepath.Base(file)
		}
		from += " (" + rel + ")"
	}
	doc := "```go\nvar " + v.Name + " " + v.Type + "\n```\n\nfrom " + from
	if pos := v.Processor.Pos; pos.Filename != "" {
		doc += ", listed in web.ContextProcessors in " + filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
	}
	return doc
}

// fileLocation is an LSP location in file, at a 1-based line and column, n
// bytes long.
func fileLocation(file string, line, col, n int) map[string]any {
	src, _ := os.ReadFile(file)
	start := lineColOffset(src, line, col)
	return map[string]any{"uri": pathToURI(file), "range": lspRange{positionOf(src, start), positionOf(src, min(start+n, len(src)))}}
}

// lineColOffset is the offset of a 1-based line and byte column in src.
func lineColOffset(src []byte, line, col int) int {
	off := 0
	for l := 1; l < line && off < len(src); l++ {
		i := strings.IndexByte(string(src[off:]), '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	return min(off+col-1, len(src))
}

var (
	pkgDirsMu sync.Mutex
	pkgDirs   = map[string]string{} // import path → its directory, found with go list
)

// processorDecl is where a processor's function is declared: its file,
// line and column.
func processorDecl(modRoot, modPath string, pr transpile.Processor) (string, int, int, bool) {
	dir := ""
	if rel, ok := strings.CutPrefix(pr.Path, modPath+"/"); ok && modPath != "" {
		dir = filepath.Join(modRoot, filepath.FromSlash(rel))
	} else {
		pkgDirsMu.Lock()
		d, ok := pkgDirs[pr.Path]
		pkgDirsMu.Unlock()
		if !ok {
			cmd := exec.Command("go", "list", "-find", "-f", "{{.Dir}}", pr.Path)
			cmd.Dir = modRoot
			if out, err := cmd.Output(); err == nil {
				d = strings.TrimSpace(string(out))
			}
			pkgDirsMu.Lock()
			pkgDirs[pr.Path] = d
			pkgDirsMu.Unlock()
		}
		dir = d
	}
	if dir == "" {
		return "", 0, 0, false
	}
	entries, _ := os.ReadDir(dir)
	decl := regexp.MustCompile(`(?m)^func\s+(` + regexp.QuoteMeta(pr.Func) + `)\s*\(`)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") || !strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") {
			continue
		}
		if strings.HasSuffix(n, ".go") && strings.HasSuffix(strings.TrimSuffix(n, ".go"), "_vuka") {
			continue // generated from a .vuka file beside it
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		if m := decl.FindSubmatchIndex(src); m != nil {
			line := 1 + strings.Count(string(src[:m[2]]), "\n")
			return filepath.Join(dir, n), line, m[2] - (strings.LastIndexByte(string(src[:m[2]]), '\n') + 1) + 1, true
		}
	}
	return "", 0, 0, false
}

// varPrefix is the identifier being typed at off in src, where a bare name
// goes: its start, or -1 after a dot or in no identifier.
func varPrefix(src []byte, off int) int {
	start := off
	for start > 0 && isIdentByte(src[start-1]) {
		start--
	}
	if start > 0 && src[start-1] == '.' || start < len(src) && start < off && src[start] >= '0' && src[start] <= '9' {
		return -1
	}
	return start
}

// varItems are completion items for the variables f may read whose names
// start with what is typed in r.
func varItems(f *vfile, typed string, r lspRange) []any {
	var out []any
	for _, v := range f.avail {
		if !strings.HasPrefix(v.Name, typed) {
			continue
		}
		out = append(out, map[string]any{
			"label":         v.Name,
			"kind":          6, // Variable
			"detail":        v.Type,
			"documentation": map[string]any{"kind": "markdown", "value": "from " + v.Processor.Display()},
			"sortText":      "0" + v.Name,
			"textEdit":      map[string]any{"range": r, "newText": v.Name},
		})
	}
	return out
}

// varTokens are the semantic tokens of f's reads of processor variables.
func (p *proxy) varTokens(f *vfile) []semToken {
	p.sem.mu.Lock()
	base := p.sem.base
	p.sem.mu.Unlock()
	kind := base + uint32(len(vukaTokenTypes)-1)
	text := f.text()
	var out []semToken
	for _, v := range f.vars {
		if f.goneAt(v.Off) {
			continue
		}
		off := f.curOff(v.Off)
		if off+len(v.Name) > len(text) || string(text[off:off+len(v.Name)]) != v.Name {
			continue
		}
		s, e := positionOf(text, off), positionOf(text, off+len(v.Name))
		if s.Line == e.Line {
			out = append(out, semToken{s.Line, s.Character, e.Character - s.Character, kind, 0})
		}
	}
	return out
}
