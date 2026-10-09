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
commands or jobs from the functions it decorates:

```vuka
package web

type route struct {
	method, path string
	decl         *vuka.Decl
}

var routes []route

func Get(path string) func(*vuka.Decl) {
	return func(d *vuka.Decl) { routes = append(routes, route{"GET", path, d}) }
}

// serve calls a handler with its parameters bound by name.
func serve(r route, params map[string]string) []reflect.Value {
	var args []reflect.Value
	for _, p := range r.decl.Params {
		args = append(args, reflect.ValueOf(params[p.Name]).Convert(p.Type))
	}
	return reflect.ValueOf(r.decl.Func).Call(args)
}
```

```vuka
@web.Get("/pets/{id}")
@logged
func showPet(id string) Pet { … }
```

| `*vuka.Decl` | |
|---|---|
| `d.Name`, `d.Pkg` | `"main.showPet"` or `"PetAdmin.Index"`; the import path |
| `d.File`, `d.Line` | where it is declared, for messages |
| `d.Func` | the function, wrapped by its other decorators (`@logged` runs when it is called); for a method, the method expression `func(recv, args…)` |
| `d.Recv` | the receiver's type, for a method |
| `d.Params` | names (as written; `""` when unnamed) and types, receiver excluded |
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
func Template(view vuka.File) func(*vuka.Decl) { … }

@web.Template("views/pet.html")
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
| `vuka.TemplComponents(f)` | for a `.templ` file of the same package, its components by name: `map[string]any{"Show": Show}`, each a `func(…) templ.Component` |

The parameter or field must be `vuka.File` itself; literals elsewhere aren't
embedded.

## Details

A function keeps its name for a wrapper that builds the decorated function once,
on first call; recursion goes through the decorators. Methods, `init`,
overloads, generic functions and `type ( … )` groups can be decorated. In the
editor, typing `@` lists every decorator in the project, and picking one from
another package adds the import.
