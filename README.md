# Vuka

Vuka is Go with function and method overloading and typed attributes, with
Elixir-style multi-clause functions, guards and pattern matching on the way. It
transpiles to plain Go and builds with the go command you already have.

```go
func area(c Circle) float64 { return math.Pi * c.R * c.R }

@export("RectArea")
func area(r Rect) float64 { return r.W * r.H }

func (c *Counter) Add(by int)    { c.n += by }
func (c *Counter) Add(by string) { c.n += len(by) }

@cache.Memo{TTL: 5 * time.Minute}
func lookup(id int) User { … }
```

```
go install github.com/vuka-lang/vuka/cmd/vuka@latest
vuka run ./examples/shapes
```

## Built to track Go

Vuka owns only its additions. Each one is found by `go/scanner` and lowered to Go
in place; everything else passes through untouched, to be parsed and
type-checked by the standard library's `go/parser` and `go/types`. When Go adds
syntax, Vuka already accepts it.

- Install Vuka with your own toolchain (`go install`, or `go get -tool` and
  `go tool vuka`). Its parser is the stdlib of the Go that built it, so it matches
  yours.
- `TestStdlibPassesThrough` feeds every file of `$GOROOT/src` through Vuka and
  requires byte-identical output. Run it on a new Go release to find out what, if
  anything, has to change.
- The generated code is plain Go with no runtime library. `//line` directives
  point compile errors and panics at the `.vuka` source, down to the column.

## What works (v0.1)

**Overloading.** Functions and methods may share a name when their parameter
types differ. Each declaration is renamed (`area__Circle`, `Add__int_int`), and each
call goes to the overload that best fits its arguments' static types: identical
types beat an untyped constant's default type, which beats any other assignable
value. A tie is a compile error that lists the candidates. Calls whose argument
types come from other overloaded calls resolve too.

**Attributes** come before a top-level declaration:

| | |
|---|---|
| `@doc("…")` | becomes the declaration's doc comment |
| `@deprecated("…")` | adds a `Deprecated:` paragraph |
| `@export("Name")` | generates `Name`, a wrapper with the declaration's signature: how Go code reaches one overload |
| `@T` / `@pkg.T{…}` | any Go type, bare or as a composite literal; the generated code type-checks it |

**CLI**

```
vuka build|run|test|vet|install [go flags] [packages]
vuka gen [-check] [dir | dir/...]
```

`build`, `run` and the rest transpile the module into a temporary overlay
(`go build -overlay`), so no generated file lands in your tree. `gen` writes
`name_vuka.go` beside each `.vuka` file for tools that run without Vuka;
`-check` is a CI drift gate.

## Roadmap

1. Result/Option, `?`, enums (sum types), `match` with exhaustiveness
2. Lambdas, `?.`, `??`, tuples, functional helpers (the rest of
   [Dingo](https://github.com/MadAppGang/dingo)'s set)
3. Multi-clause functions with patterns and guards; arity overloading for default
   arguments; Elixir-style module attributes (`@max 3`, read as `@max`)
4. `@derive` and generator attributes; `vuka lsp` (a gopls proxy); `vuka fmt`;
   lowering to native Go when a Go release adds an equivalent feature

## Known limits

- Overloads are resolved within a package; calling another package's overloads
  goes through its `@export` names.
- Generic functions can't be overloaded yet.
- Go's own error messages show mangled names (`area__int`).
- An overloaded method can't satisfy an interface method of the plain name.
