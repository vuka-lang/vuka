# Decorators

[[toc]]

## Introduction

Cross-cutting behaviour — logging, retries, caching, auth — is otherwise
written into each function, or wired with middleware by hand. A decorator
names that behaviour once and applies it with `@name` above a declaration.

One decorator works on any function or method, whatever its signature. Other
kinds of decorator run once at start-up instead, so a library can register
routes, commands or jobs from the declarations it decorates.

## Writing a Decorator

<!-- go:context
type Receipt struct{ ID int }
type Perm string
type Base[T any] struct{}

type User struct{ Name string }

type Store struct{ users []User }
-->

This sample declares a `logged` decorator and a `retry` decorator with a
parameter, then applies them to a function and a method:

```vuka
decorator logged(c) {
	fmt.Println("->", c.Name, c.Args)
	c.Next()                               // run the function (or the next decorator)
	fmt.Println("<-", c.Name, c.Results)
}

decorator retry(times int)(c) {            // with parameters
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
func (s *Store) Save(u User) error { … }
```

`decorator name(c) { … }` is shorthand for `func name(c *vuka.Call)`; plain Go
functions of that type work too, in `.vuka` or `.go` files.

## The Call

A decorator receives a `*vuka.Call`. These are its fields and methods:

| | |
|---|---|
| `c.Name`, `c.Receiver` | `"main.charge"`; the receiver of a method |
| `c.Args`, `c.Results` | arguments (change them before `Next`) and results |
| `c.Next()` | run the rest; call it again to retry, or not at all |
| `c.Return(v…)` | answer without running the function (caches, mocks) |
| `c.Err()`, `c.SetErr(err)` | the trailing error result |
| `c.Context()` | the `context.Context` argument |
| `vuka.Arg[T](c, i)` | argument `i` as a `T` |
| `c.Attr(&x)` | a [typed attribute](/features/attributes) on the same declaration |

<!-- go:context
type Order struct{ ID int }

var ErrForbidden = errors.New("forbidden")

func allowed(ctx context.Context, p Perm) bool { return p != "" }
-->

This `guard` decorator reads a `Perm` attribute from the declaration and
refuses the call when it isn't allowed:

```vuka
decorator guard(c) {
	var p Perm
	c.Attr(&p)
	if !allowed(c.Context(), p) {
		c.SetErr(ErrForbidden)
		return
	}
	c.Next()
}

@Perm("orders.write")
@guard
func save(ctx context.Context, o Order) error { … }
```

A decorator with parameters is built once per decorated function, so state it
keeps — a cache, a counter — isn't shared.

A call decorator reads parameter attributes too:

