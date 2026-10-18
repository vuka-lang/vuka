package main

import (
	"bytes"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/diag"
)

// vetIslands is what vuka vet says of the module's islands: a component
// file missing or ambiguous, a live page routed as a stateless one, and
// what ui.CheckIsland would refuse at boot that the source shows already.
func vetIslands(root, modPath string, pa *pageAssets) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, ic := range pa.islands {
		if d := ic.diagnostic(); d != nil {
			out = append(out, *d)
		}
		if a := ic.routeAdvice(); a != "" {
			out = append(out, *diag.Warn("W0301", "%s", a).At(labelSpan(ic.routeLabel)).WithSuggest("@web.Live"))
		}
		if !ic.imported && (ic.route != "" || ic.live) {
			out = append(out, checkIslandSource(root, ic, nil)...)
		}
	}
	return out
}

// islandField is a field of an island's struct as the source writes it.
type islandField struct {
	name, wire, tag string
	value, exported bool
	off             int
}

// checkIslandSource is ui.CheckIsland's checks of a page's struct that need
// only its source: its fields' bind:, defer: and url: tags, their handles,
// and the deferred groups' loaders.
func checkIslandSource(root string, ic *islandComp, src []byte) []diag.Diagnostic {
	file := labelSpan(ic.label).File
	if src == nil {
		var err error
		if src, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(file))); err != nil {
			return nil
		}
	}
	full, line := scanFile(file, src)
	fields, embedded, ok := sourceFields(full, ic.name)
	if !ok {
		return nil
	}
	at := func(off int, n int) diag.Span {
		ls := bytes.LastIndexByte(src[:off], '\n') + 1
		return diag.Span{File: file, Line: line(off), Col: off - ls + 1, EndCol: off - ls + 1 + n}
	}
	methods := sourceMethods(root, path.Dir(file), ic.name)
	var out []diag.Diagnostic
	add := func(d *diag.Diagnostic) { out = append(out, *d) }
	handles := map[string]string{}
	groups := map[string]islandField{}
	var order []string
	for _, f := range fields {
		bind, hasBind := reflect.StructTag(f.tag).Lookup("bind")
		hasBind = hasBind && bind != "-"
		span := at(f.off, len(f.name))
		if !f.exported || f.wire == "-" || !f.value {
			if hasBind {
				add(diag.New("E0322", "%s.%s: bind: is for a prop the component sets (an exported field of a JSON value)", ic.name, f.name).At(span).
					WithHelp("drop bind:, or make %s an exported field of a plain value", f.name))
			}
			continue
		}
		if v, ok := reflect.StructTag(f.tag).Lookup("validate"); ok {
			if msg, help := checkValidateTag(v); msg != "" {
				add(diag.New("E0328", "%s.%s: validate:%q: %s", ic.name, f.name, v, msg).At(span).WithHelp("%s", help))
			}
		}
		handle := strings.ToLower(f.wire[:1]) + f.wire[1:]
		if other, ok := handles[handle]; ok {
			add(diag.New("E0326", "%s: fields %s and %s are both live.%s", ic.name, other, f.name, handle).At(span).
				WithHelp("give one of them another json name"))
		}
		handles[handle] = f.name
		if d, ok := reflect.StructTag(f.tag).Lookup("defer"); ok && d != "-" {
			if d == "" {
				d = f.name
			}
			switch q, _, _ := strings.Cut(reflect.StructTag(f.tag).Get("url"), ","); {
			case !isGoIdent(d):
				add(diag.New("E0323", "%s.%s: defer:%q: a group is a name", ic.name, f.name, d).At(span).
					WithHelp("name the group: defer:\"stats\", loaded by LoadStats"))
			case hasBind:
				add(diag.New("E0323", "%s.%s: a deferred field is the server's to load", ic.name, f.name).At(span).WithHelp("drop bind: or defer:"))
			case q != "" && q != "-":
				add(diag.New("E0323", "%s.%s: a deferred field is the server's to load", ic.name, f.name).At(span).WithHelp("drop url: or defer:"))
			default:
				g := strings.ToUpper(d[:1]) + d[1:]
				if _, seen := groups[g]; !seen {
					groups[g] = f
					order = append(order, g)
				}
			}
		}
		if hasBind {
			n, opts, _ := strings.Cut(bind, ",")
			switch {
			case n != "" && n != handle && n != f.wire:
				add(diag.New("E0322", "%s.%s: bind:%q names the field live.%s", ic.name, f.name, n, handle).At(span).
					WithSuggest(strconv.Quote(handle)).WithHelp("write bind:%q, or leave the name out: bind:\"\"", handle))
			case handle == "pending" || handle == "error" || handle == "subscribe":
				add(diag.New("E0322", "%s.%s: live.%s is live's own", ic.name, f.name, handle).At(span).
					WithHelp("give the field another json name to bind it"))
			}
			for _, o := range strings.Split(opts, ",") {
				k, v, _ := strings.Cut(strings.TrimSpace(o), "=")
				switch k {
				case "":
				case "debounce":
					if !validDebounce(v) {
						add(diag.New("E0322", "%s.%s: bind:%q: bad debounce %q", ic.name, f.name, bind, v).At(span).
							WithHelp("debounce is milliseconds (debounce=500) or a duration (debounce=1s)"))
					}
				default:
					add(diag.New("E0322", "%s.%s: bind:%q: unknown option %q", ic.name, f.name, bind, k).At(span).
						WithSuggest("debounce=500"))
				}
			}
		}
	}
	for _, g := range order {
		f := groups[g]
		m, ok := methods["Load"+g]
		switch {
		case !ok && embedded:
			// a loader may come from an embedded type
		case !ok:
			recv := strings.ToLower(ic.name[:1])
			add(diag.New("E0324", "%s.%s is deferred, and the group %s has no loader", ic.name, f.name, g).
				At(at(f.off, len(f.name))).WithHelp("add the method\nfunc (%s *%s) Load%s(ctx context.Context) error", recv, ic.name, g))
		case !m.loader():
			add(diag.New("E0325", "%s.Load%s is the loader of the deferred group %s, and it isn't Load%s() or Load%s(ctx context.Context) returning nothing or an error",
				ic.name, g, g, g, g).At(diag.Span{File: m.file, Line: m.line}))
		}
	}
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := methods[name]
		if !m.callable() || strings.HasPrefix(name, "Load") && groups[strings.TrimPrefix(name, "Load")].name != "" {
			continue
		}
		h := strings.ToLower(name[:1]) + name[1:]
		if f, ok := handles[h]; ok {
			add(diag.New("E0326", "%s: the method %s and the field %s are both live.%s", ic.name, name, f, h).
				At(diag.Span{File: m.file, Line: m.line}).WithHelp("rename one, or give the field another json name"))
		}
	}
	return out
}

