# Result & Option

[[toc]]

## Introduction

In Go, a value and an error travel as a pair, `(T, error)`, and an absent value
is a pointer, a zero value, or a second boolean.

Vuka adds two types for when you want the outcome as one value — to store it,
pass it around, or [`match`](/features/match) on it. `Result[T]` holds a value
or an error; `Option[T]` holds a value or nothing.

## Returning a Result or an Option

<!-- go:context
type User struct {
	ID   int
	Name string
}

type NotFound struct{ ID int }

func (e *NotFound) Error() string { return fmt.Sprintf("user %d not found", e.ID) }

var users = map[int]User{}
-->

This sample returns a `Result` from a lookup and an `Option` from a function
that may have nothing to give:

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

## Working With Go's error

A Result's error is Go's `error`, not a new one, so it meets Go code halfway.
These are the functions and methods that move between the two:

| | |
|---|---|
| `vuka.Of(f())` | `(T, error)` → `Result[T]` |
| `r.Get()` | `Result[T]` → `(T, error)` |
| `r.IsOk()`, `r.IsErr()` | which it is |
| `r.Value()`, `r.Err()` | the value (zero on error), the error (nil on success) |
| `r.Unwrap()`, `r.UnwrapOr(def)` | the value, or a panic / a default |
| `vuka.Map`, `vuka.AndThen` | transform, chain |

### Option's Methods

Option has the same shape: `Some`, `None`, `o.Get()` (Go's comma-ok),
`IsSome`, `IsNone`, `Unwrap`, `UnwrapOr`, `vuka.FromPtr`, `vuka.MapOption`.

### Option in JSON and GraphQL {#option-json}

An `Option` is its value in JSON, or `null` for None, both ways; `IsZero`
reports None, so a field tagged `json:",omitzero"` leaves a None out:

```vuka
type Product struct {
	Name       string
	SKU        Option[string]    `json:"sku"`
	ReleasedAt Option[time.Time] `json:"releasedAt,omitzero"`
}
// Product{Name: "kettle", SKU: Some("K-1")} is {"Name":"kettle","sku":"K-1"}
```

A value cached or sent as JSON keeps its optional fields as they are. In a
GraphQL schema an `Option[T]` is a nullable `T` — as a field, a result, an
argument, an input field or a list's item (see
[Nullability](/web/graphql#nullability)) — so an ORM model's optional
columns serve as they are, with no pointers to convert to.

## What It Becomes

The `find` function above compiles to this Go:

```go
func find(id int) vuka.Result[User] {
	u, ok := users[id]
	if !ok {
		return vuka.Err[User](&NotFound{ID: id})
	}
	return vuka.Ok(u)
}
```
