# Models

::: vuka
A model is a struct embedding `orm.Base`. Its fields are its columns, typed
[field attributes](/features/fields#field-attributes) say what the columns
are, and `Post.Objects` is its query.
:::

::: go
A model is a struct embedding `orm.Model[T]`. Its fields are its columns, its
`Schema` method says what the columns are, and `orm.For[Post]()` is its
manager, declared once at package level.
:::

::: vuka
```vuka
type User struct {
	orm.Base
	ID    int64
	Name  string @orm.Char{Max: 100, Unique: true}
	Email string @orm.Char{Max: 200}
}

type Tag struct {
	orm.Base
	ID   int64
	Name string @orm.Slug{Unique: true}
}

type Post struct {
	orm.Base @orm.Meta{Table: "posts", Ordering: []string{"-published"}}
	ID        int64             @orm.PK
	Title     string            @orm.Char{Max: 200} @orm.Trigram
	Slug      string            @orm.Slug{Unique: true}
	Body      string            @orm.LongText
	Author    orm.FK[User]      @orm.Rel{OnDelete: orm.Cascade, Related: "posts"}
	Tags      orm.M2M[Tag]
	Published Option[time.Time] @orm.Index
	Views     int               @orm.DefaultTo(0)
}
```
:::

::: go
```go
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

type Tag struct {
	orm.Model[Tag]
	ID   int64
	Name string
}

func (t *Tag) Schema(s *orm.Schema) {
	s.Field(&t.Name, orm.Slug{Unique: true})
}

type Post struct {
	orm.Model[Post]
	ID        int64
	Title     string
	Slug      string
	Body      string
	Author    orm.FK[User]
	Tags      orm.M2M[Tag]
	Published vuka.Option[time.Time]
	Views     int
}

func (p *Post) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Table: "posts", Ordering: []string{"-published"}})
	s.Field(&p.ID, orm.PK{})
	s.Field(&p.Title, orm.Char{Max: 200}, orm.Trigram{})
	s.Field(&p.Slug, orm.Slug{Unique: true})
	s.Field(&p.Body, orm.LongText{})
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "posts"})
	s.Field(&p.Published, orm.Index{})
	s.Field(&p.Views, orm.DefaultTo(0))
}

var (
	Users = orm.For[User]()
	Tags  = orm.For[Tag]()
	Posts = orm.For[Post]()
)
```
:::

Attributes are Go values, not struct tags: `orm.Char{Max: 200}` is a
composite literal, type-checked, so a wrong setting is a compile error on
the attribute.<span class="go-only"> `Schema` names each field by its
address (`&p.Title`), so a renamed field is a compile error too; a field
with nothing to say isn't listed. The ORM calls `Schema` once, on a zero
value.</span><span class="vuka-only"> Vuka writes them into the model's
`Schema` method, the Go form: a field is named by its address, a bare
attribute is its zero value (`@orm.PK` is `orm.PK{}`), and a type that
declares `Schema` itself can't have attributes too (E0129).</span>

## orm.Base

`orm.Base` is a Vuka model's base; Vuka fills in the type, so `orm.Base` in
`Post` is `orm.Base[Post]`, and its static `Objects` is the model's
manager (`orm.Objects[Post]()` in Go). It gives the rows their methods:

| | |
|---|---|
| `p.Save(ctx)` | inserts a new row (its key filled in), updates a saved one |
| `p.SaveFields(ctx, Post.Title, …)` | updates only those columns (in Go `PostFields.Title`, or names: `"title"`) |
| `p.Refresh(ctx)` | reads the row again |
| `p.Delete(ctx)` | deletes it |
| `p.Load(ctx, Post.Tags, …)` | reads relations into the row |
| `p.Add(ctx, Post.Tags, rows…)`, `Remove`, `Set` | changes a many-to-many's links |

and a static, `Post.Objects`: the model's [manager](/orm/queries),
`orm.Objects[Post]()` in Go. A [static method](/features/statics) names a
query:

::: vuka
```vuka
// Live is the published posts.
func Post.Live() orm.QuerySet[Post] { return Post.Objects.Filter(Post.Published.IsNull(false)) }

n := Post.Live().Count(ctx)?
```
:::

::: go
```go
// PostLive is the published posts.
func PostLive() orm.QuerySet[Post] { return Posts.Filter(PostFields.Published.IsNull(false)) }

n, err := PostLive().Count(ctx)
```
:::

## Meta

`@orm.Meta{…}` on the embedded base describes the model:

| `orm.Meta` field | |
|---|---|
| `Table` | the table; the plural of the type's name in snake_case otherwise (`Post` → `posts`) |
| `Ordering` | the order of a query that sets none, as `OrderBy` strings: `[]string{"-published", "id"}` |
| `Indexes` | its indexes, as an `Indexes()` method returns them |
| `DB` | the [database](/orm/databases#multiple-databases) it lives on; the default otherwise |
| `Read`, `Write` | where its reads and its writes go, over `DB` |
| `Mirror` | a database its writes are repeated on |
| `Unmanaged` | the database's own table: never created or migrated |

A `Meta() orm.Meta` method works too, and wins. `@orm.Meta{…} @orm.Entity` on
the type itself is the other spelling; the `orm.Entity` type decorator also
registers the model at start-up, so `orm.Check` and the migration commands
know it without `orm generate`:

::: vuka
```vuka
@orm.Meta{Table: "audit_entries"}
@orm.Entity
type Entry struct {
	orm.Base
	ID   int64
	Note string
}
```
:::

::: go
```go
type Entry struct {
	orm.Model[Entry]
	ID   int64
	Note string
}

func (e *Entry) Schema(s *orm.Schema) { s.Model(orm.Meta{Table: "audit_entries"}) }

var Entries = orm.For[Entry]() // registers the model
```
:::

## Field attributes

Attributes say what tags say, and win over them:

| Attribute | Column |
|---|---|
| `@orm.PK{Manual, UUID, …}` | the primary key (`ID` is one by default) |
| `@orm.Char{Max: n, Unique, Index, Default, Choices, …}` | `VARCHAR(n)` |
| `@orm.Int{Min, Max, Size, Unsigned, …}` | an integer column, bounded |
| `@orm.DateField{…}`, `@orm.Datetime{Precision, UTC, DefaultNow, …}` | `DATE`, a timestamp |
| `@orm.Slug{Max, Unique}` | an indexed `VARCHAR`, 50 by default |
| `@orm.LongText` | `TEXT` (a string's default) |
| `@orm.Unique`, `@orm.Index` | a unique constraint, an index |
| `@orm.UniqueTogether("name")` | a named unique index: the fields sharing the name are unique together |
| `@orm.Column("name")` | the column's name; the field's in snake_case otherwise |
| `@orm.DBType("jsonb")` | the database type, written as is |
| `@orm.Null` | the column takes NULL, for a type with no nil (an `orm.FK` that may be unset) |
| `@orm.Skip` | no column |
| `@orm.DefaultTo(v)` | the column default, a Go value written as SQL: `@orm.DefaultTo(0)`, `@orm.DefaultTo("draft")` |
| `@orm.DefaultSQL("CURRENT_TIMESTAMP")` | the column default, in SQL |
| `@orm.AutoNow`, `@orm.AutoNowAdd` | set to the time on every save, on create |
| `@orm.Rel{OnDelete, Related, Column, Through, Null, CrossDB}` | a relation's: below |
| `@orm.Dims(n)`, `@orm.HNSW{Metric, M, EfConstruction}`, `@orm.IVFFlat{Metric, Lists}` | a [vector](/orm/search#vectors) and its pgvector index |
| `@orm.Trigram`, `@orm.FullText{Config}` | [text search](/orm/search) indexes |
| `@orm.SearchOf{Fields: []string{"title:A", "body:B"}, Config}` | an `orm.TSVector` generated from fields |

[Fields](/orm/fields) has every setting of the column attributes — defaults,
the checks a write makes, comments and collations — value types of your own,
and attributes of your own. Any number may follow a field. Column types follow the Go type: integers,
floats, `bool`, `string`, `time.Time`, `[]byte`; `orm.JSON[T]` (a JSON
column), `orm.CSV[E]` (a comma-separated list), `orm.Vector`,
`orm.TSVector`.

## Relations

**`orm.FK[T]`** is a foreign key as one field: the column `<field>_id`, and
the related row once it is loaded (`SelectRelated`, `PrefetchRelated`,
`Get`).

::: vuka
```vuka
p := Post{Title: "Go generics", Author: orm.FKOf(&ada)}   // from a row
q := Post{Title: "SQL", Author: orm.FKID[User](7)}         // from a key

author := p.Author.Get(ctx)?        // read once, then held
name := p.Author.Related().Name     // nil until loaded
```
:::

::: go
```go
p := Post{Title: "Go generics", Author: orm.FKOf(&ada)} // from a row
q := Post{Title: "SQL", Author: orm.FKID[User](7)}       // from a key

author, err := p.Author.Get(ctx).Get() // read once, then held
name := p.Author.Related().Name        // nil until loaded
```
:::

| | |
|---|---|
| `orm.FKOf(&row)`, `orm.FKID[T](key)` | an FK to a row, to a key |
| `f.Get(ctx)` | the related row: the one held, else read and held |
| `f.Related()`, `f.Loaded()` | the row held (nil when not loaded); whether one is |
| `f.ID()`, `f.Key()`, `f.IsSet()` | the key as `int64`, as it is; whether there is one |
| `f.Set(&row)`, `f.SetID(key)` | point it elsewhere |

Its JSON is the key.

**`orm.M2M[T]`** is a many-to-many, through a table between
(`<table>_<field>`, `posts_tags`, or `Rel`'s `Through`):

::: vuka
```vuka
p.Tags.Add(ctx, &golang, &tools)?
tags := p.Tags.All(ctx)?             // the rows held, else read
p.Tags.Remove(ctx, &tools)?
p.Tags.Set(ctx, &golang)?            // its only links
p.Tags.Clear(ctx)?
```
:::

::: go
```go
err := p.Tags.Add(ctx, &golang, &tools)
tags, err := p.Tags.All(ctx).Get() // the rows held, else read
err = p.Tags.Remove(ctx, &tools)
err = p.Tags.Set(ctx, &golang) // its only links
err = p.Tags.Clear(ctx)
```
:::

`All`, `Add`, `Remove`,
`Set`, `Clear` (on a saved row; rows or keys), `Rows` and `Loaded` (what
`PrefetchRelated` loaded).

A model's typed fields go on through them into the related model: `Post.Author.Name`,
`Post.Tags.Name`.

`@orm.Rel` describes a relation:

| `orm.Rel` field | |
|---|---|
| `OnDelete` | what deleting the related row does: `orm.Cascade`, `orm.SetNull`, `orm.Restrict` |
| `Related` | the inverse's name on the related model (`"posts"`: `User`'s posts, for `orm.Q{"posts__views__gt": 60}`) |
| `Column` | the foreign key's column |
| `Through` | a many-to-many's table between |
| `Null` | the key may be NULL |
| `CrossDB` | the related model is on [another database](/orm/databases#cross-database-relations): no constraint, prefetched rather than joined |

Reverse relations have no field type yet: filter through them with the
engine's lookups, `orm.Q{"posts__views__gt": 60}`.

## Option fields

An `Option[T]` field is a nullable column: `None` is NULL.

::: vuka
```vuka
p.Published = Some(time.Now())
drafts := Post.Objects.Filter(Post.Published.IsNil()).All(ctx)?
```
:::

::: go
```go
p.Published = vuka.Some(time.Now())
drafts, err := Posts.Filter(PostFields.Published.IsNull(true)).All(ctx)
```
:::

Its reference is a `vuka.NullableRef`: `IsNil`, `NotNil`, and `Eq`, `Ne`,
`In`.

## Plain Go

A Go model embeds `orm.Model[T]` (the type `orm.Base` stands for in a Vuka
model) and says in its `Schema` method what a Vuka model says in
attributes: `s.Model(…)` takes the model's (`orm.Meta`, any
`orm.ModelAttr`), `s.Field(&p.X, …)` a field's — the same values,
`@orm.Char{Max: 200}` on `Title` being `s.Field(&p.Title, orm.Char{Max:
200})`. The statics are plain names:

| Vuka | Go |
|---|---|
| `Post.Objects` | `orm.For[Post]()`, kept in a package-level variable |
| `Post.Title` (a field reference) | `PostFields.Title`, written by `ormgen` |
| `func Post.Live() orm.Query[Post]` | `func PostLive() orm.QuerySet[Post]` |
| `@orm.Entity` | `orm.For[T]()` registers the model; `orm.Check(models…)` takes others |

`PostFields` comes from `ormgen` — `//go:generate go run
github.com/vuka-lang/orm/cmd/ormgen` beside the models — as an
`orm.Field[V]` (an `orm.TextField[V]` for strings) per column and a field
per relation (`PostFields.Author.Name`, `PostFields.Tags`). It is a
convenience: without it every query takes the engine's strings,
`orm.Q{"name__startswith": "a"}` and `OrderBy("-views")`, and an unknown
lookup is a runtime error.

The engine is the same, so Go and Vuka models live side by side, share
databases and relate to each other. `orm.Check()` reports models the ORM
can't map — a `Schema` given something other than the address of one of the
receiver's fields among them; [`vukavet`](/tools/vukavet) finds those in
the source. `orm:` struct tags and `orm.SetFieldOptions(fn)` still work,
deprecated for `Schema`.