// sourceFields are the fields of the struct type name declares in full;
// embedded reports a field embedding a type other than ui.Live.
func sourceFields(full []wtok, name string) (fields []islandField, embedded, ok bool) {
	open := -1
	for i := 0; i+3 < len(full); i++ {
		if full[i].tok == token.TYPE && full[i+1].lit == name && full[i+2].tok == token.STRUCT && full[i+3].tok == token.LBRACE {
			open = i + 3
			break
		}
	}
	if open < 0 {
		return nil, false, false
	}
	depth, start := 0, open+1
	field := func(toks []wtok) {
		for len(toks) > 0 && toks[0].tok == token.SEMICOLON {
			toks = toks[1:]
		}
		if len(toks) == 0 {
			return
		}
		tag := ""
		if n := len(toks); toks[n-1].tok == token.STRING {
			tag, _ = strconv.Unquote(toks[n-1].lit)
			toks = toks[:n-1]
		}
		names, typ := splitNamesType(toks)
		if len(names) == 0 {
			if !(len(typ) == 3 && typ[2].lit == "Live") {
				embedded = true
			}
			return
		}
		wire, skip := "", false
		if len(names) == 1 {
			wire, skip = jsonTagName(tag)
		}
		for k, nm := range names {
			if nm == "Children" {
				continue
			}
			f := islandField{name: nm, wire: nm, tag: tag, value: plainType(typ), exported: token.IsExported(nm), off: toks[2*k].off}
			if skip {
				f.wire = "-"
			} else if wire != "" {
				f.wire = wire
			}
			fields = append(fields, f)
		}
	}
	for i := open; i < len(full); i++ {
		switch full[i].tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			if depth--; depth == 0 {
				field(full[start:i])
				return fields, embedded, true
			}
		case token.SEMICOLON:
			if depth == 1 {
				field(full[start:i])
				start = i + 1
			}
		}
	}
	return fields, embedded, true
}

