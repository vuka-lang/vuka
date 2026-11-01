package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// A stateful component's func prop set to a method value can't be compared at
// run time: a Go method value is a new closure each time. So a tag records
// which method it binds to which receiver, for a live session to count the
// same one as the prop being unchanged:
//
//	<OrderRow order={o} onCancel={p.Cancel}/>
//	→  ui.MethodProps(&OrderRow{Order: o, OnCancel: p.Cancel}, ui.Method("OnCancel", p, "Cancel"))
//
// The receiver written is the one the value is really bound to, the embedded
// fields a promoted method is reached through spelt out: &o for a pointer
// method of a variable, *p for a value method through a pointer. Only an
// expression that reads the same twice is recorded; anything else (a call, a
// tracked field, a type parameter's method, a method expression, a closure)
// stays what it is today, a prop that always counts as changed.

// methodProp is a func prop's method value.
type methodProp struct {
	field, method string
	pre, post     string // around the receiver: "&", "*", the embedded fields
	x             span   // the receiver's expression as written
	text          string // or, when it isn't all written (an implicit receiver), its text
}

// findMethodProps sets c's methods, once its expressions' types are final.
func (e *engine) findMethodProps(f *fileState, c *jsxComp) {
	c.methods = nil
	if c.dead || !c.stateful || c.island || !c.target.has("MethodProps") {
		return
	}
	for _, a := range c.args {
		at := a.attr
		if at == nil || at.kind != 'e' || at.stmts || at.bind != nil || at.write != nil || at.ref {
			continue
		}
		if len(a.nest) > 0 || a.wrap != "" || a.conv != "" || a.key == "" || a.typ == nil {
			continue
		}
		if _, ok := a.typ.Underlying().(*types.Signature); !ok {
			continue
		}
		if m := e.methodValue(f, e.attrExpr(f, at)); m != nil {
			m.field = strings.TrimSuffix(a.key, ": ")
			c.methods = append(c.methods, m)
		}
	}
}

// methodValue describes x when it is a method value whose receiver reads the
// same twice.
func (e *engine) methodValue(f *fileState, x ast.Expr) *methodProp {
	if x == nil {
		return nil
	}
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal || !e.rereadable(sel.X) {
		return nil
	}
	fn, ok := s.Obj().(*types.Func)
	if !ok {
		return nil
	}
	m := &methodProp{method: fn.Name()}
	t := s.Recv()
	idx := s.Index()
	for _, i := range idx[:len(idx)-1] {
		if p, ok := t.Underlying().(*types.Pointer); ok {
			t = p.Elem()
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok || i >= st.NumFields() {
			return nil
		}
		fl := st.Field(i)
		m.post += "." + fl.Name()
		t = fl.Type()
	}
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return nil
	}
	_, heldPtr := t.Underlying().(*types.Pointer)
	if _, iface := t.Underlying().(*types.Interface); !iface {
		_, ptrMethod := fn.Signature().Recv().Type().Underlying().(*types.Pointer)
		switch {
		case ptrMethod && !heldPtr:
			m.pre = "&"
		case !ptrMethod && heldPtr:
			m.pre = "*"
		}
	}
	m.x = span{f.orig(sel.X.Pos()), f.orig(sel.X.End())}
	if m.x.start >= m.x.end || !sameWritten(f, m.x, sel.X) {
		m.text = types.ExprString(sel.X)
	}
	return m
}

// sameWritten reports whether the source at sp is x as this round reads it,
// no name of it put there by a rewrite.
func sameWritten(f *fileState, sp span, x ast.Expr) bool {
	return string(f.src[sp.start:sp.end]) == types.ExprString(x)
}

// rereadable reports whether x has no calls and is no tracked field's: read
// again it gives what it gave, and it can be written twice.
func (e *engine) rereadable(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident:
		_, ok := e.info.Uses[x].(*types.Var)
		return ok
	case *ast.ParenExpr:
		return e.rereadable(x.X)
	case *ast.StarExpr:
		return e.rereadable(x.X)
	case *ast.SelectorExpr:
		if s := e.info.Selections[x]; s != nil {
			return s.Kind() == types.FieldVal && !isAssign(s.Type()) && e.rereadable(x.X)
		}
		_, ok := e.info.Uses[x.Sel].(*types.Var)
		return ok // a package's variable
	case *ast.IndexExpr:
		switch i := x.Index.(type) {
		case *ast.BasicLit:
			if i.Kind != token.INT && i.Kind != token.STRING {
				return false
			}
		case *ast.Ident:
			if !e.rereadable(i) {
				return false
			}
		default:
			return false
		}
		tv, ok := e.info.Types[x.X]
		return ok && !tv.IsType() && e.rereadable(x.X)
	}
	return false
}

// methodProps writes the rest of ui.MethodProps(&T{…}: what comp wrote the
// start of.
func (w *jsxWriter) methodProps(c *jsxComp) {
	for _, m := range c.methods {
		w.gen(", " + w.q + "Method(" + strconv.Quote(m.field) + ", " + m.pre)
		if m.text != "" {
			w.gen(m.text)
		} else {
			w.copy(m.x)
		}
		w.gen(m.post + ", " + strconv.Quote(m.method) + ")")
	}
	if len(c.methods) > 0 {
		w.gen(")")
	}
}
