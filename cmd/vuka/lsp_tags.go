package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/toolchain/tsprops"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Component tags in shape markup, read from the component's source: a
// struct declared by a .vuka.html file (its props are its exported fields),
// a struct of a .vuka or .go file, or a function (its parameters). Inside
// <pkg.Comp ␣ completion lists the props with their types, required ones
// marked; hover on the tag shows them and links the file. Unknown and missing
// attributes are the compiler's errors (E0144 and "has no attribute"),
// published as the file is typed.

// tagProp is an attribute a component takes.
type tagProp struct {
	attr, typ string
	required  bool
	slot      bool // a Node: written as <slot:name>, not an attribute
	doc       string
	kind      byte     // how a <slot:x> fills it (slotOf), 0 when none does
	params    []string // the types of the values a slot's let names
}

// tagInfo is a component, as its source declares it.
type tagInfo struct {
	name, kind string // kind: live component, component, function
	file       string
	line       int // from 1
	props      []tagProp
}

var (
	embedsLive   = regexp.MustCompile(`(?m)^\s*(?:\w+\.)?Live\s*$`)
	tagPathMajor = regexp.MustCompile(`/v[0-9]+$`)
	tagAttrStart = regexp.MustCompile(`\s@[A-Za-z_]`)
)

// resolveComponent finds the declaration of a component tag used in the
// file at srcPath, nil when it can't.
func (p *proxy) resolveComponent(srcPath string, src []byte, tag string) *tagInfo {
	qual, name := "", tag
	if i := strings.LastIndexByte(tag, '.'); i >= 0 {
		qual, name = tag[:i], tag[i+1:]
	}
	if name == "" || strings.Contains(qual, ".") {
		return nil
	}
	dir := filepath.Dir(srcPath)
	if qual == "" {
		for _, im := range sourceImports(srcPath, src) {
			if im.Name != name || !tsprops.IsComponent(im.Path) {
				continue
			}
			root, _, _ := load.ModuleRoot(dir)
			file, err := tsprops.ResolveIslandPath(root, dir, im.Path)
			if err != nil {
				return nil
			}
			data, err := load.ReadSource(file)
			if err != nil {
				return nil
			}
			comp, err := tsprops.Read(filepath.ToSlash(file), data, func(file string) ([]byte, error) { return load.ReadSource(filepath.FromSlash(file)) })
			if err != nil {
				return nil
			}
			info := &tagInfo{name: name, kind: "island", file: file, line: 1}
			for _, pr := range comp.Props {
				info.props = append(info.props, tagProp{attr: pr.Name, typ: pr.Type.String(), required: !pr.Optional})
			}
			return info
		}
	}
	if qual != "" {
		dir = ""
		for _, im := range sourceImports(srcPath, src) {
			n := im.Name
			if n == "" {
				n = path.Base(tagPathMajor.ReplaceAllString(im.Path, ""))
			}
			if n == qual && !strings.HasPrefix(im.Path, ".") {
				dir = p.importDir(im.Path)
				break
			}
		}
		if dir == "" {
			return nil
		}
	}
	info := tagComponentIn(dir, name)
	if info != nil {
		p.fillSlots(info)
	}
	return info
}

// sourceImports are a .vuka.html or .vuka file's imports.
func sourceImports(srcPath string, src []byte) []transpile.TemplateImport {
	if strings.HasSuffix(srcPath, ".vuka.html") {
		return transpile.ParseTemplateHead(src).Imports
	}
	f, _ := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly)
	if f == nil {
		return nil
	}
	var out []transpile.TemplateImport
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		n := ""
		if im.Name != nil {
			n = im.Name.Name
		}
		out = append(out, transpile.TemplateImport{Name: n, Path: p})
	}
	return out
}

// tagComponentIn finds the component name declared in the package at dir.
func tagComponentIn(dir, name string) *tagInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// .vuka.html first: a shape's struct.
	var others []string
	for _, e := range entries {
		n := e.Name()
		switch {
		case e.IsDir():
		case strings.HasSuffix(n, ".vuka.html"):
			file := filepath.Join(dir, n)
			src, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			if sh, _, _ := shapeBounds(src); sh != nil && sh.Type == name {
				info := &tagInfo{name: name, kind: "component", file: file, line: sh.TypeLine}
				if sh.Live {
					info.kind = "live component"
				}
				for _, f := range sh.Fields {
					if f.Role == transpile.Prop && f.Name != "Children" {
						info.props = append(info.props, tagProp{attr: info.attrName(f.Name), typ: f.Type, slot: isNodeTypeName(f.Type), required: requiredTag(f.Tag)})
					}
				}
				return info
			}
			others = append(others, file)
		case strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go"):
			others = append(others, filepath.Join(dir, n))
		}
	}
	structRe := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(name) + `(?:\[[^\]\n]*\])?\s+struct\s*\{`)
	funcRe := regexp.MustCompile(`(?m)^func\s+` + regexp.QuoteMeta(name) + `\s*(?:\[[^\]\n]*\])?\s*\(`)
	for _, file := range others {
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if m := structRe.FindIndex(src); m != nil {
			e := skipGo(src, m[1]-1)
			if e < 0 {
				continue
			}
			body := string(src[m[1]-1 : e])
			fields := tagStructFields(body)
			info := &tagInfo{name: name, kind: "component", file: file, line: tagLineOf(src, m[0])}
			if embedsLive.MatchString(body) {
				info.kind = "live component"
			}
			for _, f := range fields {
				if f.name != "Children" && token.IsExported(f.name) {
					info.props = append(info.props, tagProp{attr: info.attrName(f.name), typ: f.typ, slot: isNodeTypeName(f.typ), doc: f.doc, required: requiredTag(f.tag)})
				}
			}
			return info
		}
		if m := funcRe.FindIndex(src); m != nil {
			return tagFuncComponent(dir, name, file, src, m[0], m[1]-1)
		}
	}
	return nil
}

