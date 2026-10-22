package main

import (
	"bytes"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/format"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// Live pages of earlier releases are configured by field tags (bind:, url:,
// defer:, validate:) and hooks found by name (Mount, Changed, Load<Group>).
// The tags still work for one minor release: vuka vet warns (W0302) and vuka
// fix attributes rewrites them as attributes. A hook is declared only with
// its attribute (@ui.Mount, @ui.OnChange, @ui.Loads): a method found by its
// name is an error (E0337, the compiler's for Mount and Changed; vuka vet's
// for a loader), and vuka fix hooks marks the methods.

// liveTags are the field tags a live page's fields were configured by.
var liveTags = []string{"bind", "url", "defer", "validate"}

// liveType is a struct type of a .vuka file embedding ui.Live (or, for
// structTypes, any struct type).
type liveType struct {
	name, ui string // ui: the name ui is imported as, the embed's
	orm      string // the package of an embedded Base or Model[T]: an orm model's
	fields   []liveField
}

// liveField is one field of a live type as the source writes it.
type liveField struct {
	names            []string
	off              int // the first name
	tag              string
	tagStart, tagEnd int // the tag literal; -1 without one
	typeEnd          int // where the field's type ends
}

// liveTypes are the struct types of a .vuka file's source embedding Live.
func liveTypes(src []byte) []liveType {
	var out []liveType
	for _, lt := range structTypes(src) {
		if lt.ui != "" {
			out = append(out, lt)
		}
	}
	return out
}

// structTypes are the named struct types of a .vuka file's source.
func structTypes(src []byte) []liveType {
	toks, _ := scanFile("", src)
	var out []liveType
	depth := 0
	for i := 0; i+2 < len(toks); i++ {
		switch toks[i].tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
		}
		if depth > 1 || toks[i].tok != token.IDENT || toks[i+1].tok != token.STRUCT || toks[i+2].tok != token.LBRACE {
			continue
		}
		lt := liveType{name: toks[i].lit}
		d, start := 0, i+3
		for j := i + 2; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE, token.LPAREN, token.LBRACK:
				d++
			case token.RBRACE, token.RPAREN, token.RBRACK:
				d--
			}
			end := d == 0 || d == 1 && toks[j].tok == token.SEMICOLON
			if !end {
				continue
			}
			lt.field(toks[start:j])
			start = j + 1
			if d == 0 {
				break
			}
		}
		out = append(out, lt)
	}
	return out
}

func (lt *liveType) field(toks []wtok) {
	for i, t := range toks {
		if t.tok == token.COMMENT || t.tok == token.ILLEGAL && t.lit == "@" {
			toks = toks[:i]
			break
		}
	}
	if len(toks) == 0 {
		return
	}
	f := liveField{tagStart: -1, tagEnd: -1}
	if n := len(toks); toks[n-1].tok == token.STRING {
		f.tag, _ = strconv.Unquote(toks[n-1].lit)
		f.tagStart, f.tagEnd = toks[n-1].off, toks[n-1].off+len(toks[n-1].lit)
		toks = toks[:n-1]
	}
	if len(toks) == 0 {
		return
	}
	names, typ := splitNamesType(toks)
	if len(names) == 0 {
		if len(typ) >= 3 && typ[0].tok == token.IDENT && typ[1].tok == token.PERIOD {
			switch {
			case len(typ) == 3 && typ[2].lit == "Live":
				lt.ui = typ[0].lit
			case len(typ) == 3 && typ[2].lit == "Base", typ[2].lit == "Model" && len(typ) > 3 && typ[3].tok == token.LBRACK:
				lt.orm = typ[0].lit
			}
		}
		return
	}
	last := typ[len(typ)-1]
	f.names, f.off, f.typeEnd = names, toks[0].off, last.off+len(last.lit)
	if last.lit == "" {
		f.typeEnd = last.off + len(last.tok.String())
	}
	lt.fields = append(lt.fields, f)
}

