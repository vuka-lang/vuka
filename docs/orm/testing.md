# Database Testing

[[toc]]

## Introduction

Tests that touch the database should each start from a clean one.
`github.com/vuka-lang/orm/v2/ormtest` opens a fresh database per test, with
the models' tables made — in-memory SQLite by default.

<!-- go:context
type User struct {
	orm.Model[User]
	ID    int64
	Name  string
	Email string
}

type Tag struct {
	orm.Model[Tag]
	ID   int64
	Name string
}

type Post struct {
	orm.Model[Post]
	ID   int64
	Slug string
}

func publish(ctx context.Context, slug string) error { return nil }

var t *testing.T
-->

## Writing a Test {#writing}

This test opens a database, seeds a user and counts the queries `publish`
makes:

```go
func TestPublish(t *testing.T) {
	ctx := ormtest.Open(t, orm.Objects[User](), orm.Objects[Tag](), orm.Objects[Post]())
	ada := User{Name: "ada", Email: "ada@example.com"}
	ormtest.Seed(t, ctx, orm.Objects[User](), &ada)

	ctx, queries := ormtest.CountQueries(ctx)
	if err := publish(ctx, "generics"); !errors.Is(err, orm.ErrNotFound) {
		t.Fatalf("publish: %v", err)
	}
	if n := queries(); n != 1 {
		t.Errorf("%d queries", n)
	}
}
```

The helpers are:

| | |
|---|---|
| `ormtest.Open(t, models…)` | a context on a fresh database with the models' tables; closed with the test |
| `ormtest.Seed(t, ctx, manager, rows…)` | inserts rows, failing the test on an error |
| `ormtest.Exec(t, ctx, sql, args…)` | runs a statement |
| `ormtest.CountQueries(ctx)` | a context counting its statements, and the count |
| `ormtest.Mirror(t, ctx, models…)` | a second database the writes are mirrored on: `(mirrored, onMirror)` contexts |
| `ormtest.Driver()` | the driver the tests run on, to skip what a database lacks |

`orm.Objects[T]()` is a model's manager, as `Open` takes it.

## Testing on a Real Server {#on-a-real-server}

`ORMTEST_DRIVER=postgres` or `mysql` with `ORMTEST_DSN` runs the same tests
on a server, each in a schema or database of its own:

```sh
ORMTEST_DRIVER=postgres ORMTEST_DSN=postgres://localhost:5432/test?sslmode=disable go test ./...   # vuka test ./... for .vuka packages
```

### Skipping by Database

Skip what a database can't do with `ormtest.Driver()`:

<!-- go:fragment -->
```go
if ormtest.Driver() == "mysql" {
	t.Skip("no vector search on MySQL")
}
```
