<p align="center"><img src="assets/vuka-mascot.png" width="128" alt="Vuka"></p>

# Vuka

**Docs: [vuka-lang.github.io/vuka](https://vuka-lang.github.io/vuka/)**

Vuka is Go with Result and Option, `?` error propagation, pattern matching,
function and method overloading, typed attributes, decorators, and JSX
components (rendered by the [UI library](https://github.com/vuka-lang/ui), on templ), with Elixir-style
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
help, go to definition, references, rename, outline, code actions, inlay
hints and formatting (`vuka fmt`) all work; Vuka's own errors and Go's type errors show on the `.vuka`
lines. Overloads show under the name you wrote (`area`, not `area__Circle`). `vuka.File` paths (`@Page("views/pets.templ")`) are clickable.

- **VS Code:** the extension in [`editors/vscode`](editors/vscode)
  (`npm install && npx vsce package`, then install the `.vsix`). It offers to
  serve your Go files too: run as `gopls` (a link the extension makes, set as
  the Go extension's `go.alternateTools.gopls`), vuka is a drop-in gopls, so
  `.go` files in a package with `.vuka` files see their code instead of
  "undefined" errors. Both servers use one shared gopls daemon
  (`gopls -remote=auto`), so the work is done once.
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
| `c.Attr(&x)`, `vuka.AttrOf[T](c)` | a typed attribute on the same declaration |

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

A **declarer**, `func(d *vuka.Decl)` (or a call returning one, such as
`@Command("greet")`), doesn't wrap calls: it runs once at init, top to
bottom, with the function's name, attributes, parameter names and types, and
the decorated function itself — the way to register routes, commands or jobs.
A string literal passed where a decorator or attribute takes a `vuka.File`
(`@Page("views/pet.html")`) is checked at compile time and embedded in
the binary; `f.Bytes()` reads it, and with ui, `templx.Components(f)` gives a
`.templ` file's components with their parameter names — also for a `.templ`
file in a subdirectory, which is a package of its own (`package views`).

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
the whole outer value:

```go
type Model[Self any] struct {
	static Objects = Store[Self]{}           // one Store per embedding type
}

func (m *Model[Self]) Save(self *Self) { Model[Self].Objects.Add(self) }

type User struct {
	Model               // = Model[User]
	Name string
}

u.Save()                    // Save gets u itself as self
users := User.Objects.All() // User's own store, from Model
```

`Model[User]` written out works the same. `Self` is filled in only for a
type parameter of that name, so other generics never get a type argument you
didn't write. A type embedding `User` reaches `User`'s statics, as Go promotes
fields; `self` there is the embedded `User`. Calls that pass `self` must not
have side effects in the receiver expression (Vuka uses it twice). The
[orm](https://github.com/vuka-lang/orm) library's models are built this way.

**Field attributes and references.** A struct field takes typed attributes
after it (after its tag, if any), and `Type.Field` — meaningless in Go — is a
typed reference to the field:

```go
type Post struct {
	ID     int64    @PK
	Title  string   `json:"title"` @Char{Max: 200}
	Author FK[User] @Rel{OnDelete: Cascade}
	Views  int      @Default(0)
}

q := vuka.And(Post.Title.Contains("go"), Post.Author.Name.Eq("ada"), Post.Views.Gt(100))
q.Match(post)                         // in memory; or translate q.Op, q.Path, q.Value
vuka.SortBy(posts, Post.Views.Desc())
```

Attributes are type-checked Go values, stripped from the struct and recorded at
init: `vuka.FieldAttrs[Post]()["Title"]`, `vuka.FieldsOf[Post]()`, or `Attrs`
in a type decorator's fields. A reference continues through struct and pointer
fields, embedded structs, and types implementing `vuka.Related[U]` (a foreign
key's `Related() *U`). Its type follows the field's — `StringRef` has
`Contains`, `OrderedRef` has `Gt`, `NullableRef` has `IsNil` — and its
predicates are `vuka.Pred[Post]`, so an API taking them rejects a `User`
field. Hover and go to definition on a reference land on the field.

**Dependency injection** comes from a type decorator, with no annotations on
fields: a struct's fields are its dependencies.

```go
@di.Component
type OrderService struct {
	db    *DB
	users *UserService
	mu    sync.Mutex `inject:"-"`   // not a dependency
}
```

A type decorator `func(t *vuka.Type)` on a struct gets `t.New`, a constructor
taking each injected field as a parameter, `func(db *DB, users *UserService)
*OrderService`: the shape `nexus.Provide`, `fx.Provide` and `dig.Provide` take,
so a container wires it from parameter types, with no reflection on fields
(and unexported fields work). Every field is injected except `_`, those tagged
`inject:"-"`, and embedded values (embedded pointers and interfaces are). A
struct that declares its own static `New` is built with that instead;
otherwise the generated constructor is its static `New`, handy in tests:
`OrderService.New(fakeDB, fakeUsers)`. `t.Fields` lists the fields with their
tags, attributes and whether they're injected.

The whole nexus integration is a few lines
([`examples/nexus-di`](examples/nexus-di)):

```go
var providers []any

func Component(t *vuka.Type) { providers = append(providers, t.New) }
func Module() nexus.Option  { return nexus.Provide(providers...) }
```

**CLI**

```
vuka new <dir> [module path]
vuka mod tidy|why|vendor|… [args]
vuka explain [-full] file.vuka
vuka fix [-n] [fixer…]
vuka fmt [-l] [-w] [-d] [paths…]
vuka build|run|test|vet|install [go flags] [packages]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka lsp
```

`build`, `run` and the rest transpile the module into a temporary overlay
(`go build -overlay`), so no generated file lands among your sources.

`vuka explain file.vuka` shows each line Vuka rewrites beside the Go it becomes,
then the code it adds after the source (decorator wrappers, statics), each
labelled with the line it comes from; `-full` prints the whole generated file.

`vuka fmt` formats `.vuka` files the way gofmt formats Go (`-w` writes them
back, `-l` lists those that differ, `-d` shows the diff): the Go exactly as
gofmt prints it, Vuka's syntax spaced to match, and JSX laid out
Prettier-style, without changing what a page renders.

`vuka fix` repairs what tooling can: `runtime` upgrades the module's Vuka
runtime when generated code needs a newer one, `static-names` rewrites statics
spelt by their Go names (`User_Table`) to `User.Table` in `.vuka` files, and
`orphans` removes files `vuka gen -inplace` wrote for `.vuka` files that are
gone, `attr-of` renames `vuka.Attr[T](c)` to `vuka.AttrOf[T](c)` (v0.5.0), and
`ui` moves UI code to `github.com/vuka-lang/ui` (v0.10.0). `-n` reports without changing anything; name fixers to run only those.
It is also where future syntax changes will get their codemods.

**JSX.** Markup is an expression, and a component is a function. JSX is
syntax: what it renders with is the library the file imports, its JSX target —
[`github.com/vuka-lang/ui`](https://github.com/vuka-lang/ui), where `ui.Node`
is `templ.Component`:

```go
import "github.com/vuka-lang/ui"

func PetRow(pet Pet) ui.Node {
	return <tr><td>{pet.Name}</td><td>{pet.Age}</td></tr>
}

func Page(pets []Pet) ui.Node {
	return <Shell title="Pets">                      // a templ component
		<table>{for _, p := range pets { <PetRow pet={p} /> }}</table>
	</Shell>
}
```

Attributes bind to parameters by name (or to a props struct's fields), children
to a `children` parameter; `{for}`, `{if}` and `{match}` blocks hold markup;
all of it type-checked against the target's declarations. A package is a
target when it declares `const VukaJSX = 1` and the contract's names. ui
renders HTML (`ui.Handler`/`Write`/`String`) and terminal text (`ui/term`), and
its stateful components — a struct embedding `ui.Live`, `onClick={c.Inc}` —
stay live for a connected page through `ui/live`. `.templ` files compile with
the package, no `templ generate`, in modules that require templ. See
[JSX](https://vuka-lang.github.io/vuka/features/jsx) and
[UI](https://vuka-lang.github.io/vuka/ui/).

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
3. `@derive` and generator attributes;
   lowering to native Go when a Go release adds an equivalent feature

## Libraries

- [**web**](https://github.com/vuka-lang/web) — a web framework: routes,
  services and controllers declared with decorators, parameters bound by name,
  templ / html / JSX views, dependency injection, `.env` configuration and live
  components over a WebSocket. Docs: [vuka-lang.github.io/vuka/web](https://vuka-lang.github.io/vuka/web/).
- [**orm**](https://github.com/vuka-lang/orm) — a Django-style ORM: models
  declared with field attributes, queries written with field references,
  `Result`/`Option` terminals, `@orm.Transaction`, search, several databases
  and Go migrations. Docs: [vuka-lang.github.io/vuka/orm](https://vuka-lang.github.io/vuka/orm/).

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
