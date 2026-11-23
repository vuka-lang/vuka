package transpile

import (
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"
)

// A prop declares its default in its vuka tag (RFC components-seamless §4):
//
//	type Button struct {
//		Type string `vuka:"default=button"`
//		Tone Tone   `vuka:"default=ToneNeutral"`
//	}
//
// The value is the field type's: a string's text, a number, a bool, or a
// constant of the field's package by name. A tag leaving the prop out sets
// it to the default — <Button/> is Button{Type: "button", Tone: "neutral"} —
// while one setting it, even to the zero value, sets what it says: type=""
// is "". A prop written type?={t} takes the default when t is zero. A
// default that isn't of its field's type, names no constant, isn't one of
// the type's constants or sits beside required is E0136, at the field.
//
// A field of type ui.AutoID a tag leaves out is its id, the one
// ui.NewAutoID names in the context the tag renders in: the tag's call is
// in ui.Ctx, Dialog{ID: ui.NewAutoID(ctx)}.

// propDefault is what a prop the tag leaves out is set to.
type propDefault struct {
	text string // the value's Go: an untyped constant, or ui.NewAutoID(ctx)
	auto bool   // a ui.AutoID's id
}

// vukaOption is the value of key= in a struct tag's vuka options, and
// whether it is there.
func vukaOption(tag, key string) (string, bool) {
	for _, v := range strings.Split(reflect.StructTag(tag).Get("vuka"), ",") {
		if val, ok := strings.CutPrefix(strings.TrimSpace(v), key+"="); ok {
			return val, true
		}
	}
	return "", false
}

// fieldDefault is fl's default, nil when it has none; problem says why a
// default it declares can't be. A ui.AutoID has none where no ctx is
// (noAuto): a repeated slot's element, a live component's props.
func (e *engine) fieldDefault(tg *jsxTarget, fl propField, noAuto bool) (d *propDefault, problem string) {
	if val, ok := vukaOption(fl.tag, "default"); ok {
		text, problem := e.parseDefault(fl, val)
		if problem != "" {
			return nil, problem
		}
		return &propDefault{text: text}, ""
	}
	if !noAuto && tg != nil && tg.has("NewAutoID") && isNamed(fl.typ, tg.path, "AutoID") {
		return &propDefault{text: tg.q + "NewAutoID(ctx)", auto: true}, ""
	}
	return nil, ""
}

