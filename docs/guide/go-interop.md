# Working With Go

[[toc]]

## Introduction

A Vuka project **is** a Go module. Go code and Vuka code live in the same
packages, use the same dependencies and call each other directly.

This page covers mixing the two kinds of file, using Go packages, converting
between `Result` and `(T, error)`, and what your Vuka declarations are called
from Go.

## Mixing .go and .vuka

Files of both kinds share packages: a `.go` file calls a function written in a
`.vuka` file and the other way round, with no wrappers. In the editor, `.go` files
see Vuka code too when the extension [serves Go files](/tools/editors#go-files).

## Adding Dependencies

Use the go command as always:

```sh
go get github.com/google/uuid
```

Then import the package in any `.vuka` file. Every Go package works, and `?`
works directly on functions returning `(T, error)`. See
[The ? Operator](/features/try).

## Result Meets (T, error)

You convert between Go's `(T, error)` and a [Result](/features/result-option)
in both directions:

```vuka
r := vuka.Of(os.ReadFile(name))      // (T, error) → Result[T]
v, err := r.Get()                    // Result[T] → (T, error)
```

<!-- go:context
var name = "app.conf"
-->

## Calling Vuka from Go

Generated names are ordinary Go. The table below shows what Go code calls for
each Vuka declaration:

| Vuka | What Go code calls |
|---|---|
| `User.Table` (static) | `UserTable` |
| `User.New(…)` (the static constructor) | `NewUser(…)` |
| `User.Find(…)` (another static method) | `UserFind(…)` |
| `Post.Objects` (a model's manager, a static of `orm.Base`) | `orm.Objects[Post]()` |
| `Post.Title` (a model's field reference) | `PostFields.Title` (written by `orm generate`) |
| overload `area(c Circle)` | `areaCircle` (named after its parameters' types) — or the name you give with `@export("Area")` |
| `Result[T]`, `Ok`, `Err` | `vuka.Result[T]`, `vuka.Ok`, `vuka.Err` from `github.com/vuka-lang/vuka/v2` |

## Publishing a Library

Other modules `go get` your repository, not `build/`. To publish a Vuka library
for Go users, write the generated files beside the sources and commit them:

```sh
vuka gen -inplace
vuka gen -inplace -check   # in CI
```

## The Runtime

Result, Option, decorators and statics of generic types use the small package
`github.com/vuka-lang/vuka/v2` (the module's root). Each Vuka release states the
runtime it needs.
