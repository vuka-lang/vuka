# Errors

<!-- go:context
type User struct {
	orm.Model[User]
	ID    int64
	Name  string
	Email string
}
-->
The ORM's errors are `*orm.Error` values with a `Kind`; `errors.Is` matches a
kind:


```go
func findUser(ctx context.Context, email string) (User, error) {
	u, err := orm.Objects[User]().Get(ctx, UserFields.Email.Eq(email))
	switch {
	case errors.Is(err, orm.ErrNotFound):
		return u, web.NotFound
	case errors.Is(err, orm.ErrConflict):
		return u, web.Conflict
	}
	return u, err
}
```

| Kind | When |
|---|---|
| `orm.NotFound` | `Get`, the engine's `First`, `Refresh`, `Remove`, `RawRow` found no row |
| `orm.MultipleFound` | `Get` matched several rows |
| `orm.Conflict` | a write broke a unique constraint (`Fields`: the column) |
| `orm.Invalid` | a write referred to a row that doesn't exist (a foreign key), or a value broke its [field's checks](/orm/fields#validation) (`Fields`: the columns' messages) |
| `orm.Unavailable` | the database didn't answer in time; a declared database's breaker is open, no connection was free within `Acquire`, or it is closing |

<!-- go:fragment -->
```go
err := orm.Objects[User]().Create(ctx, &User{Email: "ada@example.com"})
var e *orm.Error
if errors.As(err, &e) && e.Kind == orm.Conflict {
	log.Print(e.Fields) // map[email:[already exists]]
}
```

| | |
|---|---|
| `orm.ErrNotFound`, `ErrMultipleFound`, `ErrConflict`, `ErrInvalid`, `ErrUnavailable` | the kinds as `error` values, for `errors.Is` |
| `orm.KindOf(err)` | an error's kind; 0 for one the ORM gives no meaning (a driver's, a bad query) |



A web app maps the kinds onto statuses where it answers — `NotFound` → 404,
`Conflict` → 409, `Invalid` → 422, `Unavailable` → 503 — with
[web's errors](/web/parameters#errors).
