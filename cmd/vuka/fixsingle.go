package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ui's single field attributes (@ui.Label, Help, Placeholder, Widget,
// Autocomplete, Required, Email, Min, Max, Pattern, OneOf, Msg) are the
// earlier spelling of a form field's tags and of @ui.Spec's members: the
// compiler warns at each (W0314), and vuka fix attributes writes them as
// the tags — @ui.Label("Work email") as label:"Work email", @ui.Min(12) as
// validate:"min=12", @ui.Msg("max", "Too long") as msg:"max=Too long" — or,
// where no tag says the same, as Spec's members: @ui.OneOf's values are
// Choices (the field's options, which validate:"oneof" isn't),
// @ui.Pattern is Pattern, and on a ui.Meta member, which takes no tag,
// everything is the Spec's. A field the rewrite can't keep meaning the
// same is left as written, with a note.

// singleNames are ui's single field attributes.
var singleNames = []string{"Label", "Help", "Placeholder", "Widget", "Autocomplete", "Required", "Email", "Min", "Max", "Pattern", "OneOf", "Msg"}

// singleTagKeys are the tag key of a single attribute that is one.
var singleTagKeys = map[string]string{"Label": "label", "Help": "help", "Placeholder": "placeholder", "Widget": "input", "Autocomplete": "autocomplete"}

// tagOrder is the order the rewrite adds a field's tags in.
var tagOrder = []string{"label", "help", "placeholder", "input", "autocomplete", "validate", "msg"}

// numberType matches a field type holding a number, through pointers and
// Options.
var numberType = regexp.MustCompile(`^[*\s]*(?:(?:\w+\.)?Option\[\s*)?[*\s]*(?:u?int(?:8|16|32|64)?|uintptr|byte|rune|float(?:32|64))\s*\]?$`)

// fieldRewrite is what a field's single attributes become.
type fieldRewrite struct {
	tags  map[string][]string // tag key → values (validate's rules, msg's entries)
	spec  [][2]string         // Spec members: name, Go expression
	msgs  [][2]string         // a Spec's Msg entries: rule, text, as Go
	drop  []writtenAttr
	notes []string
}

