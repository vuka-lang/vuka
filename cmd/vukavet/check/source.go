package check

import (
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// A package's .vuka.html files, as written: the checks of what their
// compiled Go no longer shows.
type htmlSource struct {
	path string
	src  string
	file *token.File

	typ    string // the struct a file in the 2.0 shape renders
	markup int    // where its markup starts; -1 for a file of templates
}

// htmlSources are the .vuka.html files of the package pass analyses.
func htmlSources(pass *analysis.Pass) []*htmlSource {
	if len(pass.Files) == 0 {
		return nil
	}
	dir := filepath.Dir(pass.Fset.File(pass.Files[0].Pos()).Name())
	paths, _ := filepath.Glob(filepath.Join(dir, "*.vuka.html"))
	var out []*htmlSource
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f := pass.Fset.AddFile(p, -1, len(b))
		f.SetLinesForContent(b)
		h := &htmlSource{path: p, src: string(b), file: f, markup: -1}
		if typ, at, ok := shapeOf(h.src); ok {
			h.typ, h.markup = typ, at
		}
		out = append(out, h)
	}
	return out
}

func (h *htmlSource) at(off, n int) analysis.Range {
	return span{h.file.Pos(off), h.file.Pos(off + n)}
}

// componentName is the type a file's headerless template is the markup
// of: order_row.vuka.html is OrderRow's.
func componentName(path string) string {
	var b strings.Builder
	up := true
	for _, r := range strings.TrimSuffix(filepath.Base(path), ".vuka.html") {
		switch {
		case r == '_' || r == '-' || r == '.':
			up = true
		case up:
			b.WriteRune(unicode.ToUpper(r))
			up = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	importLine  = regexp.MustCompile(`^import\s+(\w+\s+)?"[^"]*"\s*$`)
	packageLine = regexp.MustCompile(`^package\s+\w+\s*(//.*)?$`)
	stmtLine    = regexp.MustCompile(`^(\}\s*else\b|if\s|for\s|switch\b|case\s|[\w][\w, .]*\s*(:=|=|\+=|-=|\*=|/=|\+\+|--))`)
)

// goPart is Go text of a template: a hole's expression or a statement.
type goPart struct {
	off  int
	text string
}

// mainTemplate is the markup before a file's first func line, when the
// file starts with markup: the headerless template of its type.
func (h *htmlSource) mainTemplate() (start, end int, ok bool) {
	if h.markup >= 0 {
		end = len(h.src)
		for off := h.markup; off < len(h.src); {
			l := h.src[off:]
			if i := strings.IndexByte(l, '\n'); i >= 0 {
				l = l[:i+1]
			}
			if strings.HasPrefix(l, "func ") || strings.HasPrefix(l, "func(") {
				end = off
				break
			}
			off += len(l)
		}
		return h.markup, end, true
	}
	off := 0
	start = -1
	group := false
	for _, l := range strings.SplitAfter(h.src, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case group:
			group = t != ")"
		case start < 0 && t == "import (":
			group = true
		case start < 0 && (t == "" || strings.HasPrefix(t, "//") || importLine.MatchString(t) || packageLine.MatchString(t)):
		case strings.HasPrefix(l, "func ") || strings.HasPrefix(l, "func("):
			if start < 0 {
				return 0, 0, false
			}
			return start, off, true
		case start < 0:
			start = off
		}
		off += len(l)
	}
	return start, len(h.src), start >= 0
}

// goParts are the Go texts of src[start:end]: its holes and its
// statements' heads.
func goParts(src string, start, end int) []goPart {
	var out []goPart
	inScript := false
	for off := start; off < end; {
		nl := strings.IndexByte(src[off:end], '\n')
		lineEnd := end
		if nl >= 0 {
			lineEnd = off + nl + 1
		}
		line := src[off:lineEnd]
		t := strings.TrimLeft(line, " \t")
		lead := off + len(line) - len(t)
		switch {
		case strings.HasPrefix(t, "<script") || strings.HasPrefix(t, "<style"):
			inScript = !strings.Contains(t, "</script>") && !strings.Contains(t, "</style>")
		case inScript:
			inScript = !strings.Contains(t, "</script>") && !strings.Contains(t, "</style>")
		case stmtLine.MatchString(t):
			head := strings.TrimRight(t, " \t\r\n")
			head = strings.TrimSuffix(strings.TrimSuffix(head, "{"), " ")
			out = append(out, goPart{lead, head})
		default:
			for i := 0; i < len(line); i++ {
				switch {
				case strings.HasPrefix(line[i:], "{{"):
					i++
				case strings.HasPrefix(line[i:], "{/*"):
					if j := strings.Index(line[i:], "*/}"); j >= 0 {
						i += j + 2
					}
				case line[i] == '{':
					j := holeEnd(line, i+1)
					if j < 0 {
						i = len(line)
						continue
					}
					out = append(out, goPart{off + i + 1, line[i+1 : j]})
					i = j
				}
			}
		}
		off = lineEnd
	}
	return out
}

// holeEnd is the } closing the Go text at s[i:], or -1.
func holeEnd(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'', '`':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' && c != '`' {
					j++
				}
				j++
			}
			i = j
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if c == '}' {
					return i
				}
				return -1
			}
			depth--
		}
	}
	return -1
}

type goIdent struct {
	off  int
	name string
	def  bool // declared here: x :=, range k, v
}

// idents are the names a Go text reads or declares, past selectors' names
// and composite literals' keys.
func idents(p goPart) []goIdent {
	var sc scanner.Scanner
	fs := token.NewFileSet()
	sc.Init(fs.AddFile("", -1, len(p.text)), []byte(p.text), nil, 0)
	type tk struct {
		pos int
		tok token.Token
		lit string
	}
	var toks []tk
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		toks = append(toks, tk{int(pos) - 1, tok, lit})
	}
	define := -1
	for i, t := range toks {
		if t.tok == token.DEFINE {
			define = i
			break
		}
	}
	var out []goIdent
	for i, t := range toks {
		if t.tok != token.IDENT || i > 0 && toks[i-1].tok == token.PERIOD || i+1 < len(toks) && toks[i+1].tok == token.COLON {
			continue
		}
		out = append(out, goIdent{p.off + t.pos, t.lit, i < define})
	}
	return out
}

// bareNames warns where a headerless template's bare name is both a field
// or method of its type and a package-level name: the field is read.
func bareNames(pass *analysis.Pass, files []*htmlSource) {
	for _, h := range files {
		typ := componentName(h.path)
		if h.markup >= 0 {
			typ = h.typ
		}
		obj, _ := pass.Pkg.Scope().Lookup(typ).(*types.TypeName)
		start, end, ok := h.mainTemplate()
		if obj == nil || !ok {
			continue
		}
		parts := goParts(h.src, start, end)
		locals := map[string]bool{}
		for _, p := range parts {
			for _, id := range idents(p) {
				if id.def {
					locals[id.name] = true
				}
			}
		}
		seen := map[string]bool{}
		for _, p := range parts {
			for _, id := range idents(p) {
				pkgObj := pass.Pkg.Scope().Lookup(id.name)
				if id.def || locals[id.name] || seen[id.name] || pkgObj == nil {
					continue
				}
				if _, isType := pkgObj.(*types.TypeName); isType {
					continue
				}
				if f, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, pass.Pkg, id.name); f == nil {
					continue
				}
				seen[id.name] = true
				report(pass, h.at(id.off, len(id.name)), "W0101", "rename one, or read the field through the receiver the struct's methods name: x."+id.name,
					"%s is %s's field or method and a name of the package; the template reads the field", id.name, typ)
			}
		}
	}
}
