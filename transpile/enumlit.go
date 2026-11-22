package transpile

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// A quoted value set on a component's prop of a defined string type, or a
// number on one of a defined integer type, is one of the constants the
// type's package declares of it, by value: variant="outline" for
// ButtonOutline ButtonVariant = "outline". A type declaring none takes any
// value, as does one whose doc comment has a //vuka:open line: a few
// presets of a type that takes others too.

// Positioner is an Importer that says where the objects of the packages it
// imports are declared; the compiler reads a type's doc comment there.
type Positioner interface {
	Position(obj types.Object) token.Position
}

// checkEnumLit reports a literal a's prop's type declares no constant of
// (E0196).
func (e *engine) checkEnumLit(f *fileState, c *jsxComp, a *compArg) {
	at := a.attr
	if at == nil || a.nodeText || at.ref || at.idStr || at.model || at.event || at.stmts || at.of || at.bind != nil {
		return
	}
	typ := a.typ
	if in := optionValue(typ); in != nil {
		typ = in
	}
	named, _ := types.Unalias(typ).(*types.Named)
	if named == nil || named.TypeArgs().Len() > 0 || named.Obj().Pkg() == nil {
		return
	}
	basic, _ := named.Underlying().(*types.Basic)
	if basic == nil {
		return
	}
	var lit constant.Value
	var text string
	var from, to int
	switch {
	case at.kind == 's' && basic.Info()&types.IsString != 0 && at.quote.end > 0:
		lit, text, from, to = constant.MakeString(at.val), strconv.Quote(at.val), at.quote.start, at.quote.end
	case at.kind == 'e' && basic.Info()&types.IsInteger != 0:
		lit = intConst(e.attrExpr(f, at))
		if lit == nil {
			return
		}
		text, from, to = lit.ExactString(), at.expr.start, at.expr.end
	default:
		return
	}
	consts := enumConsts(named)
	if len(consts) == 0 {
		return
	}
	for _, k := range consts {
		if constant.Compare(k.Val(), token.EQL, lit) {
			return
		}
	}
	if e.openEnum(named.Obj()) {
		return
	}
	show := func(v constant.Value) string {
		if v.Kind() == constant.String {
			return strconv.Quote(constant.StringVal(v))
		}
		return v.ExactString()
	}
	tname := e.display(named)
	var values []string
	for _, k := range consts {
		values = append(values, show(k.Val()))
	}
	pos := f.at(from)
	written := text
	if at.kind == 'e' {
		written = "{" + text + "}"
	}
	err := e.errs.add("E0196", pos, "<%s %s=%s>: %s is no %s", c.el.tag, at.name, written, text, tname)
	err.Len = to - from
	near := ""
	for _, k := range consts {
		if lit.Kind() == constant.String && strings.EqualFold(constant.StringVal(lit), k.Name()) {
			near = show(k.Val()) // the constant's name, for its value
		}
	}
	if near == "" && lit.Kind() == constant.String {
		var vals []string
		for _, k := range consts {
			vals = append(vals, constant.StringVal(k.Val()))
		}
		if s := closestName(constant.StringVal(lit), vals); s != "" {
			near = strconv.Quote(s)
		}
	}
	if near != "" {
		err.suggest(near)
	}
	list := values
	if len(list) > 8 {
		list = append(list[:8:8], "…")
	}
	err.help("%s takes the value of one of its constants: %s, or {%s(x)} for any other", tname, strings.Join(list, ", "), tname)
}

// intConst is the integer a literal expression writes (3, -1), nil for any
// other expression.
func intConst(x ast.Expr) constant.Value {
	neg := false
	if u, ok := ast.Unparen(x).(*ast.UnaryExpr); ok && (u.Op == token.SUB || u.Op == token.ADD) {
		neg, x = u.Op == token.SUB, u.X
	}
	lit, ok := ast.Unparen(x).(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return nil
	}
	v := constant.MakeFromLiteral(lit.Value, token.INT, 0)
	if neg {
		v = constant.UnaryOp(token.SUB, v, 0)
	}
	return v
}

// enumConsts are the exported constants of type t its package declares, in
// declaration order.
func enumConsts(t *types.Named) []*types.Const {
	scope := t.Obj().Pkg().Scope()
	var out []*types.Const
	for _, n := range scope.Names() {
		if k, ok := scope.Lookup(n).(*types.Const); ok && k.Exported() && types.Identical(k.Type(), t) {
			out = append(out, k)
		}
	}
	// by position: Names is sorted by name
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Pos() < out[j-1].Pos(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// openEnum reports whether a type's doc comment has a //vuka:open line. A
// type of another package whose source can't be read (a -trimpath build's)
// counts as open: the check is left out rather than guessed.
func (e *engine) openEnum(obj *types.TypeName) bool {
	if open, ok := e.openEnums[obj]; ok {
		return open
	}
	open := true
	if f := e.fileOf(obj.Pos()); obj.Pkg() == e.pkg && f != nil {
		open = docHasOpen(f.src, f.orig(obj.Pos()))
	} else if p := e.positionOf(obj); p.Filename != "" {
		if src, err := os.ReadFile(p.Filename); err == nil {
			if off, ok := declLine(src, p.Line, obj.Name()); ok {
				open = docHasOpen(src, off)
			}
		}
	}
	if e.openEnums == nil {
		e.openEnums = map[*types.TypeName]bool{}
	}
	e.openEnums[obj] = open
	return open
}

func (e *engine) positionOf(obj types.Object) token.Position {
	if p, ok := e.imp.(Positioner); ok {
		return p.Position(obj)
	}
	if e.impFset != nil && e.impFset.File(obj.Pos()) != nil {
		return e.impFset.Position(obj.Pos())
	}
	return token.Position{}
}

// declLine is the offset of the line of src declaring the type name: line
// (from 1), when it does, else the first `type name` line.
func declLine(src []byte, line int, name string) (int, bool) {
	lines := strings.SplitAfter(string(src), "\n")
	if line > 0 && line <= len(lines) {
		if regexp.MustCompile(`^\s*(?:type\s+)?` + regexp.QuoteMeta(name) + `\b`).MatchString(lines[line-1]) {
			off := 0
			for _, l := range lines[:line-1] {
				off += len(l)
			}
			return off, true
		}
	}
	if m := regexp.MustCompile(`(?m)^\s*type\s+` + regexp.QuoteMeta(name) + `\b`).FindIndex(src); m != nil {
		return m[0], true
	}
	return 0, false
}

// docHasOpen reports whether the comment lines just above the line holding
// off — and, for a spec of a type (…) group, those above the group — have
// a //vuka:open line.
func docHasOpen(src []byte, off int) bool {
	lines := strings.Split(string(src[:min(off, len(src))]), "\n")
	i := len(lines) - 2 // the line before off's
	for {
		for ; i >= 0 && strings.HasPrefix(strings.TrimSpace(lines[i]), "//"); i-- {
			if d, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "//vuka:open"); ok && (d == "" || d[0] == ' ' || d[0] == '\t') {
				return true
			}
		}
		if i < 0 || !strings.HasPrefix(strings.TrimSpace(lines[i]), "type") || !strings.HasSuffix(strings.TrimSpace(lines[i]), "(") {
			return false
		}
		i--
	}
}
