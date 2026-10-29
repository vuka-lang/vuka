package transpile

import (
	"go/ast"
	"strings"
	"unicode"
)

// The cause of an enum's error. A variant of an enum with data whose one
// field of type error is its cause gets an Unwrap method returning it, so
// errors.Is and errors.As see through the variant:
//
//	enum RegisterError { EmailTaken; Database{Err error} }
//
//	func (r RegisterErrorDatabase) Unwrap() error { return r.Err }
//
// A variant declaring its own Unwrap keeps it; one with two fields of type
// error and no Unwrap of its own is an error, since neither is plainly the
// cause.

// lowerEnumCauses writes the Unwrap methods of the package's enums with
// data. It reports whether it wrote any.
func (e *engine) lowerEnumCauses() bool {
	own := map[string]bool{} // Go type names with an Unwrap declared
	for _, f := range e.files {
		for _, decl := range f.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Name.Name != "Unwrap" {
				continue
			}
			t := fd.Recv.List[0].Type
			for {
				switch x := t.(type) {
				case *ast.ParenExpr:
					t = x.X
					continue
				case *ast.StarExpr:
					t = x.X
					continue
				}
				break
			}
			switch x := t.(type) {
			case *ast.Ident:
				own[x.Name] = true
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok {
					own[id.Name+x.Sel.Name] = true
				}
			}
		}
	}
	wrote := false
	for _, f := range e.vuka {
		written := false
		for _, d := range f.enums {
			if !d.sum {
				continue
			}
			for _, v := range d.variants {
				goName := d.name + v.name
				if !v.fields || own[goName] {
					continue
				}
				causes := errorFields(f, goName)
				switch {
				case len(causes) == 1:
				case len(causes) > 1:
					e.errs.add("E0175", f.at(v.off), "%s.%s has %d fields of type error, %s: which is its cause?", d.name, v.name, len(causes), strings.Join(causes, " and ")).
						help("declare func (%s %s.%s) Unwrap() error returning the cause", enumRecv(d.name), d.name, v.name)
					continue
				default:
					continue
				}
				r := enumRecv(d.name)
				w := &f.enumGen
				w.gen("\n", v.off)
				if !f.bare {
					p := f.at(v.off)
					w.gen("//line "+p.Filename+":"+itoa(p.Line)+":"+itoa(p.Column)+"\n", v.off)
				}
				w.gen("// Unwrap returns the cause of "+r+".\n", v.off)
				w.gen("func ("+r+" "+goName+") Unwrap() error { return "+r+"."+causes[0]+" }\n", v.off)
				written = true
			}
		}
		if written {
			f.makeTrailer(e.bare)
			wrote = true
		}
	}
	return wrote
}

// errorFields are the names of the fields of type error of the struct type
// f declares as name.
func errorFields(f *fileState, name string) []string {
	var out []string
	ast.Inspect(f.ast, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != name {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, fl := range st.Fields.List {
			if id, ok := fl.Type.(*ast.Ident); !ok || id.Name != "error" {
				continue
			}
			if len(fl.Names) == 0 {
				out = append(out, "error")
			}
			for _, n := range fl.Names {
				out = append(out, n.Name)
			}
		}
		return false
	})
	return out
}

// enumRecv is the receiver of a generated method on a type of enum:
// its first letter, lower case.
func enumRecv(enum string) string {
	r := []rune(enum)[0]
	if !unicode.IsLetter(r) {
		return "e"
	}
	return string(unicode.ToLower(r))
}
