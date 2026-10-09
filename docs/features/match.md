# match

**Go:** `switch` compares values or types; taking a value apart is done by hand.

**Vuka adds** `match`: cases are patterns, tried in order, that test and bind at
once.

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

## Patterns

| Pattern | Matches |
|---|---|
| `_`, `default` | anything |
| `x` | anything, bound to `x` (a constant's name compares instead) |
| `0`, `"a"`, `Max`, `pkg.Const`, `true` | an equal value |
| `^x` | the current value of the variable `x` (Elixir's pin) |
| `Ok(p)`, `Err(p)`, `Some(p)`, `None` | a Result or Option, then `p` inside |
| `T{F: p, …}`, `&T{…}` | a struct's fields; against an interface, a type test too |
| `1, 2, 3` | any of them |

`case p if cond:` adds a guard.

## Exhaustive

A `match` must cover every case — Ok and Err, Some and None, true and false, or a
case that takes anything — and a case after one that matches everything is an
error. Both are reported before Go sees the code:

```text
main.vuka:4:2: match on Result[int] isn't exhaustive: missing Err(_); add the missing cases or case _:
```

## What it becomes

An `if`/`else if` chain; bodies stay where you wrote them:

```go
if __m1 := lookup(arg); false { panic(__m1)
} else if u := __m1.Value(); __m1.IsOk() { _ = u;
	fmt.Println(u.Name)
} else if e := __m1.Err(); __m1.IsErr() && (errors.Is(e, strconv.ErrSyntax)) { _ = e;
	…
} else { panic("vuka: no case matched") }
```
