# ORM

[`github.com/vuka-lang/orm`](https://github.com/vuka-lang/orm) is a
Django-style ORM for Go and Vuka: a model is a plain struct, its manager a
package-level value, its queries lazy, immutable QuerySets. Relations and
prefetching, aggregates, `Case`/`When`, subqueries, text and vector search,
raw SQL, several databases with replicas and routing, mirrored writes, and Go
migration files written by `makemigrations`.

It was extracted from the [nexus](https://github.com/paulmanoni/nexus)
framework's ORM: the engine is the same, with no dependency on nexus.
Connections are plain `database/sql`.

**Go:** an ORM names fields as strings — `Where("author_name = ?", x)`,
`Order("-views")` — that the compiler never checks, and a query for one model
takes conditions written for another.

**Vuka adds** models declared with [field attributes](/features/fields) and
queries written with [field references](/features/fields#field-references):

```vuka
type Post struct {
	orm.Base @orm.Meta{Table: "posts", Ordering: []string{"-published"}}
	ID        int64             @orm.PK
	Title     string            @orm.Char{Max: 200}
	Slug      string            @orm.Slug{Unique: true}
	Author    orm.FK[User]      @orm.Rel{OnDelete: orm.Cascade, Related: "posts"}
	Tags      orm.M2M[Tag]
	Published Option[time.Time] @orm.Index
	Views     int               @orm.DefaultTo(0)
}

posts := Post.Objects.Filter(Post.Title.Contains("Go"), Post.Author.Name.Eq("ada")).
	OrderBy(Post.Views.Desc()).All(ctx)?                     // []Post

match Post.Objects.Get(ctx, Post.Slug.Eq(slug)) {
case Ok(p):             show(p)
case Err(orm.NotFound): notFound()
case Err(e):            return e
}
```

A misspelt field is a compile error with a suggestion, `Post.Objects.Filter(
User.Name.Eq("x"))` doesn't compile, and `Contains` on a number doesn't
either. Terminals return [`Result`](/features/result-option), so `?` and
`match` take them. Everything is plain Go underneath: Go and Vuka packages
share models and databases.

## Install

```sh
go get github.com/vuka-lang/orm
```

It needs Go 1.27 (it uses generic methods: `q.Values[R](…)`).

::: info
The Vuka layer needs field attributes and field references, which are on
Vuka's main branch after v0.6.0: until the next release, the module requires
`github.com/vuka-lang/vuka v0.6.0` with a `replace` of a checkout.
:::

## Drivers

Drivers are opt-in blank imports, `database/sql` style; the `orm` package
links none:

| Import | Driver | Dialect |
|---|---|---|
| `github.com/vuka-lang/orm/postgres` | pgx | `postgres` |
| `github.com/vuka-lang/orm/mysql` | go-sql-driver/mysql | `mysql` |
| `github.com/vuka-lang/orm/sqlite` | pure-Go `glebarez/go-sqlite` | `sqlite` |

## A first program

```vuka
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/vuka-lang/orm"
	_ "github.com/vuka-lang/orm/sqlite"
)

type User struct {
	orm.Base
	ID    int64
	Name  string @orm.Char{Max: 100, Unique: true}
	Email string @orm.Char{Max: 200}
}

func run(ctx context.Context) error {
	db := orm.ConnectURL("sqlite:app.db")?
	orm.AddDatabase("main", db, orm.AsDefault())
	orm.CreateTables(ctx, orm.Objects[User]())?    // a real program runs migrations

	ada := User{Name: "ada", Email: "ada@example.com"}
	ada.Save(ctx)?

	users := User.Objects.Filter(User.Name.StartsWith("a")).OrderBy(User.Name.Asc()).All(ctx)?
	fmt.Println(len(users), "users")
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

`orm.ConnectURL` picks the dialect from the URL — `postgres://…`,
`mysql://user:pass@tcp(host)/db`, `sqlite:file.db`; `orm.Connect(dialect,
dsn)` and `orm.Open(sqlDB, dialect)` take it apart. A program with several
databases, replicas and a supervised pool declares them instead: see
[Databases](/orm/databases).

## What's in it

| | |
|---|---|
| [Models](/orm/models) | `orm.Base`, `@orm.Meta`, field attributes, relations, `Option` fields |
| [Queries](/orm/queries) | predicates from field references, `Get`/`First`, writes, relations, prefetching |
| [Expressions and functions](/orm/expressions) | `orm/fn`, `Case`/`When`, subqueries, custom functions and lookups |
| [Search](/orm/search) | text, trigram, vector and hybrid search on each database |
| [Transactions](/orm/transactions) | `@orm.Transaction`, retries, `OnCommit` |
| [Migrations](/orm/migrations) | the `orm` command and Go migration files |
| [Databases](/orm/databases) | dialects, several databases, routing, raw SQL, the connection pool |
| [Observability](/orm/observability) | observers, N+1 warnings, slow queries, pool statistics |
| [Errors](/orm/errors) | the error kinds |
| [Testing](/orm/testing) | `ormtest` |
| [Coming from nexus](/orm/nexus) | what moved where |

The repository's
[`examples/blog`](https://github.com/vuka-lang/orm/tree/main/examples/blog)
is the Vuka layer at work — see [Examples](/examples#blog).