// hookMethod matches a method of a live page's that earlier releases found
// a hook by: Mount, Changed, Load<Group>.
var hookMethod = regexp.MustCompile(`(?m)^func\s*\(\s*\w*\s*\*?\s*(\w+)\s*\)\s*(Mount|Changed|Load[A-Z]\w*)\s*\(([^)]*)\)\s*([^{]*)\{`)

// namedHook is a method earlier releases found a live page's hook by.
type namedHook struct {
	typ, name, params, results string
	off                        int // the func keyword
	hooked                     bool
}

// namedHooks are the methods src declares that earlier releases found hooks
// by, hooked when a hook attribute is written above.
func namedHooks(src []byte) []namedHook {
	var out []namedHook
	for _, m := range hookMethod.FindAllSubmatchIndex(src, -1) {
		h := namedHook{typ: string(src[m[2]:m[3]]), name: string(src[m[4]:m[5]]), params: strings.TrimSpace(string(src[m[6]:m[7]])),
			results: strings.TrimSpace(string(src[m[8]:m[9]])), off: m[0]}
		for _, a := range attrsAbove(src, m[0]) {
			_, sel, _ := strings.Cut(a, ".")
			h.hooked = h.hooked || sel == "Loads" || sel == "Mount" || sel == "OnChange"
		}
		out = append(out, h)
	}
	return out
}

// attrsAbove are the names of the attributes on the lines right above off.
func attrsAbove(src []byte, off int) []string {
	var out []string
	end := off
	for end > 0 {
		start := bytes.LastIndexByte(src[:end-1], '\n') + 1
		line := strings.TrimSpace(string(src[start : end-1]))
		if !strings.HasPrefix(line, "@") {
			break
		}
		name := strings.FieldsFunc(line[1:], func(r rune) bool { return r == '(' || r == '{' || r == ' ' })
		if len(name) > 0 {
			out = append(out, name[0])
		}
		end = start
	}
	return out
}

// deferAttr and loadsAttr match the attributes deferring a field and
// loading its group: @ui.Defer("lists"), @ui.Loads("lists").
var (
	deferAttr = regexp.MustCompile(`@\w+\.Defer\("(\w+)"\)`)
	loadsAttr = regexp.MustCompile(`@\w+\.Loads\("(\w+)"\)`)
)

// attrGroups are the groups src's attributes match, as loaders name them.
func attrGroups(src []byte, re *regexp.Regexp, into map[string]bool) {
	for _, m := range re.FindAllSubmatch(src, -1) {
		into[groupOf(string(m[1]), "")] = true
	}
}

// deferredGroups are the groups a package's live pages defer, by
// type.group, from their tags and attributes.
func deferredGroups(srcs [][]byte) (live map[string]string, deferred map[string]bool) {
	live, deferred = map[string]string{}, map[string]bool{}
	for _, src := range srcs {
		for _, lt := range liveTypes(src) {
			live[lt.name] = lt.ui
			for _, lf := range lt.fields {
				if v, ok := reflect.StructTag(lf.tag).Lookup("defer"); ok && v != "-" {
					deferred[lt.name+"."+groupOf(v, lf.names[0])] = true
				}
			}
			groups := map[string]bool{}
			attrGroups(src, deferAttr, groups)
			for g := range groups {
				deferred[lt.name+"."+g] = true
			}
		}
	}
	return live, deferred
}

// vukaSources are a package's .vuka files' paths and sources, as on disk.
func vukaSources(p *load.Package) (paths []string, srcs [][]byte) {
	for _, f := range p.Files {
		if !f.IsVuka() {
			continue
		}
		path := filepath.Join(p.Dir, f.Name)
		src, err := os.ReadFile(path)
		if err != nil {
			src = f.Src
		}
		paths, srcs = append(paths, path), append(srcs, src)
	}
	return paths, srcs
}

