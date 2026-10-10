# Decorators

**Go:** cross-cutting behaviour (logging, retries, caching, auth) is written into
each function, or wired with middleware by hand.

**Vuka adds** decorators, as easy as Python's. One decorator works on any
function or method, whatever its signature:

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

## The call

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

| | |
|---|---|
| `c.ParamAttr(i, &x)` | the [attribute](/features/attributes#parameter-attributes) written before parameter `i` |

## Optional arguments

Many decorators have a sensible default and only sometimes need settings: run
in a transaction — usually on the default database, now and then read-only or
on another one. Give the decorator only optional arguments (no parameters, or
one `...T`) and it is written bare for the default, with arguments otherwise:

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

## Composed decorators

When the same decorators keep appearing together — every API route is a GET
that needs authentication and is timed — name the set once and write that
name instead:

```vuka
decorator ApiRoute(path string) = @web.Get(path) @web.Use(auth) @timed

decorator Audited = @logged @Doc("audited")

decorator Service = (
	@web.Service
	@Audited
)

@ApiRoute("/pets/{id}")
func showPet(id int, store *PetStore) Result[Pet] { … }
```

`@ApiRoute("/pets/{id}")` applies `@web.Get("/pets/{id}")`, `@web.Use(auth)` and
`@timed` in that order, as if they were written there: its parameters are the
arguments of what it composes. A composed decorator holds call decorators,
declarers, type decorators, typed attributes and other composed decorators; its
elements go on its line, or one a line in parentheses. One without parameters
is used bare: `@Audited`.

It is a function returning a `vuka.Bundle`, so another package uses an
exported one like any decorator — `@api.ApiRoute("/x")` — and what it composes
may be unexported. Its elements are type-checked where it is declared. A
[typed decorator](#typed-decorators) can't be composed, since it needs the
decorated function's type; nor can a decorator compose itself
(`decorator A = @B`, `decorator B = @A` is an error). Plain Go builds one with
`vuka.Compose(web.Get(path), timed, Doc("x"))`.

## Type-level advice

To give every method of a type the same behaviour — log each call to a store,
retry each call to a flaky client — put the call decorator on the type. It
wraps every exported method the package's Vuka files declare for the type, on
`T` or `*T`:

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

## Typed decorators

For hot paths, a decorator can be `func(F) F` for the function's type: no
boxing, full types. Vuka picks the form from the decorator's type, and the two
mix freely.

## Types

A type decorator runs at start-up with the type: `func Model(t *vuka.Type)` gets
`t.Name`, `t.Reflect`, `t.Attrs` — and, for a struct, a constructor for
[dependency injection](/features/dependency-injection).

## Declaration decorators

A decorator of type `func(d *vuka.Decl)` — or a call returning one — is a
**declarer**: it doesn't wrap calls, it runs once at program start with a
description of the declaration. That is how a library registers routes,
commands or jobs from the functions it decorates. A small command registry:

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

The [web framework](/web/routes) declares its routes this way.

| `*vuka.Decl` | |
|---|---|
| `d.Name`, `d.Pkg` | `"main.greet"`, or `"Shell.Run"` for a method; the import path |
| `d.File`, `d.Line` | where it is declared, for messages |
| `d.Func` | the function, wrapped by its other decorators (`@logged` runs when it is called); for a method, the method expression `func(recv, args…)` |
| `d.Recv` | the receiver's type, for a method |
| `d.Params` | names (as written; `""` when unnamed), types and [attributes](/features/attributes#parameter-attributes) (`p.Attr(&x)`), receiver excluded |
| `d.Results`, `d.Variadic` | result types; whether the last parameter is `...T` |
| `d.Attr(&x)` | a typed attribute on the declaration |

Declarers run in the package's `init`, after its variables are initialised:
declarations in source order (files in name order, as Go orders `init`), and a
declaration's declarers top to bottom. They mix freely with call and typed
decorators. A declarer needs a concrete function: on a generic function, `init`
or a type it is an error — a type's decorator takes a `*vuka.Type`.

## Files

A string literal passed where a decorator or a typed attribute takes a
`vuka.File` names a file of the source tree, relative to the package's
directory:

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

| | |
|---|---|
| `f.Bytes()` | the content (a `File` Vuka didn't embed, such as `vuka.File("x")`, is read from disk) |
| `f.Path()`, `f.Pkg()` | `"views/pet.html"`; the package's import path |
| `templx.Components(f)` | for a `.templ` file, with templ and [ui](/ui/templ): its components in source order: <span v-pre>`[]templx.Component{{Name: "Show", Func: Show, Params: []string{"name"}}}`</span>, `Func` a `func(…) templ.Component` |

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

The parameter or field must be `vuka.File` itself; literals elsewhere aren't
embedded. The web framework's views are files of this kind: see [Views](/web/views).

## Details

A function keeps its name for a wrapper that builds the decorated function once,
on first call; recursion goes through the decorators. Methods, `init`,
overloads, generic functions and `type ( … )` groups can be decorated. In the
editor, typing `@` lists every decorator in the project, and picking one from
another package adds the import.