| | |
|---|---|
| `c.ParamAttr(i, &x)` | the [attribute](/features/attributes#parameter-attributes) written before parameter `i` |

## Call Metadata and Adapters

`vuka.FuncInfo{Attrs, ParamAttrs}` are what `c.Attr` and `c.ParamAttr`
read. `Wrap` calls the function through reflection; registering a typed
adapter for its signature, `vuka.RegisterAdapter`, takes reflection out.

The vuka command writes one per signature a package's decorators wrap —
or, in a package written in Go, its `vuka.Wrap` calls (a function, a method
expression, a function literal) — in `vuka_calls_gen.go` (`vuka gen
-inplace` commits it), an optional speed-up the main file never depends on.

A call through two decorators is about 500ns by reflection, 110ns with the adapter, on an Apple M1 Pro
(`go test -bench Wrap` in the vuka module). A generic function is always
called by reflection.

## Optional Arguments

Many decorators have a sensible default and only sometimes need settings: run
in a transaction — usually on the default database, now and then read-only or
on another one. Give the decorator only optional arguments (no parameters, or
one `...T`) and it is written bare for the default, with arguments otherwise.
Here `transaction` is used both ways:

<!-- go:context
type TxOption struct{ db string; readOnly bool }

func On(db string) TxOption { return TxOption{db: db} }

var ReadOnly = TxOption{readOnly: true}

type Report struct{ Total int }
-->

```vuka
func transaction(opts ...TxOption) func(*vuka.Call) { … }

@transaction                            // transaction(): the defaults
func save(ctx context.Context, u User) error { … }

@transaction(On("analytics"), ReadOnly)
func report(ctx context.Context) (Report, error) { … }
```

Written bare, it is called with no arguments. That holds for every kind:
call decorators, typed decorators, declarers and type decorators, and for a
[composed decorator](#composed-decorators) without parameters.

## Composed Decorators

When the same call decorators keep appearing together, name the set once:

<!-- go:context
type Pet struct{ Name string }

type PetStore struct{ pets map[int]Pet }

type Doc string

func auth(next http.Handler) http.Handler { return next }

func timed(c *vuka.Call) {
	start := time.Now()
	c.Next()
	fmt.Println(c.Name, time.Since(start))
}
-->

```vuka
decorator Audited = @logged @Doc("audited")

@Audited
func charge(ctx context.Context, id int) (Receipt, error) { … }
```

`@Audited` applies `@logged` and `@Doc("audited")` in order.

A composed decorator holds call decorators,
declarers, type decorators, typed attributes and other composed decorators; its
elements go on its line, or one a line in parentheses. One without parameters
is used bare: `@Audited`.

It is a function returning a `vuka.Bundle`, so another package uses an
exported one like any decorator — `@audit.Audited` — and what it composes
may be unexported. Its elements are type-checked where it is declared.

A [typed decorator](#typed-decorators) can't be composed, since it needs the
decorated function's type; nor can a decorator compose itself
(`decorator A = @B`, `decorator B = @A` is an error). Plain Go builds one with
`vuka.Compose(logged, Doc("audited"))`.

## Type-Level Advice

To give every method of a type the same behaviour — log each call to a store,
retry each call to a flaky client — put the call decorator on the type. It
wraps every exported method the package's Vuka files declare for the type, on
`T` or `*T`. This sample advises every exported method of `Store`:

```vuka
@logged
@retry(3)
type Store struct{ db *DB }

func (s *Store) Save(u User) error { … }      // logged, then retried

@timed
func (s *Store) Load(id int) (User, error) { … }   // logged, retried, then timed

@vuka.NoAdvice
func (s *Store) Close() error { … }           // left alone
```

The type's advice runs outside the method's own decorators, in the order it is
written; each method gets decorators of its own, as a decorated function does.

Unexported methods aren't advised, nor methods marked `@vuka.NoAdvice`, nor
methods declared in `.go` files. A composed decorator on a type advises its
methods with the call decorators it holds, and runs its type decorators on the
type.

## Typed Decorators

For hot paths, a decorator can be `func(F) F` for the function's type: no
boxing, full types. Vuka picks the form from the decorator's type, and the two
mix freely.

## Type Decorators {#types}

A type decorator runs at start-up with the type: `func Model(t *vuka.Type)` gets
`t.Name`, `t.Reflect`, `t.Attrs` — and, for a struct, a constructor for
[dependency injection](/features/dependency-injection): the type's `NewX`
(its static `New`), else one Vuka writes taking each injected field.
In Go, `@Model` on `User` is `Model(vuka.TypeFor[User](NewUser))`.

## Declaration Decorators

A decorator of type `func(d *vuka.Decl)` — or a call returning one — is a
**declarer**: it doesn't wrap calls, it runs once at program start with a
description of the declaration. That is how a library registers routes,
commands or jobs from the functions it decorates. This sample builds a small
command registry:

```vuka
var commands = map[string]*vuka.Decl{}

func Command(name string) func(*vuka.Decl) {
	return func(d *vuka.Decl) { commands[name] = d }
}

// run calls a command with its arguments bound to parameters by name.
func run(name string, args map[string]string) []reflect.Value {
	d := commands[name]
	var in []reflect.Value
	for _, p := range d.Params {
		in = append(in, reflect.ValueOf(args[p.Name]).Convert(p.Type))
	}
	return reflect.ValueOf(d.Func).Call(in)
}

@Command("greet")
@logged
func greet(name string) string { return "hello " + name }

run("greet", map[string]string{"name": "ada"})   // -> main.greet [ada] …
```

Application page routes instead come from the [pages tree](/web/folder-routes).

### Linking Declarer Packages

A declarer only runs if its package is compiled in, and Go compiles only what
is imported — so the `vuka` command links declarer packages itself. `vuka
build`, `run`, `test` and `vet` find every package of the module with a
declarer, a type decorator or a [composed decorator](#composed-decorators) on
a declaration and give each main package a generated file of blank imports
(`vuka gen` writes it as `vuka_link_gen.go` for plain go tools). Nothing to
write, nothing to forget: a declaration package is linked when it is discovered.

Test files don't count, and a `//vuka:nolink` line above a
package clause opts that package — or, on a main, the whole program — out;
see [the CLI](/tools/cli#build-run-test-vet-install).

A package also counts when one of its struct types embeds a type that
declares itself: one with a `vukaDeclares()` method, unexported, with no
parameters or results. [ORM models](/orm/models) link this way — `orm.Base`
has the marker — so a model package nothing imports is still migrated. A
library whose embeddable type registers its embedders at init marks it the
same way, with an empty method:

```go
func (Base[T]) vukaDeclares() {}
```

Only `.vuka` files are looked at; a `.go` file's embedding doesn't link its
package.

### The Declaration

A declarer receives a `*vuka.Decl`:

| `*vuka.Decl` | |
|---|---|
| `d.Name`, `d.Pkg` | `"main.greet"`, or `"Shell.Run"` for a method; the import path |
| `d.File`, `d.Line` | where it is declared, for messages |
| `d.Func` | the function, wrapped by its other decorators (`logged` runs when it is called); for a method, the method expression `func(recv, args…)` |
| `d.Recv` | the receiver's type, for a method |
| `d.Params` | names (as written; `""` when unnamed), types and [attributes](/features/attributes#parameter-attributes) (`p.Attr(&x)`), receiver excluded |
| `d.Results`, `d.Variadic` | result types; whether the last parameter is `...T` |
| `d.Attr(&x)` | a typed attribute on the declaration |

In Go, `@Command("greet")` on `greet` is
`Command("greet")(vuka.DeclOf(greet, vuka.Param{Name: "name"}))`: `DeclOf`
describes a function or method expression, the attributes those registered
for it (`vuka.RegisterFunc`).

### When Declarers Run

Declarers run in the package's `init`, after its variables are initialised:
declarations in source order (files in name order, as Go orders `init`), and a
declaration's declarers top to bottom. They mix freely with call and typed
decorators.

A declarer needs a concrete function: on a generic function, `init`
or a type it is an error — a type's decorator takes a `*vuka.Type`.

## Registries

Library registries can expose declaration decorators as a language mechanism.
Application pages use the generated folder route table instead: rendering lives
in `.vuka.html`, routes in `pages/` and startup in `web.New(pages.Routes(), …)`.
Service constructors register through `app.Service(NewX)`.

## Embedding Files {#files}

A string literal passed where a decorator or a typed attribute takes a
`vuka.File` names a file of the source tree, relative to the package's
directory. This sample registers a view file for each decorated function:

```vuka
var pages = map[string]vuka.File{}

func Page(view vuka.File) func(*vuka.Decl) {
	return func(d *vuka.Decl) { pages[d.Name] = view }
}

@Page("views/pet.html")
func showPet(id string) Pet { … }
```

The file must exist when the program is compiled, in the package's directory
or below it (all `go:embed` reaches); a missing, absolute or outside path is an
error on the literal. It is embedded in the binary, and the literal becomes a
`File` holding `"importpath:views/pet.html"`:

| `f.Bytes()` | the content (a `File` Vuka didn't embed, such as `vuka.File("x")`, is read from disk) |
| `f.Path()`, `f.Pkg()` | `"views/pet.html"`; the package's import path |
| `templx.Components(f)` | for a `.templ` file, with templ and [ui](/ui/templ): its components in source order: <span v-pre>`[]templx.Component{{Name: "Show", Func: Show, Params: []string{"name"}}}`</span>, `Func` a `func(…) templ.Component` |

### templ Files

A `.templ` file beside the package's files is part of the package (same
`package` clause) and gives all its components. One in a subdirectory is the
subdirectory's own package, as Go has it: `views/pets.templ` starts with
`package views`, and the subdirectory must be a package of the same module
whose Go files, if any, share that clause. Vuka compiles it with the rest of the
module, imports it into the referencing file and registers its exported
components; the `File` is then that package's —
`"example.com/app/views:pets.templ"` — whichever package names it. Files are
still embedded only from the package's directory or below: a path such as
`../shared/x.templ` is an error, since `go:embed` can't reach up.

> [!WARNING]
> The parameter or field must be `vuka.File` itself; literals elsewhere aren't
> embedded.

The web framework's views are files of this kind: see [Views](/web/views).

## How Decorated Functions Are Built

A function wrapped by its decorators keeps its name for a wrapper that builds
the decorated function once, on first call (`vuka.Decorated`); recursion goes
through the decorators. One only declarers or registries take stays as it is.

Methods, `init`, overloads, generic functions and `type ( … )` groups can be
decorated. In the editor, typing `@` lists every decorator in the project, and
picking one from another package adds the import.