// vetLivePages warns of the module's live pages configured as earlier
// releases did.
func vetLivePages(root string, pkgs []*load.Package) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, p := range pkgs {
		paths, srcs := vukaSources(p)
		live, deferred := deferredGroups(srcs)
		for i, src := range srcs {
			file := relPath(root, paths[i])
			_, line := scanFile(file, src)
			for _, lt := range liveTypes(src) {
				for _, lf := range lt.fields {
					var used []string
					for _, k := range liveTags {
						if _, ok := reflect.StructTag(lf.tag).Lookup(k); ok {
							used = append(used, k+":")
						}
					}
					if len(used) > 0 {
						out = append(out, *diag.Warn("W0302", "%s.%s is configured by the tag %s, the form of earlier releases", lt.name, lf.names[0], strings.Join(used, ", ")).
							At(span(file, src, line, lf.off, len(lf.names[0]))).WithHelp("write the tags as attributes: vuka fix attributes"))
					}
				}
			}
			for _, h := range namedHooks(src) {
				if _, ok := live[h.typ]; !ok || h.hooked || !strings.HasPrefix(h.name, "Load") || !deferred[h.typ+"."+strings.TrimPrefix(h.name, "Load")] {
					continue
				}
				g := strings.TrimPrefix(h.name, "Load")
				out = append(out, *diag.New("E0337", "%s.%s isn't a hook: a loader is declared with @ui.Loads(%q), never found by its name", h.typ, h.name, strings.ToLower(g[:1])+g[1:]).
					At(span(file, src, line, h.off, len("func"))).WithHelp("mark it with its group's attribute: vuka fix hooks"))
			}
		}
	}
	return out
}

func span(file string, src []byte, line func(int) int, off, n int) diag.Span {
	ls := bytes.LastIndexByte(src[:off], '\n') + 1
	return diag.Span{File: file, Line: line(off), Col: off - ls + 1, EndCol: off - ls + 1 + n}
}

// groupOf is a defer: tag's group as its loader names it: defer:"lists" is
// Lists, defer:"" the field's own name.
func groupOf(tag, field string) string {
	if tag == "" {
		tag = field
	}
	return strings.ToUpper(tag[:1]) + tag[1:]
}

const validatePath = "github.com/vuka-lang/ui/v2/validate"

// fixAttributes rewrites a live page's field tags as attributes.
func fixAttributes(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	for _, p := range m.pkgs {
		paths, srcs := vukaSources(p)
		for i, path := range paths {
			src, n, err := attributeTags(srcs[i])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", relPath(m.root, path), err)
			}
			if n == 0 {
				continue
			}
			changes = append(changes, fixChange{fmt.Sprintf("%s: %d field tag(s) as attributes", relPath(m.root, path), n), func() error {
				return os.WriteFile(path, src, 0o644)
			}})
		}
	}
	return changes, nil
}

