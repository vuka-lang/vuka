# Decorators

**Go:** cross-cutting behaviour (logging, retries, caching, auth) is written into
each function, or wired with middleware by hand.

**Vuka adds** decorators, as easy as Python's. One decorator works on any
function or method, whatever its signature:

<!-- go:context
type Receipt struct{ ID int }

type User struct{ Name string }

type Store struct{ users []User }
-->

::: vuka
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
:::

::: go
In Go a decorator is a function of a `*vuka.Call`, and one with parameters a
function returning one:

```go
func logged(c *vuka.Call) {
	fmt.Println("->", c.Name, c.Args)
	c.Next() // run the function (or the next decorator)
	fmt.Println("<-", c.Name, c.Results)
}

func retry(times int) vuka.Decorator {
	return func(c *vuka.Call) {
		for i := 0; i < times; i++ {
			if c.Next(); c.Err() == nil {
				return
			}
		}
	}
}
```
:::

## In Go

A decorated function is `vuka.Wrap(fn, info, decorators…)`: the function
with the decorators around each of its calls, outermost first, `info` the
name and attributes they see. A method keeps its name and calls its
wrapped method expression, built on first use:

::: vuka
```vuka
@retry(3)
@logged
func charge(ctx context.Context, id int) (Receipt, error) { … }

@logged
func (s *Store) Save(u User) error { … }
```
:::

::: go
```go
var charge = vuka.Wrap(chargeOnce, vuka.FuncInfo{Name: "main.charge"}, retry(3), logged)

func chargeOnce(ctx context.Context, id int) (Receipt, error) {
	return Receipt{ID: id}, nil
}

var storeSave vuka.Decorated[func(*Store, User) error]

func (s *Store) Save(u User) error {
	return storeSave.Get(func() func(*Store, User) error {
		return vuka.Wrap((*Store).save, vuka.FuncInfo{Name: "Store.Save", Method: true}, logged)
	})(s, u)
}

func (s *Store) save(u User) error {
	s.users = append(s.users, u)
	return nil
}
```
:::

`vuka.FuncInfo{Attrs, ParamAttrs}` are what `c.Attr` and `c.ParamAttr`
read. `Wrap` calls the function through reflection; registering a typed
adapter for its signature, `vuka.RegisterAdapter`, takes reflection out.
The vuka command writes one per signature a package's decorators wrap —
or, in a package written in Go, its `vuka.Wrap` calls (a function, a method
expression, a function literal) — in `vuka_calls_gen.go` (`vuka gen
-inplace` commits it), an optional
speed-up the main file never depends on: a call through two decorators is
about 500ns by reflection, 110ns with the adapter, on an Apple M1 Pro
(`go test -bench Wrap` in the vuka module). A generic function is always
called by reflection.

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

<!-- go:context
type Order struct{ ID int }

var ErrForbidden = errors.New("forbidden")

func allowed(ctx context.Context, p Perm) bool { return p != "" }
-->

::: vuka
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
:::

::: go
The attributes a decorator reads are the `Attrs` of its `vuka.FuncInfo`:

```go
type Perm string

func guard(c *vuka.Call) {
	var p Perm
	c.Attr(&p)
	if !allowed(c.Context(), p) {
		c.SetErr(ErrForbidden)
		return
	}
	c.Next()
}

var saveOrder = vuka.Wrap(storeOrder, vuka.FuncInfo{
	Name:  "main.saveOrder",
	Attrs: []any{Perm("orders.write")},
}, guard)

func storeOrder(ctx context.Context, o Order) error { return nil }
```
:::

A decorator with parameters is built once per decorated function, so state it
keeps — a cache, a counter — isn't shared.

