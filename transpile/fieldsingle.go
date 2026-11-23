package transpile

import (
	"go/types"
	"strconv"
	"strings"
)

// A form field's details have one main spelling, its tags (label:, help:,
// placeholder:, input:, autocomplete:, validate:, msg:), and one
// attribute, @ui.Spec{…}, for where a tag can't go (a ui.Meta member) or a
// model's field describing its forms. ui's single attributes — @ui.Label,
// Help, Placeholder, Widget, Autocomplete, Required, Email, Min, Max,
// Pattern, OneOf and Msg — are an earlier third spelling: they keep
// working, and each use is warned (W0314) with what to write instead,
// which vuka fix attributes writes. A single attribute with nothing to
// replace it where it is written (an @ui.Email on a ui.Meta member) isn't
// warned. The @validate.* attributes of live-page fields are another
// thing, and stay.

// singleAttrs are ui's single field attributes.
var singleAttrs = map[string]bool{"Label": true, "Help": true, "Placeholder": true, "Widget": true, "Autocomplete": true,
	"Required": true, "Email": true, "Min": true, "Max": true, "Pattern": true, "OneOf": true, "Msg": true}

// singleTags are the tag key each single attribute is, and its Spec member.
var singleTags = map[string]string{"Label": "label", "Help": "help", "Placeholder": "placeholder", "Widget": "input", "Autocomplete": "autocomplete"}

// warnSingleAttrs warns at each of ui's single field attributes (W0314).
func (e *engine) warnSingleAttrs() {
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			if !a.Field || len(a.fieldNames) == 0 {
				continue
			}
			pkgName, name, ok := strings.Cut(a.Name, ".")
			if !ok || !singleAttrs[name] {
				continue
			}
			if pkg := e.importedAs(f, pkgName); pkg == nil || !declaresState(pkg) {
				continue
			}
			meta, number := e.fieldKind(a.structOf, a.fieldNames[0])
			msg, ok := singleReplacement(a, pkgName, name, meta, number)
			if !ok {
				continue
			}
			f.singleWarns.add("W0314", a.Pos, "%s", msg).
				help("vuka fix attributes writes it")
		}
	}
}

// fieldKind reports whether the struct type's field is a ui.Meta member,
// and whether it holds a number (through pointers and Options).
func (e *engine) fieldKind(typeName, field string) (meta, number bool) {
	obj, _ := e.pkg.Scope().Lookup(typeName).(*types.TypeName)
	if obj == nil {
		return false, false
	}
	st, _ := obj.Type().Underlying().(*types.Struct)
	if st == nil {
		return false, false
	}
	for i := 0; i < st.NumFields(); i++ {
		fl := st.Field(i)
		if fl.Name() != field {
			continue
		}
		if isUIMeta(fl.Type()) {
			return true, false
		}
		t := fl.Type()
		for {
			if n := nullableOf(t); n != nil {
				t = n
				continue
			}
			break
		}
		b, _ := t.Underlying().(*types.Basic)
		return false, b != nil && b.Info()&types.IsNumeric != 0
	}
	return false, false
}