// attributeTags is src with its live pages' and models' field tags as
// attributes, and how many fields changed.
func attributeTags(src []byte) ([]byte, int, error) {
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	needTime, needValidate := false, false
	orm := importedAs(src, ormModule)
	for _, lt := range structTypes(src) {
		if lt.ui == "" && (lt.orm == "" || lt.orm != orm) {
			continue
		}
		for _, lf := range lt.fields {
			if lf.tagStart < 0 {
				continue
			}
			var attrs []string
			var rest string
			var usesTime, usesValidate bool
			var err error
			if lt.ui != "" {
				attrs, rest, usesTime, usesValidate, err = tagAttrs(lf.tag, lt.ui, lf.names[0])
			} else {
				attrs, rest = ormTagAttrs(lf.tag, orm)
			}
			if err != nil {
				return nil, 0, fmt.Errorf("%s.%s: %w", lt.name, lf.names[0], err)
			}
			if len(attrs) == 0 {
				continue
			}
			needTime, needValidate = needTime || usesTime, needValidate || usesValidate
			text := " " + strings.Join(attrs, " ")
			if rest == "" {
				edits = append(edits, edit{lf.typeEnd, lf.tagEnd, text})
			} else {
				edits = append(edits, edit{lf.tagStart, lf.tagEnd, "`" + rest + "`" + text})
			}
		}
	}
	if len(edits) == 0 {
		return src, 0, nil
	}
	var b bytes.Buffer
	last := 0
	for _, e := range edits {
		b.Write(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.Write(src[last:])
	out := b.Bytes()
	if needTime {
		out = ensureImport(out, "time")
	}
	if needValidate {
		out = ensureImport(out, validatePath)
	}
	if f, err := format.Source(out); err == nil {
		out = f
	}
	return out, len(edits), nil
}

// tagAttrs are the attributes a field's live tags say, and the rest of
// the tag.
func tagAttrs(tag, ui, field string) (attrs []string, rest string, usesTime, usesValidate bool, err error) {
	var keep []string
	for _, kv := range tagPairs(tag) {
		v := kv[1]
		switch kv[0] {
		case "bind":
			if v == "-" {
				continue
			}
			_, opts, _ := strings.Cut(v, ",")
			a := "@" + ui + ".Bind"
			for _, o := range strings.Split(opts, ",") {
				k, d, _ := strings.Cut(strings.TrimSpace(o), "=")
				if k != "debounce" {
					continue
				}
				expr, ok := durationExpr(d)
				if !ok {
					return nil, "", false, false, fmt.Errorf("bind:%q: bad debounce", v)
				}
				if expr == "-1" {
					expr = ui + ".NoDebounce"
				}
				a += "{Debounce: " + expr + "}"
				usesTime = usesTime || strings.Contains(expr, "time.")
			}
			attrs = append(attrs, a)
		case "url":
			if q, _, _ := strings.Cut(v, ","); q != "" && q != "-" {
				attrs = append(attrs, "@"+ui+".URL("+strconv.Quote(q)+")")
			}
		case "defer":
			if v == "-" {
				continue
			}
			if v == "" {
				v = field
			}
			attrs = append(attrs, "@"+ui+".Defer("+strconv.Quote(v)+")")
		case "validate":
			for _, r := range strings.Split(v, ",") {
				name, param, _ := strings.Cut(strings.TrimSpace(r), "=")
				switch name {
				case "":
				case "required", "omitempty", "email":
					attrs = append(attrs, "@validate."+map[string]string{"required": "Required", "omitempty": "OmitEmpty", "email": "Email"}[name])
				case "min", "max", "len":
					attrs = append(attrs, "@validate."+strings.ToUpper(name[:1])+name[1:]+"("+param+")")
				case "oneof":
					var vs []string
					for _, s := range strings.FieldsFunc(param, func(c rune) bool { return c == ' ' || c == '|' }) {
						vs = append(vs, strconv.Quote(s))
					}
					attrs = append(attrs, "@validate.OneOf{"+strings.Join(vs, ", ")+"}")
				default:
					return nil, "", false, false, fmt.Errorf("validate:%q: no rule %q", v, name)
				}
				usesValidate = true
			}
		default:
			keep = append(keep, kv[0]+":"+strconv.Quote(v))
		}
	}
	return attrs, strings.Join(keep, " "), usesTime, usesValidate, nil
}

// tagPairs are a struct tag's key:"value" pairs, in order.
func tagPairs(tag string) [][2]string {
	var out [][2]string
	for tag = strings.TrimSpace(tag); tag != ""; tag = strings.TrimSpace(tag) {
		k, rest, ok := strings.Cut(tag, ":")
		if !ok || !strings.HasPrefix(rest, `"`) {
			break
		}
		v, err := strconv.QuotedPrefix(rest)
		if err != nil {
			break
		}
		s, _ := strconv.Unquote(v)
		out = append(out, [2]string{k, s})
		tag = rest[len(v):]
	}
	return out
}

// durationExpr writes a debounce of a bind: tag (milliseconds, or a
// duration) as Go: 500 * time.Millisecond, time.Second; -1 for none.
func durationExpr(v string) (string, bool) {
	d, err := time.ParseDuration(v)
	if n, e := strconv.Atoi(v); e == nil {
		d, err = time.Duration(n)*time.Millisecond, nil
	}
	switch {
	case err != nil || d < 0:
		return "", false
	case d == 0:
		return "-1", true
	case d%time.Second == 0 && d == time.Second:
		return "time.Second", true
	case d%time.Second == 0:
		return strconv.Itoa(int(d/time.Second)) + " * time.Second", true
	}
	return strconv.Itoa(int(d/time.Millisecond)) + " * time.Millisecond", true
}

// ensureImport imports path into a Vuka source that doesn't yet: into its
// import block when it has one.
func ensureImport(src []byte, path string) []byte {
	q := strconv.Quote(path)
	if bytes.Contains(src, []byte(q)) {
		return src
	}
	if i := bytes.Index(src, []byte("\nimport (\n")); i >= 0 {
		at := i + len("\nimport (\n")
		if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			at += bytes.Index(src[at:], []byte("\n)")) + 1 // after the last group: the module's
		}
		return append(append(append([]byte{}, src[:at]...), "\t"+q+"\n"...), src[at:]...)
	}
	if out, err := addImport(src, "", path); err == nil {
		return out
	}
	return src
}

// fixHooks marks the methods earlier releases found a live page's hooks by
// with the hooks' attributes, when their signatures are the hooks'.
func fixHooks(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	for _, p := range m.pkgs {
		paths, srcs := vukaSources(p)
		live, deferred := deferredGroups(srcs)
		for i, path := range paths {
			src, marked := markHooks(srcs[i], live, deferred)
			if len(marked) == 0 {
				continue
			}
			changes = append(changes, fixChange{relPath(m.root, path) + ": " + strings.Join(marked, ", ") + " marked as hooks", func() error {
				return os.WriteFile(path, src, 0o644)
			}})
		}
	}
	return changes, nil
}

var ctxParam = regexp.MustCompile(`^\w+\s+context\.Context$`)

var ctxChangedParams = regexp.MustCompile(`^\w+\s+context\.Context\s*,\s*\w+\s+\w+\.Changed$`)

// markHooks is src with a hook attribute above each method earlier releases
// found a hook by — @ui.Mount on Mount, @ui.OnChange on Changed,
// @ui.Loads("group") on a deferred group's Load<Group> — whose signature
// is the hook's, and the methods marked.
func markHooks(src []byte, live map[string]string, deferred map[string]bool) ([]byte, []string) {
	var b bytes.Buffer
	var marked []string
	last := 0
	for _, h := range namedHooks(src) {
		ui, ok := live[h.typ]
		if !ok || h.hooked || h.results != "error" {
			continue
		}
		var attr string
		switch {
		case h.name == "Mount" && ctxParam.MatchString(h.params):
			attr = "@" + ui + ".Mount"
		case h.name == "Changed" && ctxChangedParams.MatchString(h.params):
			attr = "@" + ui + ".OnChange"
		case strings.HasPrefix(h.name, "Load") && ctxParam.MatchString(h.params) && deferred[h.typ+"."+strings.TrimPrefix(h.name, "Load")]:
			g := strings.TrimPrefix(h.name, "Load")
			attr = "@" + ui + ".Loads(" + strconv.Quote(strings.ToLower(g[:1])+g[1:]) + ")"
		default:
			continue
		}
		b.Write(src[last:h.off])
		b.WriteString(attr + "\n")
		last = h.off
		marked = append(marked, h.typ+"."+h.name)
	}
	b.Write(src[last:])
	return b.Bytes(), marked
}
