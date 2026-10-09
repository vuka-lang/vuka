<p align="center"><img src="assets/vuka-mascot.png" width="128" alt="Vuka"></p>

# Vuka

Vuka is Go with Result and Option, `?` error propagation, pattern matching,
function and method overloading, and typed attributes, with Elixir-style
multi-clause functions on the way. It transpiles to plain Go and builds with the
go command you already have.

```go
func area(c Circle) float64 { return math.Pi * c.R * c.R }

@export("RectArea")
func area(r Rect) float64 { return r.W * r.H }

func (c *Counter) Add(by int)    { c.n += by }
func (c *Counter) Add(by string) { c.n += len(by) }

@cache.Memo{TTL: 5 * time.Minute}
func lookup(id int) User { … }
```

```go
func lookup(arg string) Result[User] {
	id := strconv.Atoi(arg)?          // Go's (T, error) works directly
	u := find(id)?
	return Ok(u)
}

match lookup(arg) {
case Ok(u):
	fmt.Println(u.Name)
case Err(&NotFound{ID: id}):
	fmt.Println("missing user", id)
case Err(e) if errors.Is(e, strconv.ErrSyntax):
	fmt.Println("not a number:", arg)
case Err(e):
	fmt.Println("error:", e)
}
```

```
go install github.com/vuka-lang/vuka/cmd/vuka@latest
vuka new hello && cd hello && vuka run .
```

A Vuka project is a Go module: `.vuka` and `.go` files sit side by side in the
same packages and call each other, the way Kotlin and Java share a project.
Dependencies are Go's: `go get` a module and import it from any `.vuka` file.

## Editors

`vuka lsp` is a language server for `.vuka` files: gopls behind a proxy that
keeps the generated Go open in gopls as unsaved buffers (nothing is written to
your tree) and maps every position both ways. Completion, hover, signature
help, go to definition, references, rename, outline, code actions and inlay
hints all work; Vuka's own errors and Go's type errors show on the `.vuka`
lines. Overloads show under the name you wrote (`area`, not `area__Circle`).

- **VS Code:** the extension in [`editors/vscode`](editors/vscode)
  (`npm install && npx vsce package`, then install the `.vsix`).
- **Any other editor:** run `vuka lsp` over stdio for the `vuka` file type
  (needs gopls: `go install golang.org/x/tools/gopls@latest`).

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
- The generated code is plain Go; its only runtime is the small Result/Option
  package. `//line` directives
  point compile errors and panics at the `.vuka` source, down to the column.

## What works

**Result and Option** live in the runtime package `github.com/vuka-lang/vuka`
(its root; nothing else in it). Vuka code writes `Result[User]`, `Ok(u)`,
`Err(e)`, `Some(x)` and `None` unqualified, and the transpiler adds the import
wherever the package doesn't declare those names itself. `Err` and `None` take
their type from where the value goes (`return Err(e)` in a function returning
`Result[User]`). Result's error is Go's `error`, so it meets Go code halfway:
`vuka.Of(os.ReadFile(p))` wraps a Go call, `r.Get()` hands back `(T, error)`.
Your module needs the dependency: `go get github.com/vuka-lang/vuka`.

**`?`** ends a statement: `x := f()?`, `x = f()?`, `var x = f()?`,
`return f()?` or `f()?`. The operand is Go's `(T…, error)`, an `error`, a
Result, or an Option (in a function returning an Option). On failure the
function returns: zero values plus the error, `Err(e)`, or `None`, whichever
its results call for. The operand stays where it is, so nothing is reordered.

**`match`** is a statement of cases, tried in order:

