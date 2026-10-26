package check

import (
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Forms checks struct forms (a struct embedding ui.FormState) as the form
// reads them: a Clean<Field> or <Field>Choices method naming no field of
// the form, which never runs (W0309); a field with a placeholder and no
// label, a placeholder standing in for one (W0310); and a database model
// embedded in the form, which makes every column of it something a submit
// may set (W0311).
var Forms = &analysis.Analyzer{
	Name: "forms",
	Doc:  "check struct forms: their Clean and Choices methods, placeholders and embedded models",
	URL:  docsURL,
	Run:  runForms,
}

func runForms(pass *analysis.Pass) (any, error) {
	scope := pass.Pkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok || !embedsFormState(st) {
			continue
		}
		checkForm(pass, tn, st)
	}
	return nil, nil
}

func embedsFormState(st *types.Struct) bool {
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Embedded() && is(f.Type(), uiPath, "FormState") {
			return true
		}
	}
	return false
}

func checkForm(pass *analysis.Pass, tn *types.TypeName, st *types.Struct) {
	form := tn.Name()
	var paths []string
	formFields(st, "", map[*types.Struct]bool{}, &paths)
	known := map[string]bool{}
	for _, p := range paths {
		known[strings.ReplaceAll(p, ".", "")] = true
	}
	ms := types.NewMethodSet(types.NewPointer(tn.Type()))
	for i := 0; i < ms.Len(); i++ {
		fn, ok := ms.At(i).Obj().(*types.Func)
		if !ok || len(ms.At(i).Index()) > 1 || fn.Pkg() != pass.Pkg {
			continue // promoted (FormState's), or another package's
		}
		name := fn.Name()
		var field, kind string
		switch {
		case name != "Clean" && strings.HasPrefix(name, "Clean"):
			field, kind = strings.TrimPrefix(name, "Clean"), "Clean"
		case name != "Choices" && strings.HasSuffix(name, "Choices"):
			field, kind = strings.TrimSuffix(name, "Choices"), "Choices"
		default:
			continue
		}
		if known[field] {
			continue
		}
		names := make([]string, 0, len(known))
		for k := range known {
			names = append(names, k)
		}
		sort.Strings(names)
		help := "name a field: " + kind + " methods are " + strings.Join(methodNames(kind, names), ", ")
		if s := nearest(field, names); s != "" {
			help = "rename it " + methodName(kind, s)
		}
		report(pass, at(pass, fn, span{fn.Pos(), fn.Pos()}), "W0309", help, "%s.%s names no field of %s, so it never runs", form, name, form)
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i))
		if f.Embedded() {
			if model := embeddedModel(f.Type()); model != "" {
				report(pass, at(pass, f, span{f.Pos(), f.Pos()}), "W0311", "declare the fields the form edits in the form, and copy them to the model in the submit handler",
					"%s embeds %s, a database model: a submit may set every column of it", form, model)
			}
			continue
		}
		if !f.Exported() || strings.HasPrefix(tag.Get("form"), "-") {
			continue
		}
		if tag.Get("placeholder") != "" && tag.Get("label") == "" {
			report(pass, at(pass, f, span{f.Pos(), f.Pos()}), "W0310", `give it a label: label:"…"; a placeholder goes as the visitor types, and is no label`,
				"%s.%s has a placeholder and no label", form, f.Name())
		}
	}
}

// formFields are a form struct's fields' Go paths: Email, Address.City.
func formFields(st *types.Struct, prefix string, seen map[*types.Struct]bool, out *[]string) {
	if seen[st] {
		return
	}
	seen[st] = true
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if is(f.Type(), uiPath, "FormState") {
			continue
		}
		inner, _ := deref(f.Type()).Underlying().(*types.Struct)
		if f.Embedded() {
			if inner != nil {
				formFields(inner, prefix, seen, out)
			}
			continue
		}
		if !f.Exported() {
			continue
		}
		path := f.Name()
		if prefix != "" {
			path = prefix + "." + path
		}
		*out = append(*out, path)
		if inner != nil && !named(f.Type(), "time", "Time") {
			formFields(inner, path, seen, out)
		}
	}
}

// embeddedModel is the database model t is, or embeds: a struct embedding
// orm.Model[T]; "" when it is none.
func embeddedModel(t types.Type) string {
	t = deref(t)
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == ormPath && n.Obj().Name() == "Model" {
		return "orm.Model"
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return ""
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Embedded() {
			continue
		}
		if n, ok := types.Unalias(deref(f.Type())).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == ormPath && n.Obj().Name() == "Model" {
			if tn, ok := types.Unalias(t).(*types.Named); ok {
				return tn.Obj().Name()
			}
			return "a model"
		}
	}
	return ""
}

func deref(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func methodName(kind, field string) string {
	if kind == "Clean" {
		return "Clean" + field
	}
	return field + "Choices"
}

func methodNames(kind string, fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = methodName(kind, f)
	}
	return out
}
