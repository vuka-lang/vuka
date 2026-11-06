package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// What a component takes, read from its source: its props, the values a
// variant prop takes, its slots and events. vuka ui list <component> prints
// it, and the editor's hover on the component's tag shows it.

// propValues fills in the values of info's props that list them: the
// constants of a prop's type declared in the package at dir, or the values
// of the ui.Variants variant its attribute names.
func propValues(dir string, info *tagInfo) {
	var srcs [][]byte
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_templ.go") {
			continue
		}
		if slices.ContainsFunc([]string{".go", ".vuka", ".vuka.html", ".templ"}, func(x string) bool { return strings.HasSuffix(n, x) }) {
			if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
				srcs = append(srcs, b)
			}
		}
	}
	variants := map[string][]string{}
	for _, src := range srcs {
		for name, vs := range variantsIn(src) {
			variants[strings.ToLower(name)] = vs
		}
	}
	for i := range info.props {
		pr := &info.props[i]
		if vs, ok := variants[strings.ToLower(pr.attr)]; ok {
			pr.values = vs
			continue
		}
		typ := strings.TrimPrefix(pr.typ, "*")
		if !token.IsIdentifier(typ) || builtinTypes[typ] {
			continue
		}
		for _, src := range srcs {
			pr.values = append(pr.values, constValues(src, typ)...)
		}
	}
}

var builtinTypes = map[string]bool{"string": true, "bool": true, "int": true, "int64": true, "float64": true, "any": true, "error": true, "byte": true, "rune": true}

// constValueRe is a constant declared with a type: Name Type = value.
var constValueRe = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Z]\w*)\s+(\w+)\s*=\s*("(?:[^"\\\n]|\\.)*"|-?\d+|\w+)`)

// constValues are the constants of type typ src declares, as Name ("value").
func constValues(src []byte, typ string) []string {
	var out []string
	for _, m := range constValueRe.FindAllSubmatch(src, -1) {
		if string(m[2]) == typ {
			out = append(out, fmt.Sprintf("%s (%s)", m[1], m[3]))
		}
	}
	return out
}

// variantsIn are the variants of the ui.Variants literals src holds: a
// variant's name → its values, in order.
func variantsIn(src []byte) map[string][]string {
	out := map[string][]string{}
	for _, m := range variantsLit.FindAllIndex(src, -1) {
		end := skipGo(src, m[1]-1)
		if end < 0 {
			continue
		}
		lit, err := parser.ParseExpr("ui.Variants" + string(src[m[1]-1:end]))
		if err != nil {
			continue
		}
		cl, ok := lit.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if k, isID := kv.Key.(*ast.Ident); !ok || !isID || k.Name != "Variants" {
				continue
			}
			byName, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, e := range byName.Elts {
				v, ok := e.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, err := strconv.Unquote(litText(v.Key))
				values, isLit := v.Value.(*ast.CompositeLit)
				if err != nil || !isLit {
					continue
				}
				var vs []string
				for _, ve := range values.Elts {
					if pair, ok := ve.(*ast.KeyValueExpr); ok {
						if s, err := strconv.Unquote(litText(pair.Key)); err == nil {
							vs = append(vs, strconv.Quote(s))
						}
					}
				}
				out[name] = vs
			}
		}
	}
	return out
}

var variantsLit = regexp.MustCompile(`\bui\.Variants\s*\{`)

func litText(e ast.Expr) string {
	if b, ok := e.(*ast.BasicLit); ok {
		return b.Value
	}
	return ""
}

// note is what a prop's row says beside its type: required, the values
// it takes, an event.
func (pr tagProp) note() string {
	var parts []string
	if pr.required {
		parts = append(parts, "required")
	}
	if isEventProp(pr) {
		parts = append(parts, "event")
	}
	if len(pr.values) > 0 {
		parts = append(parts, strings.Join(pr.values, ", "))
	}
	return strings.Join(parts, "; ")
}

// isEventProp is whether a prop is an event's: on, or on and a capital
// (onClick, onClose), taking a handler rather than a plain value.
func isEventProp(pr tagProp) bool {
	return (pr.attr == "on" || isEventAttr(pr.attr)) && (pr.typ == "any" || !builtinTypes[pr.typ])
}

// slotNames are the component's slots, written as filled: children, then
// <slot:name>.
func (info *tagInfo) slotNames() []string {
	var out []string
	if info.children {
		out = append(out, "children")
	}
	for _, pr := range info.props {
		switch {
		case pr.kind != 0:
			out = append(out, "`<slot:"+slotName(pr)+">`")
		case pr.slot:
			out = append(out, "`<slot:"+lowerCamel(pr.attr)+">`")
		}
	}
	return slices.Compact(out)
}

// events are the attributes of the component's events.
func (info *tagInfo) events() []string {
	var out []string
	for _, pr := range info.props {
		if isEventProp(pr) {
			out = append(out, pr.attr)
		}
	}
	return out
}

