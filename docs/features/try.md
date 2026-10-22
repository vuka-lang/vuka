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

```vuka
func lookup(arg string) Result[User] {
	id := strconv.Atoi(arg)?          // Go's (T, error) works directly
	u := find(id)?                    // so does a Result
	return Ok(u)
}
```


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
