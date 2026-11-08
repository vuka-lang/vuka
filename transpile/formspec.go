package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// A field's details as a form field are its attributes: the model
// library's (@orm.Char{Max: 100}), ui's single ones (@ui.Placeholder(…)),
// and @ui.Spec{…}, ui's in one struct literal. A form type's ui.Meta
// member carries the details of T's field of its name in that form:
//
//	type ProductForm struct {
//		ui.FormFor[Product]
//		Name ui.Meta @ui.Spec{Required: "Name is required.", Max: 60}
//	}
//
// The compiler checks what Go's type-check can't: a Spec's Msg naming a
// rule there is none of, an empty Required, Required beside Optional, a
// Max above (a Min below) what the field's model says, Choices outside
// the model's (E0189); one setting given by a Spec and by a single
// attribute on one field (E0183); a ui.Meta naming no field of T, or
// outside a form type (E0190).

// msgRules are the rules a field's message may be for (ui's).
var msgRules = []string{"required", "min", "max", "len", "pattern", "oneof", "choice", "email", "unique", "number", "whole", "format", "digits", "file"}

// specSettings are what each ui.Spec member sets, as uiTagSettings names
// the settings.
var specSettings = map[string]string{"Label": "label", "Help": "help", "Placeholder": "placeholder", "Widget": "input", "Autocomplete": "autocomplete",
	"Required": "validate:required", "Optional": "validate:required", "Min": "validate:min", "Max": "validate:max", "Choices": "validate:oneof"}

// fieldAttr is a field attribute and the file it is written in.
type fieldAttr struct {
	f   *fileState
	a   *Attr
	lib string // "ui", "orm" or ""
	pkg *types.Package
}

// attrLimits are the limits and choices attributes say of a field.
type attrLimits struct {
	min, max   *float64
	choices    []string
	hasChoices bool
}

// specMember is a member of a Spec's literal: its value and where it is.
type specMember struct {
	val ast.Expr
	off int // in the file's src
}

// litOf is an attribute's struct literal and the src offset its parse
// starts at, for one written @x.T{…}; nil otherwise.
func litOf(a *Attr) (*ast.CompositeLit, int) {
	args := a.Args
	lead := len(args) - len(strings.TrimLeft(args, " \t"))
	if !strings.HasPrefix(args[lead:], "{") {
		return nil, 0
	}
	x, err := parser.ParseExpr("T" + args[lead:])
	if err != nil {
		return nil, 0
	}
	lit, _ := x.(*ast.CompositeLit)
	// the parse's offset 1 is the T, 2 the brace at nameStart+len(Name)+lead
	return lit, a.nameStart + len(a.Name) + lead - 2
}

// callArgs are an attribute's arguments when it is called: @ui.Max(50).
func callArgs(a *Attr) []ast.Expr {
	if !strings.HasPrefix(strings.TrimSpace(a.Args), "(") {
		return nil
	}
	x, err := parser.ParseExpr("f" + strings.TrimSpace(a.Args))
	if err != nil {
		return nil
	}
	c, _ := x.(*ast.CallExpr)
	if c == nil {
		return nil
	}
	return c.Args
}

// members are a struct literal's keyed members.
func members(lit *ast.CompositeLit, base int) map[string]specMember {
	out := map[string]specMember{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				out[id.Name] = specMember{kv.Value, base + int(kv.Key.Pos())}
			}
		}
	}
	return out
}