// text is the component as vuka ui list prints it.
func (info *tagInfo) text(tag string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<%s>  %s, %s:%d\n", tag, info.kind, filepath.Base(info.file), info.line)
	var rows [][3]string
	for _, pr := range info.props {
		if !pr.slot {
			rows = append(rows, [3]string{pr.attr, pr.typ, pr.note()})
		}
	}
	if len(rows) == 0 {
		b.WriteString("  no attributes\n")
	}
	wa, wt := 0, 0
	for _, r := range rows {
		wa, wt = max(wa, len(r[0])), max(wt, len(r[1]))
	}
	pkg, _, _ := strings.Cut(tag, ".")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\n", strings.TrimRight(fmt.Sprintf("  %-*s  %-*s  %s", wa, r[0], wt, r[1], qualifyValues(r[2], pkg)), " "))
	}
	for _, pr := range info.props {
		if pr.doc != "" && !pr.slot {
			fmt.Fprintf(&b, "  %s: %s\n", pr.attr, strings.Join(strings.Fields(pr.doc), " "))
		}
	}
	if slots := info.slotNames(); len(slots) > 0 {
		fmt.Fprintf(&b, "  slots: %s\n", strings.ReplaceAll(strings.Join(slots, ", "), "`", ""))
	}
	if ev := info.events(); len(ev) > 0 {
		fmt.Fprintf(&b, "  events: %s\n", strings.Join(ev, ", "))
	}
	return b.String()
}

// qualifyValues is a prop's note with the constants it names qualified by
// the package the tag names: design.ButtonOutline ("outline").
func qualifyValues(note, pkg string) string {
	if pkg == "" {
		return note
	}
	return constName.ReplaceAllString(note, "${1}"+pkg+".${2}")
}

var constName = regexp.MustCompile(`(^|; |, )([A-Z]\w* \()`)

// componentsIn are the components declared in the package at dir, in the
// order its files declare them: a .vuka.html file's struct, a .templ
// file's exported templ.
func componentsIn(dir string) []*tagInfo {
	entries, _ := os.ReadDir(dir)
	var out []*tagInfo
	for _, e := range entries {
		n := e.Name()
		src, err := os.ReadFile(filepath.Join(dir, n))
		if e.IsDir() || err != nil {
			continue
		}
		var names []string
		switch {
		case strings.HasSuffix(n, ".vuka.html"):
			if sh, _, _ := shapeBoundsFile(n, src); sh != nil && sh.Type != "" {
				names = append(names, sh.Type)
			}
		case strings.HasSuffix(n, ".templ"):
			for _, m := range templDeclRe.FindAllSubmatch(src, -1) {
				if token.IsExported(string(m[1])) {
					names = append(names, string(m[1]))
				}
			}
		}
		for _, name := range names {
			if info := tagComponentIn(dir, name); info != nil {
				(*proxy)(nil).fillSlots(info) // slots of the package's own types
				propValues(dir, info)
				out = append(out, info)
			}
		}
	}
	return out
}

// uiDoc is vuka ui list <component> and <library>/<component>: what the
// component takes, from the registry's source, or the library's at the
// version the app has it at (else the one vuka ui add takes).
func uiDoc(root string, f *uiFlags, cfg uiConfig, arg string, w io.Writer) error {
	tmp, err := os.MkdirTemp("", "vuka-ui-doc-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var dir, pkg, header string
	if lib, name, ok := strings.Cut(arg, "/"); ok {
		lc, err := readUILib(root)
		if err != nil {
			return err
		}
		spec, err := lc.library(lib)
		if err != nil {
			return err
		}
		version := ""
		if s, ok := lc.sources[name]; ok && libOf(s) == lib {
			version = versionOf(s)
		}
		src, err := openLibrary(root, spec, version)
		if err != nil {
			return err
		}
		all := src.components()
		if !slices.Contains(all, name) {
			return fmt.Errorf("vuka ui list: %s has no component %q%s; its components are %s", src.label(), name, suggest(name, all, ""), strings.Join(all, ", "))
		}
		dir, pkg = filepath.Join(src.dir, filepath.FromSlash(src.spec.Components), name), name
		header = fmt.Sprintf("%s/%s, from %s\n", lib, name, src.label())
	} else {
		reg, err := openRegistry(f, cfg, root)
		if err != nil {
			return err
		}
		items, err := reg.resolve("list", []string{arg})
		if err != nil {
			return err
		}
		if items, err = reg.fetch(items); err != nil {
			return err
		}
		it := items[len(items)-1]
		for _, file := range it.Files {
			b, err := reg.file(file)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(tmp, path.Base(file.Path)), b, 0o644); err != nil {
				return err
			}
		}
		dir, pkg = tmp, reg.Package
		if cfg.Package != "" {
			pkg = cfg.Package
		}
		header = fmt.Sprintf("%s: %s\n", it.Title, sentence(it.Description))
	}
	comps := componentsIn(dir)
	if len(comps) == 0 {
		return fmt.Errorf("vuka ui list: %s declares no component", arg)
	}
	fmt.Fprint(w, header)
	for _, c := range comps {
		fmt.Fprintf(w, "\n%s", c.text(pkg+"."+c.name))
	}
	return nil
}

// sentence is a description as a sentence: a capital and a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
