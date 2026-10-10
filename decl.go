package vuka

import "reflect"

// Decl describes a declaration for a declarer: a decorator of type
// func(d *vuka.Decl), which runs once at program start rather than around each
// call. Declarers register things — routes, commands, jobs — from the
// declarations they decorate:
//
//	func Get(path string) func(*vuka.Decl) {
//		return func(d *vuka.Decl) { routes[path] = d.Func }
//	}
type Decl struct {
	Name  string // "main.ShowPet", "PetAdmin.Index"
	Pkg   string // the package's import path
	File  string // the source file's base name, and the line of the name, for messages
	Line  int
	Attrs []any // the declaration's typed attributes
	// Func is the function, wrapped by the declaration's other decorators, so
	// calling it runs them too. For a method it is the method expression:
	// func(recv, args…).
	Func     any
	Recv     reflect.Type // the receiver type, for a method
	Params   []Param      // the parameters, receiver excluded
	Results  []reflect.Type
	Variadic bool
}

// Param is a parameter of a declared function: its name as written ("" when
// unnamed), type, and the typed attributes written before it:
//
//	func Show(@Path("pet_id") id int)
type Param struct {
	Name  string
	Type  reflect.Type
	Attrs []any
}

// Attr fills ptr with the parameter's typed attribute of ptr's element type,
// reporting whether there is one: var p Path; param.Attr(&p).
func (p Param) Attr(ptr any) bool { return fillAttr(p.Attrs, ptr) }

// Attr fills ptr with the declaration's typed attribute of ptr's element type,
// reporting whether there is one: var r Route; d.Attr(&r).
func (d *Decl) Attr(ptr any) bool { return fillAttr(d.Attrs, ptr) }

// FuncDecl describes a function for its declarers, given its parameters'
// names, or with d.Params holding each parameter's name and attributes.
// Generated code calls it.
func FuncDecl(d Decl, params ...string) *Decl { return d.describe(0, params) }

// MethodDecl describes a method, d.Func being its method expression.
// Generated code calls it.
func MethodDecl(d Decl, params ...string) *Decl { return d.describe(1, params) }

func (d Decl) describe(skip int, names []string) *Decl {
	t := reflect.TypeOf(d.Func)
	if skip == 1 {
		d.Recv = t.In(0)
	}
	given := d.Params
	d.Params = nil
	for i := skip; i < t.NumIn(); i++ {
		var p Param
		if i-skip < len(given) {
			p = given[i-skip]
		}
		p.Type = t.In(i)
		if i-skip < len(names) {
			p.Name = names[i-skip]
		}
		d.Params = append(d.Params, p)
	}
	for i := 0; i < t.NumOut(); i++ {
		d.Results = append(d.Results, t.Out(i))
	}
	d.Variadic = t.IsVariadic()
	return &d
}