// singleReplacement is W0314's message for the single attribute a (ui's
// name), naming what to write instead; ok is false when nothing replaces
// it where it is written.
func singleReplacement(a *Attr, ui, name string, meta, number bool) (string, bool) {
	args := callArgs(a)
	str := func(i int) (string, bool) {
		if i < len(args) {
			return strLit(args[i])
		}
		return "", false
	}
	quoted := func(i int) string {
		if s, ok := str(i); ok {
			return strconv.Quote(s)
		}
		return `"…"`
	}
	tag := func(key string, i int) string { return "`" + key + ":" + quoted(i) + "`" }
	spec := func(member, val string) string { return "@" + ui + ".Spec{" + member + ": " + val + "}" }
	at := "@" + ui + "." + name
	if meta {
		at += " on a ui.Meta member"
	}
	switch name {
	case "Label", "Help", "Placeholder", "Widget", "Autocomplete":
		if meta {
			return at + " is " + name + " in @" + ui + ".Spec — write " + spec(name, quoted(0)), true
		}
		return at + " is the " + singleTags[name] + ": tag — write " + tag(singleTags[name], 0) + ", or set " + name + " in @" + ui + ".Spec", true
	case "Required":
		text, has := str(0)
		withMsg := has && text != ""
		switch {
		case withMsg && (meta || number):
			// on a number, validate:"required" also refuses 0; Spec's
			// Required is the attribute's very setting
			return at + " is Required in @" + ui + ".Spec — write " + spec("Required", strconv.Quote(text)), true
		case meta, number && len(args) == 0:
			return "", false
		case withMsg:
			return at + ` is the validate: rule required — write ` + "`" + `validate:"required" msg:` + strconv.Quote("required="+text) + "`" + `, or set Required in @` + ui + ".Spec", true
		case len(args) > 0 && !has:
			return at + ` is the validate: rule required — write ` + "`" + `validate:"required" msg:"required=…"` + "`", true
		}
		return at + ` is the validate: rule required — write ` + "`" + `validate:"required"` + "`", true
	case "Email":
		if meta {
			return "", false
		}
		if text, ok := str(0); ok && text != "" {
			return at + ` is the validate: rule email — write ` + "`" + `validate:"email" msg:` + strconv.Quote("email="+text) + "`", true
		}
		return at + ` is the validate: rule email — write ` + "`" + `validate:"email"` + "`", true
	case "Min", "Max":
		rule := strings.ToLower(name)
		n, ok := 0.0, false
		if len(args) == 1 {
			n, ok = intLit(args[0])
		}
		whole := ok && n > 0 && n == float64(int64(n))
		if meta {
			if !whole {
				return "", false
			}
			return at + " is " + name + " in @" + ui + ".Spec — write " + spec(name, strconv.FormatInt(int64(n), 10)), true
		}
		v := "…"
		if ok {
			v = strconv.FormatFloat(n, 'f', -1, 64)
		}
		msg := at + " is the validate: rule " + rule + " — write `validate:\"" + rule + "=" + v + "\"`"
		if whole {
			msg += ", or set " + name + " in @" + ui + ".Spec"
		}
		return msg, true
	case "Pattern":
		return at + " is Pattern in @" + ui + ".Spec — write " + spec("Pattern", quoted(0)), true
	case "OneOf":
		var vs []string
		for i := range args {
			vs = append(vs, quoted(i))
		}
		return at + " is Choices in @" + ui + ".Spec — write " + spec("Choices", "[]string{"+strings.Join(vs, ", ")+"}"), true
	case "Msg":
		rule, ok := str(0)
		if !ok {
			rule = "…"
		}
		text, ok := str(1)
		if !ok {
			text = "…"
		}
		member := "Msg in @" + ui + ".Spec"
		write := spec("Msg", ui+".Msgs{"+strconv.Quote(rule)+": "+strconv.Quote(text)+"}")
		if meta {
			return at + " is " + member + " — write " + write, true
		}
		return at + " is the msg: tag — write `msg:" + strconv.Quote(rule+"="+escapeMsg(text)) + "`, or set " + member, true
	}
	return "", false
}

// escapeMsg is a message as a msg: tag holds it: a comma a rule= follows,
// and a backslash before a comma or a backslash, escaped (ui's parseMsgs).
func escapeMsg(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && (s[i+1] == ',' || s[i+1] == '\\'):
			b.WriteString(`\\`)
		case c == ',' && msgRuleAt(s[i+1:]):
			b.WriteString(`\,`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// msgRuleAt reports whether s starts with a msg: entry's rule= (after
// spaces): a name of letters, digits and _, then =.
func msgRuleAt(s string) bool {
	s = strings.TrimLeft(s, " ")
	n := 0
	for n < len(s) && (s[n] == '_' || 'a' <= s[n] && s[n] <= 'z' || 'A' <= s[n] && s[n] <= 'Z' || n > 0 && '0' <= s[n] && s[n] <= '9') {
		n++
	}
	return n > 0 && n < len(s) && s[n] == '='
}