| pattern | matches |
|---|---|
| `_`, `default` | anything |
| `x` | anything, binding it to `x` (a constant's name compares instead) |
| `0`, `"a"`, `Max`, `pkg.Const`, `true` | an equal value |
| `^x` | the value of the variable `x` (Elixir's pin) |
| `Ok(p)`, `Err(p)`, `Some(p)`, `None` | a Result or Option, then `p` inside |
| `T{F: p, …}`, `&T{…}` | a struct's fields; against an interface, a type test too |
| `1, 2, 3` | any of them |

`case p if cond:` adds a guard. A match must be exhaustive (Ok and Err, Some
and None, true and false, or a case that takes anything), and a case after one
that matches everything is an error; both are reported before Go sees the code.

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

**Decorators** wrap a declaration, Python-style. The simplest kind works on
any function, whatever its signature:

```go
decorator logged(c) {
	fmt.Println("calling", c.Name, c.Args)
	c.Next()                                   // run the function (or the next decorator)
	fmt.Println("returned", c.Results)
}

decorator retry(times int)(c) {             // with parameters
	for i := 0; i < times; i++ {
		if c.Next(); c.Err() == nil {
			return
		}
	}
}

@retry(3)
@logged
func charge(ctx context.Context, id int) (Receipt, error) { … }

@logged
func (s *Store) Save(u User) error { … }   // methods too
```

`decorator name(c) { … }` is shorthand for `func name(c *vuka.Call)`, a
`vuka.Decorator`; plain Go functions of that type work the same, in `.vuka` or
`.go` files. A decorator with parameters (`retry(3)`) is built once per
decorated function, so state it keeps (a cache, a counter) isn't shared.

| `*vuka.Call` | |
|---|---|
| `c.Name`, `c.Receiver` | `"main.charge"`; the receiver of a method |
| `c.Args`, `c.Results` | arguments (change them before `Next`) and results |
| `c.Next()` | run the rest; call it again to retry, or not at all |
| `c.Return(v…)` | answer without running the function (caches, mocks) |
| `c.Err()`, `c.SetErr(err)` | the trailing error result |
| `c.Context()` | the `context.Context` argument |
| `vuka.Arg[T](c, i)` | argument `i` as a `T` |
| `c.Attr(&x)`, `vuka.Attr[T](c)` | a typed attribute on the same declaration |

```go
@Perm("orders.write")
@guard                                      // reads it: var p Perm; c.Attr(&p)
func save(ctx context.Context, o Order) error { … }
```

A type decorator receives the type: `func Model(t *vuka.Type)` gets `t.Name`,
`t.Reflect` and `t.Attr`, at init. Packing a call into a `*vuka.Call` costs a
small allocation; for hot paths, a **typed** decorator `func(F) F` (or for a
type, a generic `func[T]`) has none and checks every type. Vuka picks the form
from the decorator's type, so both mix freely, and a decorator that fits
neither is a compile error on its `@` line. Functions keep their name for a
wrapper that builds the decorated function once; recursion goes through it;
`init`, overloads, generic functions and `type ( … )` groups can be decorated.

**Static fields and methods** belong to a type, as in Kotlin or Java:

```go
type User struct {
	Name string

	static Table   = "users"
	static created int            // private: lowercase, as in Go
	static const Max = 100
}

func User.New(name string) *User {
	User.created++
	return &User{Name: name}
}

u := User.New("ada")
fmt.Println(User.Table, User.Max)
```

They lower to plain package-level Go: `User_Table`, `_User_created`,
`func User_New`, which is also how Go code reaches them. A static can't share a
name with a field or method (in Go, `User.Save` already means a method).

A static on a **generic** type has one value per instantiation, and statics are
reached **through embedding**. With a base type whose type parameter is named
`Self`, Vuka fills in the embedding type, and methods taking `self *Self` get
the whole outer value. That's enough for a Django-style model API:

```go
// package orm
type Model[Self any] struct {
	static Objects = Manager[Self]{}
}

func (m *Model[Self]) Save(self *Self, ctx context.Context) error { … }

// your models
type User struct {
	orm.Model               // = orm.Model[User]
	Name string
}

users := User.Objects.All(ctx)   // the User manager, from orm.Model
err := u.Save(ctx)               // Save gets u itself as self
```

`orm.Model[User]` written out works the same. `Self` is filled in only for a
type parameter of that name, so other generics never get a type argument you
didn't write. A type embedding `User` reaches `User`'s statics, as Go promotes
fields; `self` there is the embedded `User`. Calls that pass `self` must not
have side effects in the receiver expression (Vuka uses it twice).

**CLI**

```
vuka new <dir> [module path]
vuka build|run|test|vet|install [go flags] [packages]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka lsp
```

`build`, `run` and the rest transpile the module into a temporary overlay
(`go build -overlay`), so no generated file lands among your sources.

### The build module

Like Kotlin's `build/`, `vuka build` and `vuka gen` keep a `build/` directory:
a complete Go module with the same module path, where every `.vuka` file is
replaced by its generated Go and everything else is copied (`.go` files,
embedded assets, `testdata`; relative `replace` paths in `go.mod` adjusted).
Inside it, plain Go tools need no Vuka at all:

```
vuka gen
cd build && go build ./... && go test ./... && go vet ./...
```

The sync is incremental (only changed files are written, deleted sources are
removed), compile errors in `build/` point at the `.vuka` lines, and a
`.vuka-build` marker means vuka never syncs over a directory it didn't make.
`vuka new` adds `/build/` to `.gitignore`. `vuka gen -check` fails when
`build/` is out of date, for CI.

## Roadmap

1. Enums (sum types) with exhaustive `match`; lambdas, `?.`, `??`, tuples, functional helpers (the rest of
   [Dingo](https://github.com/MadAppGang/dingo)'s set)
2. Multi-clause functions with patterns and guards; arity overloading for default
   arguments; Elixir-style module attributes (`@max 3`, read as `@max`)
3. `@derive` and generator attributes; `vuka fmt`;
   lowering to native Go when a Go release adds an equivalent feature

## Libraries

Other modules `go get` your repository root, not `build/`. To publish a Vuka
library for Go users, write the generated files beside the sources and commit
them (`vuka gen -inplace`, and `vuka gen -inplace -check` in CI).

## Known limits

- Overloads are resolved within a package; calling another package's overloads
  goes through its `@export` names.
- Generic functions can't be overloaded yet.
- Go's own error messages show mangled names (`area__int`).
- An overloaded method can't satisfy an interface method of the plain name.
- `?` ends a statement; it can't sit inside a larger expression, or in an
  `if`, `for` or `switch` header.
- Struct patterns of generic types aren't supported yet.