type sourceMethod struct {
	params, results string
	file            string
	line            int
}

func (m sourceMethod) results0() bool {
	r := strings.TrimSpace(m.results)
	return r == "" || r == "error"
}

func (m sourceMethod) loader() bool {
	p := strings.TrimSpace(m.params)
	if p != "" {
		f := strings.Fields(p)
		if len(f) == 0 || f[len(f)-1] != "context.Context" || strings.Contains(p, ",") {
			return false
		}
	}
	return m.results0()
}

func (m sourceMethod) callable() bool { return m.results0() && !strings.Contains(m.params, "...") }

var anyMethodRe = regexp.MustCompile(`(?m)^func\s*\(\s*\w*\s*\*?\s*(\w+)\s*\)\s*([A-Z]\w*)\s*\(([^)]*)\)\s*([^{]*)\{`)

// sourceMethods are the exported methods of type name in the package dir's
// sources, the lifecycle's left out.
func sourceMethods(root, relDir, name string) map[string]sourceMethod {
	out := map[string]sourceMethod{}
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".go")) || strings.Contains(n, "_test.") || strings.HasSuffix(n, "_vuka.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		rel := path.Join(relDir, n)
		for _, m := range anyMethodRe.FindAllStringSubmatchIndex(string(src), -1) {
			if string(src[m[2]:m[3]]) != name {
				continue
			}
			mn := string(src[m[4]:m[5]])
			if lifecycleMethods[mn] || strings.HasPrefix(mn, "Update__") {
				continue
			}
			out[mn] = sourceMethod{params: string(src[m[6]:m[7]]), results: string(src[m[8]:m[9]]), file: rel, line: bytes.Count(src[:m[0]], []byte("\n")) + 1}
		}
	}
	return out
}

func validDebounce(v string) bool {
	if n, err := strconv.Atoi(v); err == nil {
		return n >= 0
	}
	d, err := time.ParseDuration(v)
	return err == nil && d >= 0
}

func isGoIdent(s string) bool {
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

// checkValidateTag is what ui's validate: rules refuse of a tag: the problem
// and its fix, "" when none.
func checkValidateTag(tag string) (msg, help string) {
	if tag == "-" {
		return "", ""
	}
	for _, part := range strings.Split(tag, ",") {
		name, param, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch name {
		case "":
		case "required", "omitempty", "email":
			if param != "" {
				return name + " takes no value", "write " + name + " alone"
			}
		case "min", "max", "len":
			if _, err := strconv.ParseFloat(param, 64); err != nil {
				return name + " takes a number", "write " + name + "=3"
			}
		case "oneof":
			if len(strings.FieldsFunc(param, func(c rune) bool { return c == ' ' || c == '|' })) == 0 {
				return "oneof takes values", "write oneof=a b c"
			}
		default:
			return strconv.Quote(name) + " is no rule", "the rules are required, omitempty, min, max, len, oneof and email"
		}
	}
	return "", ""
}
