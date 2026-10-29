# The ? Operator

[[toc]]

## Introduction

In Go, every fallible call is followed by the same three lines. Vuka adds `?`:
on failure the function returns, passing the error on.

You reach for it whenever a function would otherwise check and return each
error by hand. It works on Go's `(T, error)` as well as on
[Result & Option](/features/result-option).

What `?` becomes is the `if err != nil` Go you would write: the value named
after what it holds, the error `err`, one statement per call in the order Go
calls them. `vuka eject` writes it out, and it reads as hand-written Go —
using `?` is never a commitment to keep using it.

## Returning Errors Early

<!-- go:context
type User struct {
	ID   int
	Name string
}

var users = map[int]User{}

func find(id int) vuka.Result[User] {
	u, ok := users[id]
	if !ok {
		return vuka.Err[User](fmt.Errorf("user %d not found", id))
	}
	return vuka.Ok(u)
}

var ErrEmailTaken = errors.New("email taken")

func exists(ctx context.Context, email string) (bool, error) { return false, nil }

func create(ctx context.Context, email string) error { return nil }

type DB struct{}

func (*DB) Close() error { return nil }

func openDB() (*DB, error) { return &DB{}, nil }

func serve(db *DB) error { return nil }
-->

This is the Go form, checking the error by hand:

```go
func parseID(arg string) (int, error) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return 0, err
	}
	return id, nil
}
```

With `?`, each fallible call is one line:

```vuka
func lookup(arg string) Result[User] {
	id := strconv.Atoi(arg)?          // Go's (T, error) works directly
	u := find(id)?                    // so does a Result
	return Ok(u)
}
```

## Where ? Works

`?` goes right after the expression it unwraps, anywhere an expression does:

```vuka
x := f()?
return f()?
f()?
if users.Exists(ctx, email)? { … }
total := price(ctx, item)? * qty(ctx, cart)?
for rows.Next(ctx)? { … }
for _, p := range listPosts(ctx)? { … }
switch status(ctx, order)? { … }
send(ctx, render(load(ctx, id)?)?)?
```

It binds tighter than any operator, as a call does: `-f()?` is `-(f()?)`,
`load(id)?.Name` the loaded value's `Name`.

The operand can be Go's `(T…, error)`, an `error`, a `Result`, or an `Option`.
On failure the function returns what its results call for: zero values and
the error, `Err(e)`, or `None` (in a function returning an `Option`; in one
returning an error, [`opt?(err)`](#adding-context) says which error a `None`
is).

The exceptions, each [E0110](/errors/E0110): a `case` of a `switch` or
`select` (a switch tries its cases one by one, so pull the call into a
statement before it, or use `if`/`else`), a `for` loop's post statement, and
markup. An `error` alone yields no value, so its `?` ends a statement. A `?`
inside a closure returns from the closure.

## What It Becomes

Each `?` becomes the assignment and the `if err != nil { return … }` that Go
code writes. A `?` inside an expression moves its call into a statement
before the one holding it, and Go's order of evaluation is kept: the calls
before it are called into variables first, `&&` and `||` still skip their
right side, an `else if` is tested only when the branches before it weren't
taken.

```vuka
func register(ctx context.Context, email string) error {
	if exists(ctx, email)? {
		return ErrEmailTaken
	}
	return create(ctx, email)
}
```

is

```go
func register(ctx context.Context, email string) error {
	exists, err := exists(ctx, email)
	if err != nil {
		return err
	}
	if exists {
		return ErrEmailTaken
	}
	return create(ctx, email)
}
```

| Written | Becomes |
|---|---|
| `if f()? {` | `exists, err := f()`, the check, `if exists {` |
| `if x := g(); f(x)? {` | `x := g()` and the call before the `if` (in a block when `x` would clash with a later name) |
| `} else if f()? {` | after branches that all return, break, continue or panic: the `else` dropped, the call after the `if`; else `} else {`, the call, `if` |
| `if a && f()? {` without `else` | `if a { allowed, err := f(); …; if allowed { … } }` |
| `if a && f()? {` with `else` | `var allowed bool` and `if a { … allowed, err = f() … }` before `if allowed {` |
| `for f()? {` | `for { next, err := f(); …; if !next { break }; … }` — `continue` still runs the post statement |
| `for range f()? {`, `switch f()? {` | the call and its check before the loop or switch |
| `g() > f()?` | `g()` into a variable first: Go calls `g` first (plain reads stay put; Go leaves their order open) |
| `send(render(load(id)?)?)?` | `user, err := load(id)`, `page, err := render(user)`, `if err := send(page); err != nil {` |

A value is named after its type (`*User` is `user`, `[]Post` `posts`), else
after the function it comes from (`Exists` is `exists`, `GetUser` `user`).
Where that name would hide one the code uses later, it is `v`, then `v2`.
The moved statements keep `//line` directives to the `?`, so a panic or a
debugger stops on the line you wrote.

`vuka vet` warns about an expression with more than one `?`
([W0109](/errors/W0109)) and a `?` on the right of `&&` or `||`
([W0110](/errors/W0110)): each is a way out of the function hidden in a
line, and a statement of its own makes it plain.

## Adding Context

`?(…)` says what the function was doing when the call failed. Its argument
is evaluated only on failure.

| Written | On failure |
|---|---|
| `f()?("checking email")` | `fmt.Errorf("checking email: %w", err)` |
| `f()?("loading user %d", id)` | `fmt.Errorf("loading user %d: %w", id, err)` |
| `f()?(wrap)`, `wrap` a `func(error) error` | `wrap(err)` |
| `f()?(ErrUnavailable)`, an error | `fmt.Errorf("%w: %w", ErrUnavailable, err)` — `errors.Is` matches both |
| `f()?(RegisterError.Database)` | `RegisterError.Database{Err: err}`, a variant whose only field is an `error` |
| `opt?(ErrNoUser)`, an `Option` | `ErrNoUser` (a message: `errors.New("…")`) |

```vuka
func signUp(ctx context.Context, email string) error {
	if exists(ctx, email)?("checking email") {
		return ErrEmailTaken
	}
	create(ctx, email)?("creating %s", email)
	return nil
}
```

The errors stay chained: `errors.Is(err, sql.ErrNoRows)` still finds the
error a context wraps. A module can have `vuka vet` ask for context on every
`?` passing on another package's error ([W0111](/errors/W0111)):

```toml
[vet]
wrap_errors = true
```

## main and Tests

In `main`, a `?` exits the program: the body runs as a function returning
an error, so deferred calls run first, then `main` prints `error: …` to
standard error and exits with status 1.

```vuka
func main() {
	db := openDB()?
	defer db.Close()
	serve(db)?
}
```

is

```go
func main() {
	if err := main_(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func main_() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := serve(db); err != nil {
		return err
	}
	return nil
}
```

In a function taking a `*testing.T`, `*testing.B` or `testing.TB` that
returns no error, a `?` fails the test: `t.Helper(); t.Fatal(err)`.

```vuka
func TestRegister(t *testing.T) {
	a := newAccounts(t)?
	if a.Exists(ctx, "a@example.com")? {
		t.Fatal("registered before signing up")
	}
}
```
