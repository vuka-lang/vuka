# Errors

The ORM's errors are `*orm.Error` values with a `Kind`; `errors.Is` matches a
kind, and `match` takes the kinds `Get` fails with:

```vuka
match User.Objects.Get(ctx, User.Email.Eq(email)) {
case Ok(u):
	return Ok(u)
case Err(orm.NotFound):
	return Err(web.NotFound)
case Err(e) if errors.Is(e, orm.ErrConflict):
	return Err(web.Conflict)
case Err(e):
	return Err(e)
}
```

| Kind | When |
|---|---|
| `orm.NotFound` | `Get`, the engine's `First`, `Refresh`, `Remove`, `RawRow` found no row |
| `orm.MultipleFound` | `Get` matched several rows |
| `orm.Conflict` | a write broke a unique constraint (`Fields`: the column) |
| `orm.Invalid` | a write referred to a row that doesn't exist (a foreign key) |
| `orm.Unavailable` | the database didn't answer in time; a declared database's breaker is open, no connection was free within `Acquire`, or it is closing |

```go
var e *orm.Error
if errors.As(err, &e) && e.Kind == orm.Conflict {
	log.Print(e.Fields) // map[email:[already exists]]
}
```

| | |
|---|---|
| `orm.ErrNotFound`, `ErrMultipleFound`, `ErrConflict`, `ErrInvalid`, `ErrUnavailable` | the kinds as `error` values, for `errors.Is` |
| `orm.KindOf(err)` | an error's kind; 0 for one the ORM gives no meaning (a driver's, a bad query) |

In Vuka, `Get` fails with the kind itself for none or several rows, so
`case Err(orm.NotFound)` matches it; the other errors are `*orm.Error`
values, matched with a guard as above.

A web app maps the kinds onto statuses where it answers — `NotFound` → 404,
`Conflict` → 409, `Invalid` → 422, `Unavailable` → 503 — with
[web's errors](/web/parameters#errors).
