# match

[[toc]]

## Introduction

In Go, `switch` compares values or types, and taking a value apart is done by
hand. Vuka adds `match`: cases are patterns, tried in order, that test and bind
at once.

You use it to take apart a [Result or Option](/features/result-option), an
[enum](/features/enums), or a struct, and the compiler checks that every case
is covered.

## Matching a Value

<!-- vuka:context
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

This sample matches the Result of a lookup, taking apart a specific error type
and using a guard for another:

::: code-group

```vuka [Vuka]
func show(arg string) {
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
}
```

```go [Generated Go]
func show(arg string) {
	if m1 := lookup(arg); false {
		panic(m1)
	} else if u := m1.Value(); m1.IsOk() {
		fmt.Println(u.Name)
	} else if id, ok := func() (id int, ok bool) {
		if !(m1.IsErr()) {
			return
		}
		var p1 *NotFound
		if !errors.As(m1.Err(), &p1) {
			return
		}
		if !(p1 != nil) {
			return
		}
		id = p1.ID
		ok = true
		return
	}(); ok {
		fmt.Println("missing user", id)
	} else if e := m1.Err(); m1.IsErr() && (errors.Is(e, strconv.ErrSyntax)) {
		_ = e
		fmt.Println("not a number:", arg)
	} else if e := m1.Err(); m1.IsErr() {
		fmt.Println("error:", e)
	} else {
		panic("vuka: no case matched")
	}
}
```

:::

## Matching Errors

<!-- vuka:context
var ErrNotFound = errors.New("not found")

type ValidationError struct{ Field string }

func (e *ValidationError) Error() string { return "invalid " + e.Field }

type User struct {
	ID   int
	Name string
}

func findUser(email string) (*User, error) { return nil, ErrNotFound }
-->

`match` takes a call returning `(T, error)` as it takes a Result, and its
`Err` patterns test the error the way Go does, through every error it
wraps:

::: code-group

```vuka [Vuka]
func login(email string) string {
	match findUser(email) {
	case Ok(u):
		return "hello " + u.Name
	case Err(ErrNotFound):
		return "unknown email"
	case Err(&ValidationError{Field: f}):
		return "invalid " + f
	case Err(e):
		return e.Error()
	}
}
```

```go [Generated Go]
func login(email string) string {
	if u, err := findUser(email); err == nil {
		return "hello " + u.Name
	} else if errors.Is(err, ErrNotFound) {
		return "unknown email"
	} else if f, ok := func() (f string, ok bool) {
		var p1 *ValidationError
		if !errors.As(err, &p1) {
			return
		}
		if !(p1 != nil) {
			return
		}
		f = p1.Field
		ok = true
		return
	}(); ok {
		return "invalid " + f
	} else if e := err; err != nil {
		return e.Error()
	} else {
		panic("vuka: no case matched")
	}
}
```

:::

The call runs once, in the `if` that tests the first case, its value and error that `if`'s own variables.

- `Err(ErrNotFound)`, `Err(fs.ErrNotExist)`: a sentinel, tested with
  `errors.Is` — so an error wrapped with `fmt.Errorf("…: %w", err)` (or
  `?("…")`) still matches. A bare name in a pattern against an error is a
  sentinel when it names a package-level variable of type `error`.
- `Err(&ValidationError{Field: f})`: an error type, found in the chain with
  `errors.As`; its fields are patterns as in any struct pattern.
- `Err(e)`: any error, bound to `e`.

The same holds for a `match` on an `error` value itself: `case ErrNotFound:`
is `errors.Is`, `case &ValidationError{}:` is `errors.As`, and `case nil:`
compares.

The value is named after what a case's `Ok` binds it to, else its type
(`*User` is `user`) or the call, and the error is `err`; a name a case uses
for something else, or one in scope, takes the next (`v`, `err2`). A call
returning anything other than `(T, error)` is an error.

## Patterns

The table below lists every pattern a case can use:

| Pattern | Matches |
|---|---|
| `_`, `default` | anything |
| `x` | anything, bound to `x` (a constant's name compares instead) |
| `0`, `"a"`, `Max`, `pkg.Const`, `true` | an equal value; against an error, `errors.Is` |
| `^x` | the current value of the variable `x` (pinned, not bound) |
| `Ok(p)`, `Err(p)`, `Some(p)`, `None` | a Result or Option, then `p` inside |
| `T{F: p, …}`, `&T{…}` | a struct's fields; against an interface, a type test too (against an error, `errors.As`) |
| `Status.Draft`, `Shape.Circle{Radius: p}` | an [enum](/features/enums)'s variant |
| `1, 2, 3` | any of them |

### Guards

`case p if cond:` adds a guard.

## Exhaustive Matching

A `match` must cover every case — Ok and Err, Some and None, true and false,
every variant of an enum, or a
case that takes anything — and a case after one that matches everything is an
error. Both are reported before Go sees the code:

```text
main.vuka:4:2: match on Result[int] isn't exhaustive: missing Err(_); add case Err(e): …, or case _:
```

On a call returning `(T, error)` the message names it so: `match on (User,
error) isn't exhaustive`. In the editor the error is on the `match` and its
subject as you type, and its quick fix **Add missing cases** writes them at
the end of the match: a missing `Err(_)` returns the error when the function
can (`return nil, "", e`, or `return Err(e)` from one returning a Result);
any other case gets a `// TODO` to fill in, which doesn't compile in a
function with results until it returns. A misspelt pattern — `Error(…)` for
`Err(…)`, `ok(u)`, `none`, a variant that doesn't exist — is an error on the
name with the right spelling as its quick fix, and completion after `case`
offers the patterns that fit the subject.

### Coverage Inside Patterns

Coverage goes inside patterns: `Ok` is covered when the patterns inside the Ok
cases cover the Result's value, and a struct when its fields' patterns do,
together. So on a `Result[Option[User]]`, this match

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

## What It Becomes

An `if`/`else if` chain that tests each pattern in turn with the Result's and
Option's own methods (`IsOk`, `Value`, `Err`, `IsSome`), bodies where you
wrote them, ending in `panic("vuka: no case matched")` — which exhaustiveness
makes unreachable. A match whose cases have no guards and simple patterns is
a `switch`. On a `(T, error)` call the tests are `err == nil`, `errors.Is`
and `errors.As` on the call's own variables (see
[Matching Errors](#matching-errors)).
