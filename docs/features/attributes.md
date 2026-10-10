# Attributes

**Go:** metadata lives in struct tags and comments (`//go:generate`, doc text).

**Vuka adds** attributes: `@` before a declaration. An attribute is either built
in, or any Go type — so its arguments are type-checked Go.

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
wrong field or type is a compile error on the `@` line. Attributes are readable
at run time by [decorators](/features/decorators) through `c.Attr(&x)`.

An attribute that names a *function* rather than a type is a decorator.

Parameters take them before the parameter — `func show(@Path("id") id int)` —
see [Parameter attributes](#parameter-attributes); and an attribute type can
restrict where it goes: see [Targets](#targets).

Struct fields take typed attributes too, after the field:
`Title string @Char{Max: 200}`. See
[Field attributes and references](/features/fields).

A field of type `vuka.File` set to a string literal names a file that is
checked at compile time and embedded: see [Files](/features/decorators#files).

## Parameter attributes

Parameters take typed attributes too, written before the parameter, for code
that needs to know something about each argument: where a web route reads it
from, which arguments to validate:

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
function literals. A [declarer](/features/decorators#declaration-decorators)
reads them from `d.Params[i].Attrs`, or with `d.Params[i].Attr(&x)`; a call
decorator with `c.ParamAttr(i, &x)`, `i` indexing `c.Args`:

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

## Targets

Some attributes only make sense in one place — a column setting belongs on a
field, a route's path source on a parameter. The attribute type can say so,
and writing it anywhere else becomes a compile error:

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
type, which Go code declares by hand as
`func (Char) vukaTargets() [vuka.OnField]struct{} { return [vuka.OnField]struct{}{} }`.
An attribute written as a call, such as `@Size(3)`, is checked against the
targets of the type it returns.
