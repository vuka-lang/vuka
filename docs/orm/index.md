# Database: Getting Started

[[toc]]

## Introduction

[`github.com/vuka-lang/orm/v2`](https://github.com/vuka-lang/orm) is the ORM
for Go and Vuka. A model is a plain struct, its manager a package-level value,
and its queries lazy, immutable QuerySets. Connections are plain
`database/sql`.

Each field's column — name, type, default, checks — is declared in one
attribute beside it, and custom value types and attributes plug in the same
way. On top of that you get relations and prefetching, aggregates,
`Case`/`When`, subqueries, text and vector search, raw SQL, several databases
with replicas and routing, mirrored writes, and Go migration files written by
`makemigrations`.

## Checked Fields and Queries

In plain Go, an ORM names fields as strings — `Where("author_name = ?", x)`,
`Order("-views")` — that the compiler never checks, and a query for one model
takes conditions written for another.

Vuka declares models with [field attributes](/features/fields) and writes
queries with [field references](/features/fields#field-references). In Go, the
same attributes go in a `Schema` method and the references are generated. Here
is a model declared that way:

```go
type Post struct {
	orm.Model[Post]
	ID        int64
	Title     string
	Slug      string
	Status    string
	Author    orm.FK[User]
	Tags      orm.M2M[Tag]
	Published vuka.Option[time.Time]
	Views     int
	Created   time.Time
}

func (p *Post) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Table: "posts", Ordering: []string{"-published"}})
	s.Field(&p.ID, orm.PK{Column: "post_id"})
	s.Field(&p.Title, orm.Char{Max: 200, Trim: true})
	s.Field(&p.Slug, orm.Slug{Unique: true})
	s.Field(&p.Status, orm.Char{Max: 20, Default: "draft", Choices: []string{"draft", "published"}})
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "posts"})
	s.Field(&p.Published, orm.Datetime{Index: true})
	s.Field(&p.Views, orm.Int{Default: 0, Min: 0})
	s.Field(&p.Created, orm.Datetime{DefaultNow: true})
}

```

<!-- go:context
type Tag struct {
	orm.Model[Tag]
	ID   int64
	Name string
}

var slug = "hello-go"
-->

You query it through the generated fields:

<!-- go:fragment -->
```go
f := PostFields // typed fields, written by go generate
posts, err := orm.Objects[Post]().Filter(f.Title.Contains("Go"), f.Author.Name.Eq("ada")).
	OrderBy(f.Views.Desc()).All(ctx) // []Post
if err != nil {
	return err
}

p, err := orm.Objects[Post]().Get(ctx, f.Slug.Eq(slug))
if errors.Is(err, orm.ErrNotFound) { // there is none
	return web.NotFound
}
```

A model's `Schema` names each field by its address, so a rename is checked
by the compiler. `PostFields`, written by
`//go:generate go run github.com/vuka-lang/orm/v2/cmd/ormgen`, gives typed
lookups. `orm.Objects[Post]()` is the model's manager; without the generated
fields, every query takes `orm.Q{"title__contains": "Go"}` strings.

Terminals return `(T, error)`. Everything is plain Go underneath — the
attributes are the model's `Schema` method — so Go and Vuka packages share
models and databases.

> [!NOTE]
> A value outside a field's `Choices`, bounds or length is refused on every
> write with an [`orm.Invalid`](/orm/errors) naming the field, before the
> database sees it.

## Installation

Add the module to your project:

```sh
go get github.com/vuka-lang/orm/v2
```

The ORM needs Go 1.27, because it uses generic methods: `q.Values[R](…)`.

## Choosing a Driver

Drivers are opt-in blank imports, `database/sql` style; the `orm` package
links none. Import the one for your database:

| Import | Driver | Dialect |
|---|---|---|
| `github.com/vuka-lang/orm/v2/postgres` | pgx | `postgres` |
| `github.com/vuka-lang/orm/v2/mysql` | go-sql-driver/mysql | `mysql` |
| `github.com/vuka-lang/orm/v2/sqlite` | pure-Go `glebarez/go-sqlite` | `sqlite` |

## Your First Program {#a-first-program}

This complete program connects to SQLite, creates a table, saves a user and
queries it back:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/vuka-lang/orm/v2"
	_ "github.com/vuka-lang/orm/v2/sqlite"
)

//go:generate go run github.com/vuka-lang/orm/v2/cmd/ormgen

type User struct {
	orm.Model[User]
	ID    int64
	Name  string
	Email string
}

func (u *User) Schema(s *orm.Schema) {
	s.Field(&u.Name, orm.Char{Max: 100, Unique: true})
	s.Field(&u.Email, orm.Char{Max: 200})
}

func run(ctx context.Context) error {
	db, err := orm.ConnectURL("sqlite:app.db")
	if err != nil {
		return err
	}
	orm.AddDatabase("main", db, orm.AsDefault())
	if err := orm.CreateTables(ctx, orm.Objects[User]()); err != nil { // a real program runs migrations
		return err
	}

	ada := User{Name: "ada", Email: "ada@example.com"}
	if err := ada.Save(ctx); err != nil {
		return err
	}

	users, err := orm.Objects[User]().Filter(UserFields.Name.StartsWith("a")).OrderBy(UserFields.Name.Asc()).All(ctx)
	if err != nil {
		return err
	}
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
`mysql://user:pass@tcp(host)/db`, `sqlite:file.db`. `orm.Connect(dialect,
dsn)` and `orm.Open(sqlDB, dialect)` take it apart.

A program with several databases, replicas and a supervised pool declares
them instead: see [Databases](/orm/databases).

## Where to Go Next {#what-s-in-it}

The rest of the database guide covers each part in turn:

| | |
|---|---|
| [Models](/orm/models) | `orm.Model[T]`, `Schema`, relations, `Option` fields |
| [Fields](/orm/fields) | column attributes (`PK`, `Char`, `Int`, `Datetime`, …), defaults, write checks, custom value types and attributes |
| [Queries](/orm/queries) | predicates from field references, `Get`/`First`, writes, relations, prefetching |
| [Expressions & Functions](/orm/expressions) | `orm/fn`, `Case`/`When`, subqueries, custom functions and lookups |
| [Search](/orm/search) | text, trigram, vector and hybrid search on each database |
| [Transactions](/orm/transactions) | `orm.Transaction`, retries, `OnCommit` |
| [Migrations](/orm/migrations) | the `orm` command and Go migration files |
| [Databases](/orm/databases) | dialects, several databases, routing, raw SQL, the connection pool |
| [Observability](/orm/observability) | observers, N+1 warnings, slow queries, pool statistics |
| [Errors](/orm/errors) | the error kinds |
| [Database Testing](/orm/testing) | `ormtest` |

The repository's
[`examples/blog`](https://github.com/vuka-lang/orm/tree/main/examples/blog)
is the Vuka layer at work.