| | |
|---|---|
| `c.ParamAttr(i, &x)` | the [attribute](/features/attributes#parameter-attributes) written before parameter `i` |

## Optional arguments

Many decorators have a sensible default and only sometimes need settings: run
in a transaction — usually on the default database, now and then read-only or
on another one. Give the decorator only optional arguments (no parameters, or
one `...T`) and it is <span class="vuka-only">written bare</span><span class="go-only">called with no arguments</span> for the default, with arguments otherwise:

<!-- go:context
type TxOption struct{ db string; readOnly bool }

func On(db string) TxOption { return TxOption{db: db} }

var ReadOnly = TxOption{readOnly: true}

type Report struct{ Total int }
-->

::: vuka
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
:::

::: go
In Go the bare form is the call with no arguments:

```go
func transaction(opts ...TxOption) vuka.Decorator {
	return func(c *vuka.Call) {
		// begin, on the database the options name
		c.Next()
		// commit, or roll back on c.Err()
	}
}

var saveUser = vuka.Wrap(insertUser, vuka.FuncInfo{Name: "main.saveUser"}, transaction())

var report = vuka.Wrap(buildReport, vuka.FuncInfo{Name: "main.report"},
	transaction(On("analytics"), ReadOnly))

func insertUser(ctx context.Context, u User) error { return nil }

func buildReport(ctx context.Context) (Report, error) { return Report{}, nil }
```
:::

## Composed decorators

When the same decorators keep appearing together — every API route is a GET
that needs authentication and is timed — name the set once and write that
name instead:

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

::: vuka
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
:::

::: go
In Go a composed decorator is a function returning a `vuka.Bundle`, built by
`vuka.Compose` from call decorators (`func(*vuka.Call)`), declarers
(`func(*vuka.Decl)`), type decorators (`func(*vuka.Type)`), attributes and
other bundles:

```go
func ApiRoute(path string) vuka.Bundle {
	return vuka.Compose(web.Get(path), web.Use(auth), timed)
}

func Audited() vuka.Bundle { return vuka.Compose(logged, Doc("audited")) }

func Service() vuka.Bundle { return vuka.Compose(web.Service, Audited()) }
```

A function takes a bundle's parts where they go — its attributes into
`vuka.RegisterFunc` and the `FuncInfo`, its call decorators into `vuka.Wrap`,
its declarers through `b.Declare` — and a type through
`b.Decorate(vuka.TypeFor[T](NewT, b))`:

```go
var petRoute = ApiRoute("/pets/{id}")

var showPetDec vuka.Decorated[func(int, *PetStore) (Pet, error)]

func showPet(id int, store *PetStore) (Pet, error) {
	return showPetDec.Get(func() func(int, *PetStore) (Pet, error) {
		return vuka.Wrap(findPet, vuka.FuncInfo{Name: "main.showPet", Attrs: petRoute.Attrs()}, petRoute.Calls()...)
	})(id, store)
}

func findPet(id int, store *PetStore) (Pet, error) { return store.pets[id], nil }

func init() {
	vuka.RegisterFunc(showPet, petRoute.Attrs()...)
	petRoute.Declare(vuka.DeclOf(showPet, vuka.Param{Name: "id"}, vuka.Param{Name: "store"}))
}
```

For the web framework's own routes the registration call is shorter: the
route's options and middleware go with it,
`web.Routes.Get("/pets/{id}", showPet, web.Use(auth), web.Params("id", "store"))`.
:::

## Type-level advice

To give every method of a type the same behaviour — log each call to a store,
retry each call to a flaky client — put the call decorator on the type. It
wraps every exported method the package's Vuka files declare for the type, on
`T` or `*T`:

::: vuka
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
:::

::: go
In Go the advice is a list of decorators the methods share, put first in
each method's `vuka.Wrap`; a method that shouldn't be advised doesn't use it:

```go
func storeAdvice() []vuka.Decorator { return []vuka.Decorator{logged, retry(3)} }

var storeSaveDec vuka.Decorated[func(*Store, User) error]

func (s *Store) Save(u User) error { // logged, then retried
	return storeSaveDec.Get(func() func(*Store, User) error {
		return vuka.Wrap((*Store).save, vuka.FuncInfo{Name: "Store.Save", Method: true}, storeAdvice()...)
	})(s, u)
}

var storeLoadDec vuka.Decorated[func(*Store, int) (User, error)]

func (s *Store) Load(id int) (User, error) { // logged, retried, then timed
	return storeLoadDec.Get(func() func(*Store, int) (User, error) {
		return vuka.Wrap((*Store).load, vuka.FuncInfo{Name: "Store.Load", Method: true}, append(storeAdvice(), timed)...)
	})(s, id)
}

func (s *Store) load(id int) (User, error) { return s.users[id], nil }

func (s *Store) Close() error { return nil } // left alone
```
:::

## Typed decorators

For hot paths, a decorator can be `func(F) F` for the function's type: no
boxing, full types. <span class="vuka-only">Vuka picks the form from the decorator's type, and the two
mix freely.</span><span class="go-only">In Go it is applied by calling it: `var load = cached(loadUser)`.</span>

## Types

A type decorator runs at start-up with the type: `func Model(t *vuka.Type)` gets
`t.Name`, `t.Reflect`, `t.Attrs` — and, for a struct, a constructor for
[dependency injection](/features/dependency-injection): the type's `NewX`
(its static `New`), <span class="vuka-only">else one Vuka writes taking each injected field.</span><span class="go-only">the one you pass.</span>
<span class="vuka-only">In Go, `@Model` on `User` is</span><span class="go-only">In Go it is called with the type, at init:</span> `Model(vuka.TypeFor[User](NewUser))`.

## Declaration decorators

A decorator of type `func(d *vuka.Decl)` — or a call returning one — is a
**declarer**: it doesn't wrap calls, it runs once at program start with a
description of the declaration. That is how a library registers routes,
commands or jobs from the functions it decorates. A small command registry:

::: vuka
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

The [web framework](/web/routes) declares its routes and [crons](/web/crons) this way.

A declarer only runs if its package is compiled in, and Go compiles only what
is imported — so the `vuka` command links declarer packages itself. `vuka
build`, `run`, `test` and `vet` find every package of the module with a
declarer, a type decorator or a [composed decorator](#composed-decorators) on
a declaration and give each main package a generated file of blank imports
(`vuka gen` writes it as `vuka_link_gen.go` for plain go tools). Nothing to
write, nothing to forget: a new page package exists in the binary the moment
it is saved. Test files don't count, and a `//vuka:nolink` line above a
package clause opts that package — or, on a main, the whole program — out;
see [the CLI](/tools/cli#build-run-test-vet-install).

A package also counts when one of its struct types embeds a type that
declares itself: one with a `vukaDeclares()` method, unexported, with no
parameters or results. [ORM models](/orm/models) link this way — `orm.Base`
has the marker — so a model package nothing imports is still migrated. A
library whose embeddable type registers its embedders at init marks it the
same way:

```go
func (Base[T]) vukaDeclares() {}
```

Only `.vuka` files are looked at; a `.go` file's embedding doesn't link its
package.
:::

::: go
In Go a declarer is called at init with `vuka.DeclOf(fn, params…)`, which
describes a function or method expression — its attributes those
registered for it (`vuka.RegisterFunc`); `vuka.Param` gives each
parameter's name, which reflection can't see, and its attributes:

```go
import "reflect"

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

var greetDec vuka.Decorated[func(string) string]

func greet(name string) string {
	return greetDec.Get(func() func(string) string {
		return vuka.Wrap(hello, vuka.FuncInfo{Name: "main.greet"}, logged)
	})(name)
}

func hello(name string) string { return "hello " + name }

func init() {
	Command("greet")(vuka.DeclOf(greet, vuka.Param{Name: "name"}))
}
```

<!-- go:fragment -->
```go
run("greet", map[string]string{"name": "ada"}) // -> main.greet [ada] …
```

The [web framework](/web/routes) declares its routes and [crons](/web/crons)
this way. A package whose `init` registers runs only if it is compiled in,
and Go compiles only what is imported: import it from main, blank when
nothing else does (`import _ "example.com/app/pets"`).
:::

| `*vuka.Decl` | |
|---|---|
| `d.Name`, `d.Pkg` | `"main.greet"`, or `"Shell.Run"` for a method; the import path |
| `d.File`, `d.Line` | where it is declared, for messages |
| `d.Func` | the function, wrapped by its other decorators (`logged` runs when it is called); for a method, the method expression `func(recv, args…)` |
| `d.Recv` | the receiver's type, for a method |
| `d.Params` | names (as written; `""` when unnamed), types and [attributes](/features/attributes#parameter-attributes) (`p.Attr(&x)`), receiver excluded |
| `d.Results`, `d.Variadic` | result types; whether the last parameter is `...T` |
| `d.Attr(&x)` | a typed attribute on the declaration |

<span class="vuka-only">In Go, `@Command("greet")` on `greet` is
`Command("greet")(vuka.DeclOf(greet, vuka.Param{Name: "name"}))`: `DeclOf`
describes a function or method expression, the attributes those registered
for it (`vuka.RegisterFunc`).</span>

::: vuka
Declarers run in the package's `init`, after its variables are initialised:
declarations in source order (files in name order, as Go orders `init`), and a
declaration's declarers top to bottom. They mix freely with call and typed
decorators. A declarer needs a concrete function: on a generic function, `init`
or a type it is an error — a type's decorator takes a `*vuka.Type`.
:::

::: go
A declarer runs where it is called, usually an `init`. It needs a concrete
function: a generic function has no one function to describe.
:::

## Registries

<!-- go:context
func requireToken(next http.Handler) http.Handler { return next }

func pageLayout(page ui.Node) ui.Node { return page }

type Board struct{ ui.Live }

func NewBoard() *Board { return &Board{} }

func ShowPet(id int, store *PetStore) (Pet, error) { return store.pets[id], nil }
-->

::: vuka
A library that registers what it is given — routes, pages, services — offers
its decorators as methods of a **registry**: an exported package-level
variable whose type has the marker method `vukaRegistry()` (unexported, no
parameters or results). A decorator naming a method of its package's
registry is a call of that method at init with the declaration: the
function, a method's expression, or a type's constructor. The decorator's
arguments come first, as many as the method takes before the declaration,
and the rest after it:

```vuka
@web.Get("/pets/{id}")
@web.Use(requireToken)
func ShowPet(id int, store *PetStore) Result[Pet] { … }

@web.Live("/board", web.Layout(pageLayout))
type Board struct{ ui.Live … }
```

is the Go a person would write:

```go
func init() {
	web.Routes.Get("/pets/{id}", ShowPet, web.Use(requireToken), web.Params("id", "store"))
	web.Routes.Live("/board", NewBoard, web.Layout(pageLayout))
}
```

The other decorators on the declaration whose values the method takes as
its options (its variadic parameter's type) go with the call — `@web.Use`
here; a [composed decorator](#composed-decorators) is never one, it
decorates the declaration itself (an auth gate wraps the function and the
route reads it from the function's attributes). When the registry's package
has `Params(names ...string)` returning such an option, a function whose
parameters include one only a name tells apart (a string, a number, a
`TextUnmarshaler`) gets its parameters' names with it, since reflection
can't see them. A registry decorator needs a concrete function, as a
declarer does.

```go
type Registry struct{ … }

func (*Registry) vukaRegistry() {}

var Routes = &Registry{}

func (r *Registry) Get(path string, handler any, opts ...RouteOption) *Registry { … }
```
:::

::: go
A library that registers what it is given — routes, pages, services —
offers it as methods of an exported package-level variable, a
**registry**: `web.Routes`. Registering is a call at init, with the
function, a method's expression or a type's constructor, and the
options after it:

```go
func init() {
	web.Routes.Get("/pets/{id}", ShowPet, web.Use(requireToken), web.Params("id", "store"))
	web.Routes.Live("/board", NewBoard, web.Layout(pageLayout))
}
```

`web.Params` names the handler's parameters, which reflection can't see:
scalars bind from the path or query by name. A registry type marks itself
with an unexported `vukaRegistry()` method, so that `.vuka` files may call
its methods as decorators.
:::

## Files

A string literal passed where a decorator or a typed attribute takes a
`vuka.File` names a file of the source tree, relative to the package's
directory:

::: vuka
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
:::

::: go
In Go, embed the file with `//go:embed` and make its `File` with
`vuka.FileOf("importpath:path", content)`; a plain `vuka.File("views/pet.html")`
is read from disk instead:

```go
import _ "embed"

var pages = map[string]vuka.File{}

func Page(view vuka.File) func(*vuka.Decl) {
	return func(d *vuka.Decl) { pages[d.Name] = view }
}

//go:embed views/pet.html
var petView string

func showPetPage(id string) Pet { return Pet{Name: id} }

func init() {
	Page(vuka.FileOf("example.com/app:views/pet.html", petView))(vuka.DeclOf(showPetPage, vuka.Param{Name: "id"}))
}
```
:::

| `f.Bytes()` | the content (a `File` Vuka didn't embed, such as `vuka.File("x")`, is read from disk) |
| `f.Path()`, `f.Pkg()` | `"views/pet.html"`; the package's import path |
| `templx.Components(f)` | for a `.templ` file, with templ and [ui](/ui/templ): its components in source order: <span v-pre>`[]templx.Component{{Name: "Show", Func: Show, Params: []string{"name"}}}`</span>, `Func` a `func(…) templ.Component` |

::: vuka
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
:::

::: go
The web framework's views are files of this kind: see [Views](/web/views).
:::

## Details

::: vuka
A function wrapped by its decorators keeps its name for a wrapper that builds
the decorated function once, on first call (`vuka.Decorated`); recursion goes
through the decorators. One only declarers or registries take stays as it is. Methods, `init`,
overloads, generic functions and `type ( … )` groups can be decorated. In the
editor, typing `@` lists every decorator in the project, and picking one from
another package adds the import.
:::

::: go
`vuka.Decorated` builds a decorated function once, on its first call, which
is how a function or method keeps its name with decorators around it; a
recursive call through that name goes through the decorators. For a generic
function, `vuka.Instances` holds one decorated function per instantiation.
:::
