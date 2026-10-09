# The ? operator

**Go:** every fallible call is followed by the same three lines.

```go
id, err := strconv.Atoi(arg)
if err != nil {
	return User{}, err
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

The call stays where you wrote it, so nothing is reordered:

```go
id, __e1 := strconv.Atoi(arg); if __e1 != nil { return vuka.Err[User](__e1) }
u, __e2 := find(id).Get(); if __e2 != nil { return vuka.Err[User](__e2) }
```

Using `?` inside a larger expression, or in an `if`, `for` or `switch` header, is
an error that tells you to give it its own statement.