// parseDefault is a default's value as an untyped constant of fl's type,
// or why it can't be.
func (e *engine) parseDefault(fl propField, val string) (string, string) {
	tname := e.display(fl.typ)
	switch {
	case fl.required:
		return "", "default=" + val + " beside required: a tag that leaves the prop out gets its default, so it is never missing; drop one"
	case val == "":
		return "", "default= takes a value: default=" + exampleDefault(fl.typ)
	}
	basic, _ := fl.typ.Underlying().(*types.Basic)
	if basic == nil || basic.Info()&(types.IsString|types.IsNumeric|types.IsBoolean) == 0 || basic.Info()&types.IsComplex != 0 {
		return "", "default=" + val + ": " + tname + " takes no default; a default is a string, a number, a bool, or a constant of the field's package"
	}
	var v constant.Value
	if k := sameConst(fl, val); k != nil {
		v = k.Val()
	} else {
		switch {
		case basic.Info()&types.IsString != 0:
			v = constant.MakeString(val)
		case basic.Info()&types.IsBoolean != 0:
			b, err := strconv.ParseBool(val)
			if err != nil {
				return "", e.noConst(fl, val, "a bool: true or false")
			}
			v = constant.MakeBool(b)
		default:
			v = constant.MakeFromLiteral(val, token.INT, 0)
			if v.Kind() == constant.Unknown && basic.Info()&types.IsFloat != 0 {
				v = constant.MakeFromLiteral(val, token.FLOAT, 0)
			}
			if v.Kind() == constant.Unknown {
				what := "an integer"
				if basic.Info()&types.IsFloat != 0 {
					what = "a number"
				}
				return "", e.noConst(fl, val, what)
			}
		}
	}
	if _, ok := constant.Val(v).(bool); ok && basic.Info()&types.IsBoolean == 0 ||
		v.Kind() == constant.String && basic.Info()&types.IsString == 0 ||
		(v.Kind() == constant.Int || v.Kind() == constant.Float) && basic.Info()&types.IsNumeric == 0 {
		return "", "default=" + val + " isn't " + tname
	}
	if basic.Info()&types.IsInteger != 0 {
		if v.Kind() != constant.Int {
			return "", "default=" + val + " isn't an integer, and " + tname + " is"
		}
		if !fitsBasic(v, basic) {
			return "", "default=" + val + " overflows " + tname
		}
	}
	if named, ok := types.Unalias(fl.typ).(*types.Named); ok && named.Obj().Pkg() != nil && named.TypeArgs().Len() == 0 && basic.Info()&(types.IsString|types.IsInteger) != 0 {
		if consts := enumConsts(named); len(consts) > 0 && !e.openEnum(named.Obj()) {
			found := false
			var vals, names []string
			for _, k := range consts {
				found = found || constant.Compare(k.Val(), token.EQL, v)
				vals = append(vals, constText(k.Val()))
				names = append(names, k.Name())
			}
			if !found {
				msg := "default=" + val + " is none of " + tname + "'s constants: " + strings.Join(vals, ", ")
				near := closestName(val, names)
				if near == "" && v.Kind() == constant.String {
					for _, k := range consts {
						if s := constant.StringVal(k.Val()); closestName(val, []string{s}) != "" {
							near = s
						}
					}
				}
				if near != "" {
					msg += " (did you mean default=" + near + "?)"
				}
				return "", msg
			}
		}
	}
	return constText(v), ""
}

// sameConst is the constant of fl's package named name that a value of
// fl's type can be, if any.
func sameConst(fl propField, name string) *types.Const {
	if fl.pkg == nil || !token.IsIdentifier(name) {
		return nil
	}
	k, _ := fl.pkg.Scope().Lookup(name).(*types.Const)
	if k == nil || !types.AssignableTo(k.Type(), fl.typ) {
		return nil
	}
	return k
}

// noConst says a default is neither a value of its kind nor a constant.
func (e *engine) noConst(fl propField, val, kind string) string {
	tname := e.display(fl.typ)
	if !token.IsIdentifier(val) || fl.pkg == nil {
		return "default=" + val + " isn't " + kind + ", as " + tname + " takes"
	}
	msg := "default=" + val + " is neither " + kind + " nor a constant of " + tname + " in package " + fl.pkg.Name()
	var names []string
	for _, n := range fl.pkg.Scope().Names() {
		if k, ok := fl.pkg.Scope().Lookup(n).(*types.Const); ok && types.AssignableTo(k.Type(), fl.typ) {
			names = append(names, n)
		}
	}
	if near := closestName(val, names); near != "" {
		msg += " (did you mean default=" + near + "?)"
	}
	return msg
}

func fitsBasic(v constant.Value, b *types.Basic) bool {
	switch b.Kind() {
	case types.Int8:
		return fitsInt(v, 8)
	case types.Int16:
		return fitsInt(v, 16)
	case types.Int32:
		return fitsInt(v, 32)
	case types.Int, types.Int64:
		return fitsInt(v, 64)
	case types.Uint8:
		return fitsUint(v, 8)
	case types.Uint16:
		return fitsUint(v, 16)
	case types.Uint32:
		return fitsUint(v, 32)
	case types.Uint, types.Uint64, types.Uintptr:
		return fitsUint(v, 64)
	}
	return true
}

func fitsInt(v constant.Value, bits uint) bool {
	n, exact := constant.Int64Val(v)
	return exact && n >= -1<<(bits-1) && n <= 1<<(bits-1)-1
}

