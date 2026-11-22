//go:build !vuka_release

package vuka

import "reflect"

// reflectMethods is t's method set by reflection, for a type Vuka
// registered no table for. A program built with the vuka_release tag
// leaves it out (methods_release.go): with it, the linker would keep every
// exported method of the program.
func reflectMethods(t reflect.Type) ([]Method, bool) {
	base := t
	if t.Kind() == reflect.Pointer && t.Name() == "" {
		base = t.Elem()
	}
	var out []Method
	for i := range t.NumMethod() {
		m := t.Method(i)
		if !m.IsExported() {
			continue
		}
		decl := m.Func
		if base != t {
			if vm, ok := base.MethodByName(m.Name); ok {
				decl = vm.Func
			}
		}
		out = append(out, Method{Method: m, Decl: decl, Promoted: promotedBy(base, m.Name)})
	}
	return out, true
}

// promotedBy is the index of the field of t whose type has the method
// name, which t's method of that name is then promoted from; nil for none.
func promotedBy(t reflect.Type, name string) []int {
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.Anonymous {
			continue
		}
		if _, ok := reflect.PointerTo(f.Type).MethodByName(name); ok {
			return []int{i}
		}
		if _, ok := f.Type.MethodByName(name); ok {
			return []int{i}
		}
	}
	return nil
}
