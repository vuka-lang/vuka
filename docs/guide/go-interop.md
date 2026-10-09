# Working with Go

A Vuka project **is** a Go module.

## Mixing .go and .vuka

Files of both kinds share packages: a `.go` file calls a function written in a
`.vuka` file and the other way round, with no wrappers. In the editor, `.go` files
see Vuka code too when the extension [serves Go files](/tools/editors#go-files).

## Dependencies

Use the go command as always:

```sh
go get github.com/google/uuid
```

and import the package in any `.vuka` file. Every Go package works, and `?` works
directly on functions returning `(T, error)`.

## Result meets (T, error)

```vuka
data := vuka.Of(os.ReadFile(name))   // (T, error) → Result[T]
v, err := r.Get()                    // Result[T] → (T, error)
```

## Calling Vuka from Go

Generated names are ordinary Go:

| Vuka | What Go code calls |
|---|---|
| `User.Table` (static) | `User_Table` |
| `User.New(…)` (static method) | `User_New(…)` |
| overload `area(c Circle)` | `area__Circle` — or the name you give with `@export("Area")` |
| `Result[T]`, `Ok`, `Err` | `vuka.Result[T]`, `vuka.Ok`, `vuka.Err` from `github.com/vuka-lang/vuka` |

## Publishing a library

Other modules `go get` your repository, not `build/`. To publish a Vuka library
for Go users, write the generated files beside the sources and commit them:

```sh
vuka gen -inplace
vuka gen -inplace -check   # in CI
```

## The runtime

Result, Option, decorators and statics of generic types use the small package
`github.com/vuka-lang/vuka` (the module's root). Each Vuka release states the
runtime it needs; `vuka fix runtime` upgrades it.