func fitsUint(v constant.Value, bits uint) bool {
	n, exact := constant.Uint64Val(v)
	return exact && (bits == 64 || n <= 1<<bits-1)
}

// constText is v as an untyped constant's Go.
func constText(v constant.Value) string {
	switch v.Kind() {
	case constant.String:
		return strconv.Quote(constant.StringVal(v))
	case constant.Float:
		f, _ := constant.Float64Val(v)
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return v.ExactString()
}

func exampleDefault(t types.Type) string {
	if b, ok := t.Underlying().(*types.Basic); ok {
		switch {
		case b.Info()&types.IsBoolean != 0:
			return "true"
		case b.Info()&types.IsNumeric != 0:
			return "1"
		}
	}
	return "button"
}

// checkDefaults reports, at the field, each default of the package's props
// that can't be (E0136): those of every struct type it declares, whether
// or not a tag uses it yet.
func (e *engine) checkDefaults() {
	if e.defaultsChecked || e.pkg == nil || e.info == nil {
		return
	}
	e.defaultsChecked = true
	scope := e.pkg.Scope()
	for _, n := range scope.Names() {
		tn, ok := scope.Lookup(n).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < st.NumFields(); i++ {
			fld := st.Field(i)
			val, ok := vukaOption(st.Tag(i), "default")
			if !ok {
				continue
			}
			if isInvalid(fld.Type()) {
				e.defaultsChecked = false // known in a later round
				continue
			}
			fl := propField{name: fld.Name(), typ: fld.Type(), required: required(st.Tag(i)), pkg: fld.Pkg(), tag: st.Tag(i)}
			if _, problem := e.parseDefault(fl, val); problem != "" {
				e.reportDefault(tn.Name(), fld, problem)
			}
		}
	}
}

func (e *engine) reportDefault(typ string, fld *types.Var, problem string) {
	key := fld.Pos()
	if e.defaultErrs[key] {
		return
	}
	if e.defaultErrs == nil {
		e.defaultErrs = map[token.Pos]bool{}
	}
	e.defaultErrs[key] = true
	var pos token.Position
	if f := e.fileOf(fld.Pos()); f != nil {
		pos = f.at(f.orig(fld.Pos()))
	} else {
		pos = e.fset.Position(fld.Pos())
	}
	err := e.errs.add("E0136", pos, "%s.%s: %s", typ, fld.Name(), problem)
	err.help("a default is the field's value when a tag leaves it out: `vuka:\"default=button\"` on a string, `vuka:\"default=ToneNeutral\"` naming a constant of the package")
}

// bindDefaults gives the props a tag leaves out their defaults, and those
// it sets with name?={v} the default v stands aside for when zero. A
// problem with a default of another package's props is the tag's, as that
// package may be Go the compiler never checked.
func (e *engine) bindDefaults(c *jsxComp, fields []propField, used map[int]bool, live bool) {
	c.badDefaults = c.badDefaults[:0]
	set := map[string]*compArg{}
	for _, a := range c.args {
		if a.attr != nil && a.attr.omit {
			set[a.key] = a
		}
	}
	for i, fl := range fields {
		if fl.ambiguous != "" || used[i] && set[fl.name+": "] == nil {
			continue
		}
		d, problem := e.fieldDefault(c.target, fl, c.elem || live || c.island)
		if problem != "" {
			if fl.pkg != e.pkg {
				c.badDefaults = append(c.badDefaults, fl.name+": "+problem)
			}
			continue
		}
		if d == nil {
			continue
		}
		c.autoID = c.autoID || d.auto
		if a := set[fl.name+": "]; a != nil {
			a.orDef = d
			continue
		}
		c.args = append(c.args, &compArg{key: fl.name + ": ", typ: fl.typ, nest: fl.nest, text: d.text, dflt: true})
	}
}
