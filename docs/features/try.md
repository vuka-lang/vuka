# The ? operator

**Go:** every fallible call is followed by the same three lines.

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
-->

```go
func parseID(arg string) (int, error) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return 0, err
	}
	return id, nil
}
```

**Vuka adds** `?`: on failure the function returns, passing the error on.

::: vuka
```vuka
func lookup(arg string) Result[User] {
	id := strconv.Atoi(arg)?          // Go's (T, error) works directly
	u := find(id)?                    // so does a Result
	return Ok(u)
}
```
:::

::: go
In Go, each step is checked where it happens; a `vuka.Result` comes back to
`(T, error)` with `Get`:

```go
func lookup(arg string) vuka.Result[User] {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return vuka.Err[User](err)
	}
	u, err := find(id).Get()
	if err != nil {
		return vuka.Err[User](err)
	}
	return vuka.Ok(u)
}
```

The same function in Go's own shape:

```go
func lookupUser(arg string) (User, error) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return User{}, err
	}
	return find(id).Get()
}
```
:::

::: vuka
## Where it works

`?` ends a statement:

```vuka
x := f()?
x = f()?
var x = f()?
return f()?
f()?
```

The operand can be Go's `(T…, error)`, an `error`, a `Result`, or an `Option`
(in a function returning an `Option`). On failure the function returns what its
results call for: zero values and the error, `Err(e)`, or `None`.

## What it becomes

The call stays where you wrote it, so nothing is reordered: each `?` becomes
the assignment and the `if err != nil { return … }` that Go code writes, as in
the Go form of `lookup` above.

Using `?` inside a larger expression, or in an `if`, `for` or `switch` header, is
an error that tells you to give it its own statement.
:::

::: go
An `Option` steps the same way with its comma-ok `Get`:

```go
func firstName(u vuka.Option[User]) vuka.Option[string] {
	v, ok := u.Get()
	if !ok {
		return vuka.None[string]()
	}
	return vuka.Some(v.Name)
}
```
:::