func intLit(x ast.Expr) (float64, bool) {
	neg := false
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		neg, x = true, u.X
	}
	lit, ok := x.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT && lit.Kind != token.FLOAT {
		return 0, false
	}
	n, err := strconv.ParseFloat(lit.Value, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

func strLit(x ast.Expr) (string, bool) {
	lit, ok := x.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// strList is a []string{…} literal's strings; ok is false for anything
// else (a variable the compiler can't read).
func strList(x ast.Expr) ([]string, bool) {
	lit, ok := x.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	var out []string
	for _, el := range lit.Elts {
		s, ok := strLit(el)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// limit tightens or sets what an attribute says of a field's limits.
func (l *attrLimits) add(fa fieldAttr) {
	name := strings.TrimPrefix(fa.a.Name, fa.a.Name[:strings.IndexByte(fa.a.Name, '.')+1])
	set := func(dst **float64, x ast.Expr) {
		if n, ok := intLit(x); ok && n != 0 {
			*dst = &n
		}
	}
	setInt := func(dst **float64, x ast.Expr) {
		if n, ok := intLit(x); ok {
			*dst = &n
		}
	}
	choices := func(x ast.Expr) {
		if vs, ok := strList(x); ok {
			l.choices, l.hasChoices = vs, true
		}
	}
	switch fa.lib {
	case "orm":
		lit, base := litOf(fa.a)
		var ms map[string]specMember
		if lit != nil {
			ms = members(lit, base)
		}
		switch name {
		case "Char", "Slug":
			if m, ok := ms["Max"]; ok {
				set(&l.max, m.val)
			} else if name == "Slug" {
				n := 50.0
				l.max = &n
			}
			if m, ok := ms["Min"]; ok {
				set(&l.min, m.val)
			}
			if m, ok := ms["Choices"]; ok {
				choices(m.val)
			}
		case "Int":
			if m, ok := ms["Max"]; ok {
				setInt(&l.max, m.val)
			}
			if m, ok := ms["Min"]; ok {
				setInt(&l.min, m.val)
			}
		case "LongText":
			l.max = nil
		}
	case "ui":
		args := callArgs(fa.a)
		switch name {
		case "Max", "Min":
			if len(args) == 1 {
				dst := &l.max
				if name == "Min" {
					dst = &l.min
				}
				setInt(dst, args[0])
			}
		case "OneOf":
			var vs []string
			for _, x := range args {
				s, ok := strLit(x)
				if !ok {
					return
				}
				vs = append(vs, s)
			}
			l.choices, l.hasChoices = vs, true
		case "Spec":
			lit, base := litOf(fa.a)
			if lit == nil {
				return
			}
			ms := members(lit, base)
			if m, ok := ms["Max"]; ok {
				if n, ok := intLit(m.val); ok && n > 0 && (l.max == nil || n < *l.max) {
					l.max = &n
				}
			}
			if m, ok := ms["Min"]; ok {
				if n, ok := intLit(m.val); ok && n > 0 && (l.min == nil || n > *l.min) {
					l.min = &n
				}
			}
			if m, ok := ms["Choices"]; ok {
				choices(m.val)
			}
		}
	}
}

// fieldAttrs are the field attributes of every .vuka file, by struct type
// and field: "Product.Name".
func (e *engine) fieldAttrs() map[string][]fieldAttr {
	out := map[string][]fieldAttr{}
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			if !a.Field || len(a.fieldNames) == 0 {
				continue
			}
			pkgName, _, ok := strings.Cut(a.Name, ".")
			if !ok {
				continue
			}
			fa := fieldAttr{f: f, a: a}
			if fa.pkg = e.importedAs(f, pkgName); fa.pkg != nil {
				switch {
				case declaresState(fa.pkg):
					fa.lib = "ui"
				case ormPackage(fa.pkg):
					fa.lib = "orm"
				}
			}
			key := a.structOf + "." + a.fieldNames[0]
			out[key] = append(out[key], fa)
		}
	}
	return out
}

// limitsOf are what a field's attributes say of its limits, in their
// order of precedence: the model library's, then ui's.
func limitsOf(attrs []fieldAttr) attrLimits {
	var l attrLimits
	for _, lib := range []string{"orm", "ui"} {
		for _, fa := range attrs {
			if fa.lib == lib {
				l.add(fa)
			}
		}
	}
	return l
}

// checkFieldSpecs reports what is wrong with each field's ui.Spec and
// ui.Meta (E0189, E0190), and a setting both a Spec and a single
// attribute give one field (E0183).
func (e *engine) checkFieldSpecs() {
	all := e.fieldAttrs()
	metas := e.formMetas()
	keys := slices.Sorted(maps.Keys(all))
	for _, key := range keys {
		attrs := all[key]
		var model attrLimits
		if m, ok := metas[key]; ok {
			model = limitsOf(all[m])
		} else {
			// a model's own Spec answers to the model library's limits
			var lib []fieldAttr
			for _, fa := range attrs {
				if fa.lib == "orm" {
					lib = append(lib, fa)
				}
			}
			model = limitsOf(lib)
		}
		set := map[string]*Attr{}
		for _, fa := range attrs {
			if fa.lib != "ui" {
				continue
			}
			for _, s := range e.uiSettings(fa) {
				if prev := set[s]; prev != nil && prev != fa.a {
					what := strings.TrimPrefix(s, "validate:")
					if r, ok := strings.CutPrefix(s, "msg:"); ok {
						what = "message for " + r
					}
					e.errs.add("E0183", fa.a.Pos, "%s.%s's %s is set by @%s and by @%s: a setting has one source",
						fa.a.structOf, fa.a.fieldNames[0], what, prev.Name, fa.a.Name).
						help("keep one of them: a ui.Spec's member, or the single attribute")
					continue
				}
				set[s] = fa.a
			}
			if strings.HasSuffix(fa.a.Name, ".Spec") {
				e.checkSpec(fa, model)
			}
		}
	}
}

// uiSettings are the settings a ui attribute gives its field: "label",
// "validate:max", "msg:unique".
func (e *engine) uiSettings(fa fieldAttr) []string {
	_, name, _ := strings.Cut(fa.a.Name, ".")
	if name != "Spec" {
		var out []string
		if s := uiTagSettings[name]; s != "" {
			out = append(out, s)
		}
		switch args := callArgs(fa.a); {
		case name == "Msg" && len(args) > 0:
			if r, ok := strLit(args[0]); ok {
				out = append(out, "msg:"+r)
			}
		case name == "Required" && len(args) > 0:
			out = append(out, "msg:required")
		}
		return out
	}
	lit, base := litOf(fa.a)
	if lit == nil {
		return nil
	}
	var out []string
	for k, m := range members(lit, base) {
		if s := specSettings[k]; s != "" {
			out = append(out, s)
		}
		if k == "Required" {
			out = append(out, "msg:required")
		}
		if k == "Msg" {
			for r := range msgKeys(m.val, 0) {
				out = append(out, "msg:"+r)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// msgKeys are a ui.Msgs{…} literal's keys, by their offset from base.
func msgKeys(x ast.Expr, base int) map[string]int {
	lit, ok := x.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := map[string]int{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if r, ok := strLit(kv.Key); ok {
				out[r] = base + int(kv.Key.Pos())
			}
		}
	}
	return out
}

// checkSpec reports a ui.Spec's members the compiler can tell are wrong
// (E0189), model being what the field's model says.
func (e *engine) checkSpec(fa fieldAttr, model attrLimits) {
	lit, base := litOf(fa.a)
	if lit == nil {
		return
	}
	f, field := fa.f, fa.a.structOf+"."+fa.a.fieldNames[0]
	ms := members(lit, base)
	if m, ok := ms["Required"]; ok {
		if s, ok := strLit(m.val); ok && s == "" {
			e.errs.add("E0189", f.at(m.off), "%s: ui.Spec's Required is the message of a required field, and is empty", field).
				help(`write its message, Required: "Enter a name", or @ui.Required for the default one`)
		}
		if o, ok := ms["Optional"]; ok {
			if id, ok := o.val.(*ast.Ident); !ok || id.Name != "false" {
				e.errs.add("E0189", f.at(o.off), "%s: ui.Spec has Required and Optional: a field is one or the other", field).
					help("drop one of them")
			}
		}
	}
	if m, ok := ms["Msg"]; ok {
		keys := msgKeys(m.val, base)
		for _, r := range slices.Sorted(maps.Keys(keys)) {
			off := keys[r]
			if slices.Contains(msgRules, r) {
				continue
			}
			d := e.errs.add("E0189", f.at(off), "%s: ui.Spec's Msg %q names no rule", field, r)
			if s := closestName(r, msgRules); s != "" {
				d.help("did you mean %q? the rules are %s", s, strings.Join(msgRules, ", "))
			} else {
				d.help("the rules are %s", strings.Join(msgRules, ", "))
			}
		}
	}
	num := func(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
	if m, ok := ms["Max"]; ok && model.max != nil {
		if n, ok := intLit(m.val); ok && n > *model.max {
			e.errs.add("E0189", f.at(m.off), "%s: ui.Spec's Max %s is above the model's %s: a form may tighten a field's limits, not loosen them", field, num(n), num(*model.max)).
				help("write a Max of %s or less, or raise the model's", num(*model.max))
		}
	}
	if m, ok := ms["Min"]; ok && model.min != nil {
		if n, ok := intLit(m.val); ok && n > 0 && n < *model.min {
			e.errs.add("E0189", f.at(m.off), "%s: ui.Spec's Min %s is below the model's %s: a form may tighten a field's limits, not loosen them", field, num(n), num(*model.min)).
				help("write a Min of %s or more, or lower the model's", num(*model.min))
		}
	}
	if m, ok := ms["Choices"]; ok && model.hasChoices {
		if vs, ok := strList(m.val); ok {
			for _, v := range vs {
				if !slices.Contains(model.choices, v) {
					e.errs.add("E0189", f.at(m.off), "%s: ui.Spec's choice %q isn't one of the model's: a form may narrow a field's choices, not add to them", field, v).
						help("the model's choices are %s", strings.Join(model.choices, ", "))
					break
				}
			}
		}
	}
}

// formMetas are the ui.Meta members of the form types the .vuka files
// declare, "ProductForm.Name" → "Product.Name": the field each describes,
// when T is declared in this package. A ui.Meta naming no field of T, or
// in a struct that is no form type, is reported (E0190).
func (e *engine) formMetas() map[string]string {
	out := map[string]string{}
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			obj, ok := e.info.Defs[spec.Name].(*types.TypeName)
			if !ok {
				return false
			}
			st, ok := obj.Type().Underlying().(*types.Struct)
			if !ok {
				return false
			}
			vt := formTypeValue(st)
			for i := 0; i < st.NumFields(); i++ {
				fl := st.Field(i)
				if fl.Embedded() || !isUIMeta(fl.Type()) {
					continue
				}
				pos := f.nodePos(fl.Pos())
				if vt == nil {
					e.errs.add("E0190", pos, "%s.%s is a ui.Meta, which describes a field of a form type's T, and %s embeds no ui.FormFor", obj.Name(), fl.Name(), obj.Name()).
						help("embed ui.FormFor[T] in %s, or give %s a type", obj.Name(), fl.Name())
					continue
				}
				other, _, _ := types.LookupFieldOrMethod(vt, true, fl.Pkg(), fl.Name())
				if v, ok := other.(*types.Var); !ok || !v.IsField() {
					t := e.display(vt)
					names := fieldNames(vt)
					if s := closestName(fl.Name(), names); s != "" {
						e.errs.add("E0190", pos, "%s.%s is a ui.Meta describing %s's field of the same name, and %s has no field %s; did you mean %s?", obj.Name(), fl.Name(), t, t, fl.Name(), s).
							help("name the member as %s's field: %s ui.Meta", t, s)
					} else {
						e.errs.add("E0190", pos, "%s.%s is a ui.Meta describing %s's field of the same name, and %s has no field %s", obj.Name(), fl.Name(), t, t, fl.Name()).
							help("%s's fields are %s", t, strings.Join(names, ", "))
					}
					continue
				}
				if n := namedOf(vt); n != nil && n.Obj().Pkg() == e.pkg {
					out[obj.Name()+"."+fl.Name()] = n.Obj().Name() + "." + fl.Name()
				}
			}
			return false
		})
	}
	return out
}

// formTypeValue is T of a struct embedding a FormFor[T] (by value); nil
// for any other struct.
func formTypeValue(st *types.Struct) types.Type {
	for i := 0; i < st.NumFields(); i++ {
		fl := st.Field(i)
		if _, ptr := types.Unalias(fl.Type()).(*types.Pointer); !fl.Embedded() || ptr {
			continue
		}
		if vt := formValueType(fl.Type()); vt != nil {
			return vt
		}
	}
	return nil
}

// isUIMeta reports whether t is the ui target's Meta, a form type's
// marker member.
func isUIMeta(t types.Type) bool {
	n := namedOf(t)
	if n == nil || n.Obj().Name() != "Meta" || n.Obj().Pkg() == nil || !declaresState(n.Obj().Pkg()) {
		return false
	}
	st, ok := n.Underlying().(*types.Struct)
	return ok && st.NumFields() == 0
}
