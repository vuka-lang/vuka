package transpile

import (
	"fmt"
	"go/ast"
	"strings"
)

// exports generates a wrapper for every @export: a function (or method) with the
// exported name and the annotated declaration's signature, calling it. It is how
// Go code reaches one overload by a stable name.
func (f *fileState) exports(bare bool, errs *ErrorList) string {
	var b strings.Builder
	for _, a := range f.attrs {
		if a.kind != attrExport {
			continue
		}
		fd, ok := a.decl.(*ast.FuncDecl)
		if !ok {
			errs.add(a.Pos, "@export applies to functions and methods")
			continue
		}
		if a.value == fd.Name.Name {
			errs.add(a.Pos, "@export(%q) names the function itself", a.value)
			continue
		}
		b.WriteString("\n")
		for _, d := range f.attrs {
			if d.decl == a.decl && (d.kind == attrDoc || d.kind == attrDeprecated) {
				b.WriteString(d.replacementLines() + "\n")
			}
		}
		b.WriteString("func ")
		if !bare {
			b.WriteString(lineDirective(a.Pos))
		}
		call := ""
		if fd.Recv != nil {
			fmt.Fprintf(&b, "(r %s) ", f.text(fd.Recv.List[0].Type))
			call = "r."
		}
		call += fd.Name.Name
		b.WriteString(a.value)
		if tp := fd.Type.TypeParams; tp != nil {
			b.WriteString(f.text(tp))
			var names []string
			for _, field := range tp.List {
				for _, n := range field.Names {
					names = append(names, n.Name)
				}
			}
			call += "[" + strings.Join(names, ", ") + "]"
		}
		var params, args []string
		for _, field := range fd.Type.Params.List {
			for range max(1, len(field.Names)) {
				name := "a" + itoa(len(params))
				params = append(params, name+" "+f.text(field.Type))
				if _, ok := field.Type.(*ast.Ellipsis); ok {
					name += "..."
				}
				args = append(args, name)
			}
		}
		fmt.Fprintf(&b, "(%s)", strings.Join(params, ", "))
		ret := ""
		if res := fd.Type.Results; res != nil {
			fmt.Fprintf(&b, " %s", f.text(res))
			ret = "return "
		}
		fmt.Fprintf(&b, " { %s%s(%s) }\n", ret, call, strings.Join(args, ", "))
	}
	return b.String()
}

// replacementLines is a doc attribute as // comment lines.
func (a *Attr) replacementLines() string {
	v := a.value
	if a.kind == attrDeprecated {
		v = "Deprecated: " + v
	}
	return commentLines(strings.Split(v, "\n"))
}
