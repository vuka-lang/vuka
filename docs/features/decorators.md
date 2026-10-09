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

## Details

A function keeps its name for a wrapper that builds the decorated function once,
on first call; recursion goes through the decorators. Methods, `init`,
overloads, generic functions and `type ( … )` groups can be decorated. In the
editor, typing `@` lists every decorator in the project, and picking one from
another package adds the import.
