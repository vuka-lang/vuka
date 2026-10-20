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
		out = append(out, &htmlSource{path: p, src: string(b), file: f})
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
				report(pass, h.at(id.off, len(id.name)), "W0101", "rename one, or write the file's header, func (x "+typ+") …, and x."+id.name,
					"%s is %s's field or method and a name of the package; the template reads the field", id.name, typ)
			}
		}
	}
}

var headerName = regexp.MustCompile(`^func(?:\s*(\([^)]*\))\s*|\s+)(\w+)`)

// elements are the HTML, SVG and MathML element names, as the compiler has
// them (transpile/elements.go): a template can't be named so.
var elements = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`
a abbr acronym address applet area article aside audio b base basefont bdi bdo big blink blockquote
body br button canvas caption center cite code col colgroup data datalist dd del details dfn dialog
dir div dl dt em embed fieldset figcaption figure font footer form frame frameset h1 h2 h3 h4 h5 h6
head header hgroup hr html i iframe image img input ins kbd keygen label legend li link listing main
map mark marquee math menu menuitem meta meter nav nobr noembed noframes noscript object ol optgroup
option output p param picture plaintext portal pre progress q rb rp rt rtc ruby s samp script search
section select selectedcontent slot small source span strike strong style sub summary sup svg table
tbody td template textarea tfoot th thead time title tr track tt u ul var video wbr xmp
animate animateMotion animateTransform circle clipPath defs desc discard ellipse feBlend
feColorMatrix feComponentTransfer feComposite feConvolveMatrix feDiffuseLighting feDisplacementMap
feDistantLight feDropShadow feFlood feFuncA feFuncB feFuncG feFuncR feGaussianBlur feImage feMerge
feMergeNode feMorphology feOffset fePointLight feSpecularLighting feSpotLight feTile feTurbulence
filter foreignObject g line linearGradient marker mask metadata mpath path pattern polygon polyline
radialGradient rect set stop switch symbol text textPath tspan use view
annotation annotation-xml maction menclose merror mfrac mglyph mi mlabeledtr mmultiscripts mn mo
mover mpadded mphantom mprescripts mroot mrow ms mspace msqrt mstyle msub msubsup msup mtable mtd
mtext mtr munder munderover semantics
`) {
		elements[n] = true
	}
}

// templateHeaders reports a template named as an element (E0145), and warns
// where an exported template has no doc comment (W0104), as Go's linters do
// of an exported function; View and Render methods are documented by their
// type.
func templateHeaders(pass *analysis.Pass, files []*htmlSource) {
	for _, h := range files {
		off, prev := 0, ""
		for _, l := range strings.SplitAfter(h.src, "\n") {
			if m := headerName.FindStringSubmatchIndex(l); m != nil {
				name := l[m[4]:m[5]]
				method := m[2] >= 0
				if !method && elements[name] {
					report(pass, h.at(off+m[4], len(name)), "E0145", "rename it: "+name+"View, or a name of what it shows",
						"the template %s has an element's name: <%s> is the element", name, name)
				}
				if unicode.IsUpper([]rune(name)[0]) && !strings.HasPrefix(strings.TrimSpace(prev), "//") && !(method && (name == "View" || name == "Render")) {
					report(pass, h.at(off+m[4], len(name)), "W0104", "say what it shows in a comment right above it: // "+name+" …",
						"the exported template %s has no doc comment", name)
				}
			}
			off, prev = off+len(l), l
		}
	}
}