// attrName is how a prop is written on the component's tag: a live
// component's as its field is declared, another's lower camel case (both
// bind, the compiler matching either).
func (info *tagInfo) attrName(field string) string {
	if info.kind == "live component" {
		return field
	}
	return lowerCamel(field)
}

func tagLineOf(src []byte, off int) int { return strings.Count(string(src[:off]), "\n") + 1 }

type tagField struct{ name, typ, doc, tag string }

// tagStructFields reads the fields of a struct's {…}, field attributes (.vuka's
// @…) left out.
func tagStructFields(body string) []tagField {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if loc := tagAttrStart.FindStringIndex(l); loc != nil && !strings.Contains(l[:loc[0]], "`") {
			lines[i] = l[:loc[0]]
		}
	}
	src := "package p\ntype T struct" + strings.Join(lines, "\n")
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", src, parser.ParseComments)
	if f == nil || len(f.Decls) == 0 {
		return nil
	}
	gd, ok := f.Decls[0].(*ast.GenDecl)
	if !ok || len(gd.Specs) == 0 {
		return nil
	}
	st, ok := gd.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType)
	if !ok {
		return nil
	}
	var out []tagField
	for _, fl := range st.Fields.List {
		typ := src[fset.Position(fl.Type.Pos()).Offset:fset.Position(fl.Type.End()).Offset]
		doc := strings.TrimSpace(fl.Doc.Text() + fl.Comment.Text())
		tag := ""
		if fl.Tag != nil {
			tag, _ = strconv.Unquote(fl.Tag.Value)
		}
		for _, n := range fl.Names {
			out = append(out, tagField{n.Name, typ, doc, tag})
		}
	}
	return out
}

// tagFuncComponent reads a function component's parameters: every one is
// required (E0144) but a first context.Context, children, a Node (a slot)
// and a variadic last; a single struct parameter passes its fields.
func tagFuncComponent(dir, name, file string, src []byte, start, open int) *tagInfo {
	e := tagParenEnd(src, open)
	if e < 0 {
		return nil
	}
	text := "package p\nfunc _" + string(src[open:e+1]) + "{}"
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", text, 0)
	if f == nil || len(f.Decls) == 0 {
		return nil
	}
	fd, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil
	}
	info := &tagInfo{name: name, kind: "function", file: file, line: tagLineOf(src, start)}
	type param struct {
		name, typ string
		variadic  bool
	}
	var params []param
	for _, fl := range fd.Type.Params.List {
		typ := text[fset.Position(fl.Type.Pos()).Offset:fset.Position(fl.Type.End()).Offset]
		_, variadic := fl.Type.(*ast.Ellipsis)
		for _, n := range fl.Names {
			params = append(params, param{n.Name, typ, variadic})
		}
	}
	if len(params) > 0 && params[0].typ == "context.Context" {
		params = params[1:]
	}
	if len(params) == 1 && params[0].name != "children" {
		t := strings.TrimPrefix(params[0].typ, "...")
		if c := tagPropsStruct(dir, t); c != nil {
			for _, fl := range c {
				if token.IsExported(fl.name) && fl.name != "Children" {
					info.props = append(info.props, tagProp{attr: lowerCamel(fl.name), typ: fl.typ, slot: isNodeTypeName(fl.typ), doc: fl.doc, required: requiredTag(fl.tag)})
				}
			}
			if !params[0].variadic {
				info.props = append(info.props, tagProp{attr: params[0].name, typ: params[0].typ, doc: "the whole " + t})
			}
			return info
		}
	}
	for i, pr := range params {
		if pr.name == "children" || pr.name == "_" {
			continue
		}
		node := isNodeTypeName(pr.typ)
		info.props = append(info.props, tagProp{attr: pr.name, typ: pr.typ, slot: node,
			required: !node && !(pr.variadic && i == len(params)-1)})
	}
	return info
}