// singleAttrTags is src with its fields' single ui attributes written as
// tags and Spec members, how many fields changed, and why fields were left.
func singleAttrTags(src []byte) ([]byte, int, []string) {
	ui := importedAs(src, uiPath)
	if ui == "" {
		return src, 0, nil
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var notes []string
	n := 0
	for _, lt := range structTypes(src) {
		if lt.ui != "" {
			continue // a live page's fields: their attributes are @ui.Bind, @validate.…
		}
		for _, lf := range lt.fields {
			rw, ok := rewriteSingles(src, ui, lt.name, lf)
			notes = append(notes, rw.notes...)
			if !ok {
				continue
			}
			n++
			var fe []edit
			// the tag, rewritten or added after the type
			if len(rw.tags) > 0 {
				text := tagLiteral(lf.tag, rw.tags)
				if lf.tagStart >= 0 {
					fe = append(fe, edit{lf.tagStart, lf.tagEnd, text})
				} else {
					fe = append(fe, edit{lf.typeEnd, lf.typeEnd, " " + text})
				}
			}
			// the Spec: into the field's own, or in the first dropped one's place
			specAt := -1
			if len(rw.spec) > 0 || len(rw.msgs) > 0 {
				members := rw.spec
				if len(rw.msgs) > 0 {
					var es []string
					for _, m := range rw.msgs {
						es = append(es, m[0]+": "+m[1])
					}
					members = append(members, [2]string{"Msg", ui + ".Msgs{" + strings.Join(es, ", ") + "}"})
				}
				var parts []string
				for _, m := range members {
					parts = append(parts, m[0]+": "+m[1])
				}
				if sp := specOf(lf, ui); sp != nil {
					closing := sp.end - 1 // its }
					before := strings.TrimRight(string(src[sp.start:closing]), " \t\n")
					sep := ", "
					if strings.HasSuffix(before, "{") {
						sep = ""
					}
					text := sep + strings.Join(parts, ", ")
					if strings.HasSuffix(before, ",") { // a literal ending in a comma: one member a line
						text = "\t" + strings.Join(parts, ",\n\t") + ",\n"
						fe = append(fe, edit{specLine(src, closing), specLine(src, closing), text})
					} else {
						at := sp.start + len(before)
						fe = append(fe, edit{at, at, text})
					}
				} else {
					specAt = rw.drop[0].start
					fe = append(fe, edit{specAt, specAt, "@" + ui + ".Spec{" + strings.Join(parts, ", ") + "}"})
				}
			}
			for _, a := range rw.drop {
				start := a.start
				if start != specAt {
					for start > 0 && (src[start-1] == ' ' || src[start-1] == '\t') {
						start--
					}
				}
				fe = append(fe, edit{start, a.end, ""})
			}
			edits = append(edits, fe...)
		}
	}
	if len(edits) == 0 {
		return src, 0, notes
	}
	slices.SortStableFunc(edits, func(a, b edit) int { return a.start - b.start })
	var b bytes.Buffer
	last := 0
	for _, e := range edits {
		if e.start < last {
			continue
		}
		b.Write(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.Write(src[last:])
	return b.Bytes(), n, notes
}

// specLine is the offset of the start of the line holding off.
func specLine(src []byte, off int) int {
	return bytes.LastIndexByte(src[:off], '\n') + 1
}

// specOf is the field's @ui.Spec{…}, if it has one.
func specOf(lf liveField, ui string) *writtenAttr {
	for i, a := range lf.written {
		if attrName(a) == ui+".Spec" && len(a.toks) > 3 && a.toks[3].tok == token.LBRACE {
			return &lf.written[i]
		}
	}
	return nil
}

// attrName is an attribute's name as written: "ui.Label", "orm.Char".
func attrName(a writtenAttr) string {
	t := a.toks
	if len(t) >= 3 && t[0].tok == token.IDENT && t[1].tok == token.PERIOD && t[2].tok == token.IDENT {
		return t[0].lit + "." + t[2].lit
	}
	if len(t) > 0 && t[0].tok == token.IDENT {
		return t[0].lit
	}
	return ""
}

// rewriteSingles is what the field's single ui attributes become; ok is
// false when it has none, or when one can't be rewritten meaning the same.
func rewriteSingles(src []byte, ui, typeName string, lf liveField) (fieldRewrite, bool) {
	rw := fieldRewrite{tags: map[string][]string{}}
	typ := strings.TrimSpace(string(src[lf.typeStart:lf.typeEnd]))
	meta := typ == ui+".Meta"
	number := numberType.MatchString(typ)
	other := false // another library's attributes (a model's): its limits, choices, labels are there too
	specMembers := map[string]bool{}
	specMsg := false
	for _, a := range lf.written {
		name := attrName(a)
		pkg, _, _ := strings.Cut(name, ".")
		if pkg != ui {
			other = true
		}
		if name == ui+".Spec" {
			if lit := parseArgs(src, a, true); lit != nil {
				for _, el := range lit.(*ast.CompositeLit).Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok {
							specMembers[id.Name] = true
							specMsg = specMsg || id.Name == "Msg"
						}
					}
				}
			}
		}
	}
	field := typeName + "." + lf.names[0]
	leave := func(why string) (fieldRewrite, bool) {
		return fieldRewrite{notes: []string{field + " keeps its single attributes: " + why}}, false
	}
	existing := map[string]string{}
	for _, kv := range tagPairs(lf.tag) {
		existing[kv[0]] = kv[1]
	}
	addTag := func(key, v string) bool {
		if key != "validate" && key != "msg" && (existing[key] != "" || len(rw.tags[key]) > 0) {
			return false
		}
		if key == "validate" || key == "msg" {
			name, _, _ := strings.Cut(v, "=")
			for _, have := range append(strings.Split(existing[key], ","), rw.tags[key]...) {
				if h, _, _ := strings.Cut(strings.TrimSpace(have), "="); h == name {
					return false
				}
			}
		}
		rw.tags[key] = append(rw.tags[key], v)
		return true
	}
	addSpec := func(member, expr string) bool {
		if specMembers[member] || slices.ContainsFunc(rw.spec, func(m [2]string) bool { return m[0] == member }) {
			return false
		}
		rw.spec = append(rw.spec, [2]string{member, expr})
		return true
	}
	for _, a := range lf.written {
		pkg, name, _ := strings.Cut(attrName(a), ".")
		if pkg != ui || !slices.Contains(singleNames, name) {
			continue
		}
		at := "@" + ui + "." + name
		var args []ast.Expr
		if x := parseArgs(src, a, false); x != nil {
			args = x.(*ast.CallExpr).Args
		} else if len(a.toks) > 3 {
			return leave(at + " isn't read")
		}
		text := func(i int) string { return types0(src, a, args[i]) }
		lit := func(i int) (string, bool) {
			if i >= len(args) {
				return "", false
			}
			b, ok := args[i].(*ast.BasicLit)
			if !ok || b.Kind != token.STRING {
				return "", false
			}
			s, err := strconv.Unquote(b.Value)
			return s, err == nil
		}
		ok := true
		switch name {
		case "Label", "Help", "Placeholder", "Widget", "Autocomplete":
			if len(args) != 1 {
				return leave(at + " takes one value")
			}
			if s, isLit := lit(0); isLit && !meta && !other {
				ok = addTag(singleTagKeys[name], s)
			} else {
				ok = addSpec(name, text(0))
			}
		case "Required":
			s, isLit := lit(0)
			switch {
			case len(args) > 0 && !isLit:
				return leave(at + "'s message isn't a literal")
			case s != "" && (meta || number):
				// validate:"required" on a number also refuses 0; a Spec's
				// Required is the attribute's very setting
				ok = addSpec("Required", text(0))
			case meta:
				return leave(at + " with no message has no Spec spelling on a ui.Meta member")
			case number:
				return leave(at + " with no message on a number: a ui.FormFor requires the field by its type; elsewhere write validate:\"required\"")
			default:
				ok = addTag("validate", "required")
				if s != "" {
					ok = ok && addTag("msg", "required="+escapeMsgTag(s))
				}
			}
		case "Email":
			s, isLit := lit(0)
			switch {
			case meta:
				return leave(at + " has no Spec spelling on a ui.Meta member")
			case len(args) > 0 && !isLit:
				return leave(at + "'s message isn't a literal")
			}
			ok = addTag("validate", "email")
			if s != "" {
				ok = ok && addTag("msg", "email="+escapeMsgTag(s))
			}
		case "Min", "Max":
			v, isNum := numLit(args)
			whole := isNum && v > 0 && v == float64(int64(v))
			switch {
			case !isNum:
				return leave(at + "'s bound isn't a number literal")
			case other:
				return leave(at + " beside the model's attributes, which bound the field too: write it in @" + ui + ".Spec where it tightens them")
			case meta && !whole:
				return leave(at + " on a ui.Meta member: Spec's " + name + " is a whole number above 0")
			case meta:
				ok = addSpec(name, strconv.FormatInt(int64(v), 10))
			default:
				ok = addTag("validate", strings.ToLower(name)+"="+strconv.FormatFloat(v, 'f', -1, 64))
			}
		case "Pattern":
			if len(args) != 1 {
				return leave(at + " takes one value")
			}
			ok = addSpec("Pattern", text(0))
		case "OneOf":
			if other {
				return leave(at + " beside the model's attributes, whose choices it may widen: Spec's Choices only narrow them")
			}
			var vs []string
			for i := range args {
				vs = append(vs, text(i))
			}
			ok = addSpec("Choices", "[]string{"+strings.Join(vs, ", ")+"}")
		case "Msg":
			if len(args) != 2 {
				return leave(at + " takes a rule and a text")
			}
			rule, ruleLit := lit(0)
			msg, msgLit := lit(1)
			if ruleLit && msgLit && !meta {
				ok = addTag("msg", rule+"="+escapeMsgTag(msg))
			} else if specMsg {
				return leave(at + " beside a Spec's Msg")
			} else {
				rw.msgs = append(rw.msgs, [2]string{text(0), text(1)})
			}
		}
		if !ok {
			return leave(at + " says what another of its settings says")
		}
		rw.drop = append(rw.drop, a)
	}
	if len(rw.drop) == 0 {
		return fieldRewrite{}, false
	}
	if meta && len(rw.tags) > 0 {
		return leave("a ui.Meta member takes no tag")
	}
	return rw, true
}

// parseArgs parses the attribute's arguments: its call, or its composite
// literal when lit.
func parseArgs(src []byte, a writtenAttr, lit bool) ast.Expr {
	if len(a.toks) < 4 {
		return nil
	}
	open := a.toks[3]
	switch {
	case lit && open.tok == token.LBRACE:
		x, err := parser.ParseExpr("T" + string(src[open.off:a.end]))
		if err != nil {
			return nil
		}
		if _, ok := x.(*ast.CompositeLit); ok {
			return x
		}
	case !lit && open.tok == token.LPAREN:
		x, err := parser.ParseExpr("f" + string(src[open.off:a.end]))
		if err != nil {
			return nil
		}
		if _, ok := x.(*ast.CallExpr); ok {
			return x
		}
	}
	return nil
}

// types0 is the source text of an argument parsed by parseArgs.
func types0(src []byte, a writtenAttr, x ast.Expr) string {
	base := a.toks[3].off - 1 // the parse's offset 1 is the f, 2 its (
	return string(src[base+int(x.Pos())-1 : base+int(x.End())-1])
}

// numLit is the one argument's number, when it is a literal.
func numLit(args []ast.Expr) (float64, bool) {
	if len(args) != 1 {
		return 0, false
	}
	x, neg := args[0], false
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		x, neg = u.X, true
	}
	b, ok := x.(*ast.BasicLit)
	if !ok || b.Kind != token.INT && b.Kind != token.FLOAT {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(b.Value, "_", ""), 64)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// tagLiteral is the field's tag with adds: its own pairs in their order,
// validate and msg extended, then the new keys.
func tagLiteral(tag string, adds map[string][]string) string {
	var pairs []string
	done := map[string]bool{}
	for _, kv := range tagPairs(tag) {
		v := kv[1]
		if more := adds[kv[0]]; len(more) > 0 {
			v = strings.Join(append([]string{v}, more...), ",")
			done[kv[0]] = true
		}
		pairs = append(pairs, kv[0]+":"+strconv.Quote(v))
	}
	for _, k := range tagOrder {
		if vs := adds[k]; len(vs) > 0 && !done[k] {
			sep := " "
			if k == "validate" || k == "msg" {
				sep = ","
			}
			pairs = append(pairs, k+":"+strconv.Quote(strings.Join(vs, sep)))
		}
	}
	s := strings.Join(pairs, " ")
	if strings.Contains(s, "`") {
		return strconv.Quote(s)
	}
	return "`" + s + "`"
}

// escapeMsgTag is a message as a msg: tag holds it: a comma a rule=
// follows, and a backslash before a comma or a backslash, escaped (ui's
// parseMsgs reads them back).
func escapeMsgTag(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && (s[i+1] == ',' || s[i+1] == '\\'):
			b.WriteString(`\\`)
		case c == ',' && msgRuleNext(s[i+1:]):
			b.WriteString(`\,`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// msgRuleNext reports whether s starts with a msg: entry's rule= (after
// spaces): a name of letters, digits and _, then =.
func msgRuleNext(s string) bool {
	s = strings.TrimLeft(s, " ")
	n := 0
	for n < len(s) && (s[n] == '_' || 'a' <= s[n] && s[n] <= 'z' || 'A' <= s[n] && s[n] <= 'Z' || n > 0 && '0' <= s[n] && s[n] <= '9') {
		n++
	}
	return n > 0 && n < len(s) && s[n] == '='
}
