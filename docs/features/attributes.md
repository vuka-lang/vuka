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

Struct fields take typed attributes too, after the field:
`Title string @Char{Max: 200}`. See
[Field attributes and references](/features/fields).

A field of type `vuka.File` set to a string literal names a file that is
checked at compile time and embedded: see [Files](/features/decorators#files).
