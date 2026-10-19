# match

**Go:** `switch` compares values or types; taking a value apart is done by hand.

**Vuka adds** `match`: cases are patterns, tried in order, that test and bind at
once.

<!-- go:context
type User struct {
	ID   int
	Name string
}

type NotFound struct{ ID int }

func (e *NotFound) Error() string { return fmt.Sprintf("user %d not found", e.ID) }

func lookup(arg string) vuka.Result[User] {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return vuka.Err[User](err)
	}
	return vuka.Err[User](&NotFound{ID: id})
}
-->

::: vuka
```vuka
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
:::

::: go
In Go, take the value apart first, then `switch` on what you need —
`errors.As` for an error's type, `errors.Is` for a sentinel:

```go
func report(arg string) {
	u, err := lookup(arg).Get()
	var nf *NotFound
	switch {
	case err == nil:
		fmt.Println(u.Name)
	case errors.As(err, &nf):
		fmt.Println("missing user", nf.ID)
	case errors.Is(err, strconv.ErrSyntax):
		fmt.Println("not a number:", arg)
	default:
		fmt.Println("error:", err)
	}
}
```
:::

::: vuka
## Patterns

| Pattern | Matches |
|---|---|
| `_`, `default` | anything |
| `x` | anything, bound to `x` (a constant's name compares instead) |
| `0`, `"a"`, `Max`, `pkg.Const`, `true` | an equal value |
| `^x` | the current value of the variable `x` (Elixir's pin) |
| `Ok(p)`, `Err(p)`, `Some(p)`, `None` | a Result or Option, then `p` inside |
| `T{F: p, …}`, `&T{…}` | a struct's fields; against an interface, a type test too |
| `Status.Draft`, `Shape.Circle{Radius: p}` | an [enum](/features/enums)'s variant |
| `1, 2, 3` | any of them |

`case p if cond:` adds a guard.

## Exhaustive

A `match` must cover every case — Ok and Err, Some and None, true and false,
every variant of an enum, or a
case that takes anything — and a case after one that matches everything is an
error. Both are reported before Go sees the code:

```text
main.vuka:4:2: match on Result[int] isn't exhaustive: missing Err(_); add the missing cases or case _:
```

Coverage goes inside patterns: `Ok` is covered when the patterns inside the Ok
cases cover the Result's value, and a struct when its fields' patterns do,
together. So on a `Result[Option[User]]`

```vuka
match r {
case Ok(Some(u)):
	…
case Ok(None):
	…
case Err(e):
	…
}
```

needs no catch-all, while leaving out `case Ok(None):` reports
`missing Ok(None)`. A case whose patterns earlier cases already match entirely
— `case Ok(Some(u)):` after `case Ok(_):` — is unreachable, and an error too.
Guarded cases cover nothing; literals, constants and type tests cover only a
value of their own, so `Some(1)` leaves `Some(_)` missing.

## What it becomes

An `if`/`else if` chain that tests each pattern in turn with the Result's and
Option's own methods (`IsOk`, `Value`, `Err`, `IsSome`), bodies where you
wrote them, ending in `panic("vuka: no case matched")` — which exhaustiveness
makes unreachable.
:::

::: go
## Nested values

A `vuka.Result[vuka.Option[User]]` comes apart one level at a time, with a
`switch` once the pieces are plain values:

```go
func describe(r vuka.Result[vuka.Option[User]]) string {
	o, err := r.Get()
	if err != nil {
		return "error: " + err.Error()
	}
	u, ok := o.Get()
	switch {
	case !ok:
		return "none"
	case u.Name == "":
		return "unnamed"
	default:
		return u.Name
	}
}
```

Go doesn't check that a `switch` covers every case: end it with a `default`
that handles — or panics on — what's left. A type switch takes interfaces
apart by type:

```go
func kind(err error) string {
	switch e := err.(type) {
	case nil:
		return "ok"
	case *NotFound:
		return fmt.Sprint("missing ", e.ID)
	default:
		return "error"
	}
}
```
:::
