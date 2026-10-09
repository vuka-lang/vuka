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
// unnamed) and type.
type Param struct {
	Name string
	Type reflect.Type
}

// Attr fills ptr with the declaration's typed attribute of ptr's element type,
// reporting whether there is one: var r Route; d.Attr(&r).
func (d *Decl) Attr(ptr any) bool { return fillAttr(d.Attrs, ptr) }

// FuncDecl describes a function for its declarers, given its parameters'
// names. Generated code calls it.
func FuncDecl(d Decl, params ...string) *Decl { return d.describe(0, params) }

// MethodDecl describes a method, d.Func being its method expression.
// Generated code calls it.
func MethodDecl(d Decl, params ...string) *Decl { return d.describe(1, params) }

func (d Decl) describe(skip int, names []string) *Decl {
	t := reflect.TypeOf(d.Func)
	if skip == 1 {
		d.Recv = t.In(0)
	}
	for i := skip; i < t.NumIn(); i++ {
		p := Param{Type: t.In(i)}
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
