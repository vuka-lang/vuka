# Testing

`github.com/vuka-lang/orm/ormtest` opens a fresh database per test, with the
models' tables made — in-memory SQLite by default:

```vuka
func TestPublish(t *testing.T) {
	ctx := ormtest.Open(t, orm.Objects[User](), orm.Objects[Tag](), orm.Objects[Post]())
	ada := User{Name: "ada", Email: "ada@example.com"}
	ormtest.Seed(t, ctx, User.Objects.Manager(), &ada)

	ctx, queries := ormtest.CountQueries(ctx)
	if err := publish(ctx, "generics"); !errors.Is(err, orm.ErrNotFound) {
		t.Fatalf("publish: %v", err)
	}
	if n := queries(); n != 1 {
		t.Errorf("%d queries", n)
	}
}
```

| | |
|---|---|
| `ormtest.Open(t, models…)` | a context on a fresh database with the models' tables; closed with the test |
| `ormtest.Seed(t, ctx, manager, rows…)` | inserts rows, failing the test on an error |
| `ormtest.Exec(t, ctx, sql, args…)` | runs a statement |
| `ormtest.CountQueries(ctx)` | a context counting its statements, and the count |
| `ormtest.Mirror(t, ctx, models…)` | a second database the writes are mirrored on: `(mirrored, onMirror)` contexts |
| `ormtest.Driver()` | the driver the tests run on, to skip what a database lacks |

`orm.Objects[T]()` is a model's manager, as `Open` takes it;
`Post.Objects.Manager()` is the same from a query.

## On a real server

`ORMTEST_DRIVER=postgres` or `mysql` with `ORMTEST_DSN` runs the same tests
on a server, each in a schema or database of its own:

```sh
ORMTEST_DRIVER=postgres ORMTEST_DSN=postgres://localhost:5432/test?sslmode=disable vuka test ./...
```

Skip what a database can't do with `ormtest.Driver()`:

```vuka
if ormtest.Driver() == "mysql" {
	t.Skip("no vector search on MySQL")
}
```
