# Attributes

**Go:** metadata lives in struct tags and comments (`//go:generate`, doc text).

**Vuka adds** attributes: `@` before a declaration. An attribute is either built
in, or any Go type — so its arguments are type-checked Go.

<!-- go:context
type User struct{ Name string }
-->

::: vuka
```vuka
type Route struct{ Method, Path string }

@doc("Lists the users")
@Route{Method: "GET", Path: "/users"}
func listUsers() []User { … }

@deprecated("use the shapes service")
func handler() string { … }
```

| Built-in | Becomes |
|---|---|
| `@doc("…")` | the declaration's doc comment |
| `@deprecated("…")` | a `Deprecated:` paragraph |
| `@export("Name")` | a wrapper named `Name`, for Go code to call |

A typed attribute (`@Route{…}`, `@Perm("orders.write")`) is checked by Go: a
wrong field or type is a compile error on the `@` line.
:::

::: go
In Go, the built-ins are what they become — a doc comment, a `Deprecated:`
paragraph, an exported wrapper you write — and a typed attribute is a value
registered for its declaration at init with `vuka.RegisterFunc(fn, attrs…)`
(a method by its method expression, `(*Board).Clear`) or
`vuka.RegisterType[T](attrs…)`. Being values, they are checked by the
compiler like any other:

```go
type Route struct{ Method, Path string }

// Lists the users
func listUsers() []User { return nil }

// Deprecated: use the shapes service
func handler() string { return "" }

func init() {
	vuka.RegisterFunc(listUsers, Route{Method: "GET", Path: "/users"})
}
```

Registration calls such as `web.Routes.Get(path, fn, opts…)` take
attributes as their options too: any value with an `Attrs() []any` method
becomes the route's.
:::