// tagPropsStruct is the fields of the struct type name declared in the
// package at dir, nil when name is no struct there.
func tagPropsStruct(dir, name string) []tagField {
	if !token.IsIdentifier(name) {
		return nil
	}
	re := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(name) + `(?:\[[^\]\n]*\])?\s+struct\s*\{`)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka")) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		if m := re.FindIndex(src); m != nil {
			if e := skipGo(src, m[1]-1); e > 0 {
				return tagStructFields(string(src[m[1]-1 : e]))
			}
		}
	}
	return nil
}

// tagParenEnd is the ) closing the ( at i, -1 when none does.
func tagParenEnd(src []byte, i int) int {
	depth := 0
	for ; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		case '{':
			return -1
		}
	}
	return -1
}

// isNodeTypeName reports whether a type is a markup node: ui.Node, vuka.Node,
// templ.Component.
func isNodeTypeName(t string) bool {
	if strings.ContainsAny(t, "([]* ") {
		return false
	}
	t = t[strings.LastIndexByte(t, '.')+1:]
	return t == "Node" || t == "Component"
}

// propCompletion completes the attributes in a component's tag at off from
// the component's source; false when off is in no such tag, or the
// component can't be read (gopls's answer then).
func (p *proxy) propCompletion(path string, src []byte, off int) ([]any, bool) {
	spot, ok := completionSpot(src, off)
	if !ok || spot.kind != "attr" || !isComponentTag(spot.tag.name) || strings.Contains(spot.tag.name, ":") {
		return nil, false
	}
	info := p.resolveComponent(path, src, spot.tag.name)
	if info == nil {
		return nil, false
	}
	used := map[string]bool{}
	for _, a := range spot.tag.attrs {
		if off < a.start || off > a.end {
			used[strings.ToLower(a.name)] = true
		}
	}
	typed := lspRange{positionOf(src, spot.from), positionOf(src, off)}
	items := []any{}
	for i, pr := range info.props {
		if pr.slot || used[strings.ToLower(pr.attr)] {
			continue
		}
		text, format := pr.attr+"={$1}", 2
		switch pr.typ {
		case "bool":
			text, format = pr.attr, 1
		case "string":
			text = pr.attr + `="$1"`
		}
		sortText := fmt.Sprintf("1%02d", i)
		desc := "optional"
		if pr.required {
			sortText, desc = fmt.Sprintf("0%02d", i), "required"
		}
		it := map[string]any{"label": pr.attr, "kind": 5, "detail": pr.typ, "sortText": sortText, "filterText": pr.attr,
			"labelDetails": map[string]any{"detail": " " + pr.typ, "description": desc},
			"textEdit":     map[string]any{"range": typed, "newText": text}, "insertTextFormat": format}
		if pr.doc != "" {
			it["documentation"] = map[string]any{"kind": "markdown", "value": pr.doc}
		}
		items = append(items, it)
	}
	return items, true
}

// tagNote is a component tag's kind, file and props, for its hover.
func (p *proxy) tagNote(path string, src []byte, s *shapeScan, off int) string {
	if h := p.tagHover(path, src, s, off); h != nil {
		return h.(map[string]any)["contents"].(map[string]any)["value"].(string)
	}
	return ""
}

// tagHover shows a component tag's kind, file and props.
func (p *proxy) tagHover(path string, src []byte, s *shapeScan, off int) any {
	var tag jsxTag
	found := false
	for _, t := range s.tags {
		if off >= t.nameStart && off <= t.nameEnd {
			tag, found = t, true
			break
		}
	}
	if !found {
		return nil
	}
	info := p.resolveComponent(path, src, tag.name)
	if info == nil {
		return nil
	}
	return markdownHover(info.markdown(tag.name), src, tag.nameStart, tag.nameEnd)
}

func (info *tagInfo) markdown(tag string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** `<%s>` · [%s](%s#L%d)\n\n", info.kind, tag, filepath.Base(info.file), pathToURI(info.file), info.line)
	var rows []tagProp
	for _, pr := range info.props {
		if !pr.slot {
			rows = append(rows, pr)
		}
	}
	if len(rows) == 0 {
		b.WriteString("It takes no attributes.")
	} else {
		b.WriteString("| attribute | type | |\n|---|---|---|\n")
		for _, pr := range rows {
			req := ""
			if pr.required {
				req = "required"
			}
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", pr.attr, strings.ReplaceAll(pr.typ, "|", `\|`), req)
		}
	}
	var slots []string
	for _, pr := range info.props {
		if pr.kind != 0 {
			slots = append(slots, "`<slot:"+slotName(pr)+">`")
		}
	}
	if len(slots) > 0 {
		b.WriteString("\nSlots: " + strings.Join(slices.Compact(slots), ", ") + ".")
	}
	if info.kind != "function" {
		b.WriteString("\n\nAn attribute left out is the field's zero value.")
	}
	return b.String()
}
