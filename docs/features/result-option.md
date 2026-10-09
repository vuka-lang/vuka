# Result and Option

**Go:** a value and an error travel as a pair, `(T, error)`; an absent value is a
pointer, a zero value, or a second boolean.

**Vuka adds** two types for when you want the outcome as one value — to store
it, pass it around, or [`match`](/features/match) on it.

```vuka
func find(id int) Result[User] {
	u, ok := users[id]
	if !ok {
		return Err(&NotFound{ID: id})
	}
	return Ok(u)
}

func nickname(u User) Option[string] {
	if u.Name == "linus" {
		return Some("torvalds")
	}
	return None
}
```

Write `Result`, `Option`, `Ok`, `Err`, `Some` and `None` unqualified; Vuka
imports the runtime for you. `Err` and `None` take their type from where the
value goes — `return Err(e)` in a function returning `Result[User]` becomes
`vuka.Err[User](e)`.

## Go's error, not a new one

A Result's error is Go's `error`, so it meets Go code halfway:

| | |
|---|---|
| `vuka.Of(f())` | `(T, error)` → `Result[T]` |
| `r.Get()` | `Result[T]` → `(T, error)` |
| `r.IsOk()`, `r.IsErr()` | which it is |
| `r.Value()`, `r.Err()` | the value (zero on error), the error (nil on success) |
| `r.Unwrap()`, `r.UnwrapOr(def)` | the value, or a panic / a default |
| `vuka.Map`, `vuka.AndThen` | transform, chain |

Option has the same shape: `Some`, `None`, `o.Get()` (Go's comma-ok),
`IsSome`, `IsNone`, `Unwrap`, `UnwrapOr`, `vuka.FromPtr`.

## What it becomes

```go
func find(id int) vuka.Result[User] {
	u, ok := users[id]
	if !ok {
		return vuka.Err[User](&NotFound{ID: id})
	}
	return vuka.Ok(u)
}
```
