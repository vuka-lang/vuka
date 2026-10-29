package transpile

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

// Match is a match statement, for the editor: its braces, its subject's
// type as the source has it ((T, error) for a call returning one), the
// patterns that fit the subject as snippets, and the Ok, Err, Some and
// None its patterns name.
type Match struct {
	LBrace, RBrace int
	Subject        string
	Cases          []string
	Ctors          []MatchCtor
}

// MatchCtor is an Ok, Err, Some or None in a pattern: Off and End its
// name, Of the type it matches, Binds what its argument binds ("" for None).
type MatchCtor struct {
	Off, End int
	Name, Of string
	Binds    string
}

// matchesOf are f's match statements, at their offsets in the file as written.
func (f *fileState) matchesOf() []Match {
	out := slices.Clone(f.matchInfos)
	if f.html != nil {
		for i := range out {
			m := &out[i]
			m.LBrace, m.RBrace = f.html.toSrc(m.LBrace), f.html.toSrc(m.RBrace)
			m.Ctors = slices.Clone(m.Ctors)
			for j := range m.Ctors {
				c := &m.Ctors[j]
				c.Off, c.End = f.html.toSrc(c.Off), f.html.toSrc(c.End)
			}
		}
	}
	return out
}

// srcOff is the offset in the file as written of off in f.src.
func (f *fileState) srcOff(off int) int {
	if f.html != nil {
		return f.html.toSrc(off)
	}
	return off
}

// snippets are the patterns of t's constructors and _, as editor snippets.
func (pc *patCtx) snippets(t types.Type, tuple *matchTuple) []string {
	var out []string
	for _, c := range ctors(t) {
		switch {
		case c.variant != "":
			name := pc.e.display(t) + "." + c.variant
			if len(c.fields) > 0 {
				name += "{${1}}"
			}
			out = append(out, name)
		case c.name == "Ok", c.name == "Some":
			v := "v"
			if tuple != nil {
				v = tuple.val
			} else if n := typeVarName(c.args[0]); n != "" {
				v = n
			}
			out = append(out, c.name+"(${1:"+v+"})")
		case c.name == "Err":
			out = append(out, "Err(${1:err})")
		case c.name == "{}":
		default:
			out = append(out, c.name)
		}
	}
	return append(out, "_")
}

// notExhaustive reports a match that leaves out the values cases would
// match, with the edit adding those cases: an Err(_) left out returns
// the error when its function can, the others are left to write.
func (e *engine) notExhaustive(f *fileState, m *matchStmt, pc *patCtx, head ast.Node, t types.Type, missing string, cases []string) {
	m.dead = true
	er := e.errs.add("E0113", f.at(m.start), "match on %s isn't exhaustive: %s", pc.of, missing)
	er.Len = m.subj.end - m.start
	data := map[string]bool{}
	for _, c := range ctors(t) {
		if c.variant != "" && len(c.fields) > 0 {
			data[pc.e.display(t)+"."+c.variant] = true
		}
	}
	errName := "e"
	for _, n := range []string{"e", "err", "err2", "err3"} {
		if errName = n; pc.lookup(n) == nil {
			break
		}
	}
	var pats, bodies, shown []string
	for _, c := range cases {
		p, body := c, "// TODO: handle "+c
		switch {
		case data[c]:
			p += "{}"
		case c == "Err(_)":
			p = "Err(" + errName + ")"
			if ret := e.propagate(f, head, errName); ret != "" {
				body = ret
			}
		}
		pats, bodies = append(pats, p), append(bodies, body)
		if len(shown) < 3 {
			shown = append(shown, "case "+p+": …")
		} else if len(shown) == 3 {
			shown = append(shown, "…")
		}
	}
	if len(pats) == 1 && pats[0] == "_" {
		er.Help = "add case _:"
	} else {
		er.Help = "add " + strings.Join(shown, ", ") + ", or case _:"
	}
	indent := func(off int) string {
		ls := lineStart(f.src, off)
		ws := string(f.src[ls:off])
		if strings.TrimLeft(ws, " \t") != "" {
			return ""
		}
		return ws
	}
	caseIndent := indent(m.start) + "\t"
	if len(m.cases) > 0 {
		caseIndent = indent(m.cases[0].start)
	}
	var b strings.Builder
	for i, p := range pats {
		b.WriteString(caseIndent + "case " + p + ":\n" + caseIndent + "\t" + bodies[i] + "\n")
	}
	at, text := m.rbrace, "\n"+b.String()+indent(m.start)
	if ls := lineStart(f.src, m.rbrace); strings.TrimLeft(string(f.src[ls:m.rbrace]), " \t") == "" {
		at, text = ls, b.String()
	}
	er.FixTitle = "Add missing cases"
	er.Edits = []Edit{{f.srcOff(at), f.srcOff(at), text}}
}

// propagate is the statement returning err from the function holding n,
// or "" when it can't: return Err(err) from one returning a Result, the
// zero values and err from one whose last result is an error.
func (e *engine) propagate(f *fileState, n ast.Node, err string) string {
	var sig *types.Signature
	for p := f.parents[n]; p != nil && sig == nil; p = f.parents[p] {
		switch fn := p.(type) {
		case *ast.FuncDecl:
			if obj, ok := e.info.Defs[fn.Name].(*types.Func); ok {
				sig, _ = obj.Type().(*types.Signature)
			}
		case *ast.FuncLit:
			sig, _ = e.info.Types[fn].Type.(*types.Signature)
		}
	}
	if sig == nil {
		return ""
	}
	res := sig.Results()
	if res.Len() == 1 {
		if kind, _ := runtimeType(res.At(0).Type()); kind == "Result" {
			return "return Err(" + err + ")"
		}
	}
	if res.Len() == 0 || !types.Identical(res.At(res.Len()-1).Type(), errorType) {
		return ""
	}
	var vals []string
	for i := range res.Len() - 1 {
		z := e.zeroText(f, res.At(i).Type())
		if z == "" {
			return ""
		}
		vals = append(vals, z)
	}
	return "return " + strings.Join(append(vals, err), ", ")
}

// zeroText is t's zero value as the source writes it, or "".
func (e *engine) zeroText(f *fileState, t types.Type) string {
	switch kind, _ := runtimeType(t); kind {
	case "Option":
		return "None"
	case "Result":
		return ""
	}
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return ""
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "false"
		case u.Info()&types.IsString != 0:
			return `""`
		case u.Info()&types.IsNumeric != 0:
			return "0"
		case u.Kind() == types.UnsafePointer:
			return "nil"
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil"
	case *types.Struct, *types.Array:
		if text, msg := e.typeText(f, t); msg == "" {
			return text + "{}"
		}
	}
	return ""
}