Attributes are readable
at run time by [decorators](/features/decorators) through `c.Attr(&x)`, and
from anywhere through [the registry](#reading-attributes-from-a-value).

<span class="vuka-only">An attribute that names a *function* rather than a type is a decorator.</span>

::: vuka
Parameters take them before the parameter — `func show(@Path("id") id int)` —
see [Parameter attributes](#parameter-attributes); and an attribute type can
restrict where it goes: see [Targets](#targets).

Struct fields take typed attributes too, after the field:
`Title string @Char{Max: 200}`. See
[Field attributes and references](/features/fields).

A field of type `vuka.File` set to a string literal names a file that is
checked at compile time and embedded: see [Files](/features/decorators#files).
:::

::: go
Parameters have attributes too, given with the function to whatever reads
them — see [Parameter attributes](#parameter-attributes) — and struct fields,
given to the field's schema or registered by field name: see
[Field attributes and references](/features/fields).
:::

## Reading attributes from a value

<span class="vuka-only">Vuka records, at program start, the typed attributes of every function,
method and type that has any (a bundle's count), so</span><span class="go-only">With the attributes registered at init,</span>
a library holding only a
value — a method value it was handed as a handler, a `reflect.Type` — reads
what was written on its declaration, without calling it:

::: vuka
```vuka
type Gate struct{ Perm string }

@Gate{"board.delete"}
func (b *Board) Clear() { … }

ref, _ := vuka.FuncOf(b.Clear)       // a method value, or (*Board).Clear
var g Gate
ref.Attrs.Attr(&g)                   // g.Perm == "board.delete"; ref.Recv is Board
vuka.MethodAttrs(reflect.TypeFor[Board](), "Clear")
vuka.TypeAttrs(reflect.TypeFor[Board]())
```
:::

::: go
```go
import "reflect"

type Gate struct{ Perm string }

type Board struct{ cards []string }

func (b *Board) Clear() { b.cards = nil }

func init() {
	vuka.RegisterFunc((*Board).Clear, Gate{"board.delete"})
}

func clearGate(b *Board) string {
	ref, _ := vuka.FuncOf(b.Clear) // a method value, or (*Board).Clear
	var g Gate
	ref.Attrs.Attr(&g) // g.Perm == "board.delete"; ref.Recv is Board
	vuka.MethodAttrs(reflect.TypeFor[Board](), "Clear")
	vuka.TypeAttrs(reflect.TypeFor[Board]())
	return g.Perm
}
```
:::

| | |
|---|---|
| `vuka.FuncOf(fn)` | the declaration of a function, method expression or method value: `Recv` and `Method` for a method, its `Attrs`; `Literal` for a function literal, which has none |
| `vuka.MethodAttrs(t, name)` | a method's attributes (its own; its type's are `TypeAttrs`) |
| `vuka.TypeAttrs(t)` | a type's attributes |
| `attrs.Attr(&x)` | fills `x` with the attribute of its type, as `c.Attr` does |

The lookups are a map read (`FuncOf` caches its answer per function). Not
recorded: generic functions and types (there is no one function to name),
and method values made by reflection (`reflect.Value.Method`), which name no
declaration — look those up with `MethodAttrs`. Go code registers a
declaration of its own with `vuka.RegisterFunc(fn, attrs…)` and
`vuka.RegisterType[T](attrs…)`<span class="vuka-only">, what the generated code calls</span>. Its
`vuka.Call` also lists every attribute: `c.Attrs()`.

## Parameter attributes

Parameters take typed attributes too, <span class="vuka-only">written before the parameter, </span>for code
that needs to know something about each argument: where a web route reads it
from, which arguments to validate:

::: vuka
```vuka
type Path string
type Query string
type Body struct{}
type Valid struct{}

@web.Get("/pets/{pet_id}")
func showPet(@Path("pet_id") id int, @Query("format") format string, store *Store) Result[Pet] { … }

@web.Post("/pets")
@valid
func addPet(@Body @Valid in NewPet, store *Store) Result[Pet] { … }
```

They are values, checked like any typed attribute, and several may precede a
parameter; an unnamed parameter takes them too (`func f(@Query("q") string)`).
They go on top-level functions and methods, not on the receiver, results or
function literals.
:::

::: go
In Go they go with the function, parameter by parameter, to what reads them:
a `vuka.Param{Name, Attrs}` per parameter for `vuka.DeclOf` (a declarer's
description), and `vuka.FuncInfo.ParamAttrs` for the call decorators
`vuka.Wrap` puts around it. With a small route registry of your own:

<!-- go:context
type Store struct{ pets map[int]Pet }

type Pet struct{ Name string }

type NewPet struct{ Name string }

type Validator interface{ Validate() error }

var routes = map[string]*vuka.Decl{}

func Get(path string) func(*vuka.Decl) {
	return func(d *vuka.Decl) { routes["GET "+path] = d }
}

func Post(path string) func(*vuka.Decl) {
	return func(d *vuka.Decl) { routes["POST "+path] = d }
}
-->

```go
type Path string
type Query string
type Body struct{}
type Valid struct{}

func showPet(id int, format string, store *Store) (Pet, error) {
	return store.pets[id], nil
}

var addPetDec vuka.Decorated[func(NewPet, *Store) (Pet, error)]

func addPet(in NewPet, store *Store) (Pet, error) {
	return addPetDec.Get(func() func(NewPet, *Store) (Pet, error) {
		return vuka.Wrap(storePet, vuka.FuncInfo{
			Name:       "main.addPet",
			ParamAttrs: [][]any{{Body{}, Valid{}}, nil},
		}, valid)
	})(in, store)
}

func storePet(in NewPet, store *Store) (Pet, error) {
	p := Pet{Name: in.Name}
	store.pets[len(store.pets)+1] = p
	return p, nil
}

func init() {
	Get("/pets/{pet_id}")(vuka.DeclOf(showPet,
		vuka.Param{Name: "id", Attrs: []any{Path("pet_id")}},
		vuka.Param{Name: "format", Attrs: []any{Query("format")}},
		vuka.Param{Name: "store"}))
	Post("/pets")(vuka.DeclOf(addPet,
		vuka.Param{Name: "in", Attrs: []any{Body{}, Valid{}}},
		vuka.Param{Name: "store"}))
}
```

The web framework's own routes bind scalars by name instead, the names
recorded by the compiler: `vuka.RegisterParams(showPet, "pet_id", "format", "store")`
and `web.Routes.Get("/pets/{pet_id}", showPet)`.
:::

A [declarer](/features/decorators#declaration-decorators)
reads them from `d.Params[i].Attrs`, or with `d.Params[i].Attr(&x)`; a call
decorator with `c.ParamAttr(i, &x)`, `i` indexing `c.Args`:

::: vuka
```vuka
decorator valid(c) {
	for i, arg := range c.Args {
		var v Valid
		if c.ParamAttr(i, &v) {
			if err := arg.(Validator).Validate(); err != nil {
				c.SetErr(err)
				return
			}
		}
	}
	c.Next()
}
```
:::

::: go
```go
func valid(c *vuka.Call) {
	for i, arg := range c.Args {
		var v Valid
		if c.ParamAttr(i, &v) {
			if err := arg.(Validator).Validate(); err != nil {
				c.SetErr(err)
				return
			}
		}
	}
	c.Next()
}
```
:::

## Targets

Some attributes only make sense in one place — a column setting belongs on a
field, a route's path source on a parameter. The attribute type can say so,
and writing it anywhere else becomes a compile error:

::: vuka
```vuka
@vuka.Targets(vuka.OnField)
type Char struct{ Max int }

@vuka.Targets(vuka.OnParam | vuka.OnField)
type Path string
```

Using it anywhere else is a compile error at the attribute:
`@Char can't go on a function; Char is for fields`. The targets are
`vuka.OnFunc`, `OnMethod`, `OnType`, `OnField`, `OnParam` and `OnVar` (var
and const declarations); a type that declares none goes anywhere. They are
checked in every package using the type: Vuka records them in a method of the
type, which Go code declares by hand as below.
An attribute written as a call, such as `@Size(3)`, is checked against the
targets of the type it returns.
:::

::: go
A type declares its targets with an unexported `vukaTargets` method whose
result is an array as long as the targets — `vuka.OnFunc`, `OnMethod`,
`OnType`, `OnField`, `OnParam` and `OnVar` (var and const declarations),
or'ed together. `.vuka` files using the type are checked against it, in
every package; a type that declares none goes anywhere.

```go
type Char struct{ Max int }

func (Char) vukaTargets() [vuka.OnField]struct{} { return [vuka.OnField]struct{}{} }

type Path string

func (Path) vukaTargets() [vuka.OnParam | vuka.OnField]struct{} {
	return [vuka.OnParam | vuka.OnField]struct{}{}
}
```
:::
