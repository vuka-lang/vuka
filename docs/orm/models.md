# Models


A model is a struct embedding `orm.Model[T]`. Its fields are its columns, its
`Schema` method says what the columns are, and `orm.Objects[Post]()` is its
manager.


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
```

Attributes are Go values, not struct tags: `orm.Char{Max: 200}` is a
composite literal, type-checked, so a wrong setting is a compile error on
the attribute. `Schema` names each field by its
address (`&p.Title`), so a renamed field is a compile error too; a field
with nothing to say isn't listed. The ORM calls `Schema` once, on a zero
value.


## orm.Model

`orm.Model[Post]`, embedded by value, gives the rows their methods:

| | |
|---|---|
| `p.Save(ctx)` | inserts a new row (its key filled in), updates a saved one |
| `p.SaveFields(ctx, PostFields.Title, …)` | updates only those columns (or names: `"title"`) |
| `p.Refresh(ctx)` | reads the row again |
| `p.Delete(ctx)` | deletes it |
| `p.Load(ctx, PostFields.Tags, …)` | reads relations into the row |
| `p.Add(ctx, "tags", rows…)`, `Remove`, `Set` | changes a many-to-many's links |

`orm.Objects[Post]()` is the model's [manager](/orm/queries). A named query
is a package-level function:


```go
// PostLive is the published posts.
func PostLive() orm.QuerySet[Post] {
	return orm.Objects[Post]().Filter(PostFields.Published.IsNull(false))
}
```

<!-- go:fragment -->
```go
n, err := PostLive().Count(ctx)
```

## Meta

`s.Model(orm.Meta{…})` in `Schema` describes the model:

| `orm.Meta` field | |
|---|---|
| `Table` | the table; the plural of the type's name in snake_case otherwise (`Post` → `posts`) |
| `Ordering` | the order of a query that sets none, as `OrderBy` strings: `[]string{"-published", "id"}` |
| `Indexes` | its indexes, as an `Indexes()` method returns them |
| `DB` | the [database](/orm/databases#multiple-databases) it lives on; the default otherwise |
| `Read`, `Write` | where its reads and its writes go, over `DB` |
| `Mirror` | a database its writes are repeated on |
| `Unmanaged` | the database's own table: never created or migrated |

A `Meta() orm.Meta` method works too, and wins. `orm.Register[Entry]()` at
init registers a model at start-up, so `orm.Check` and the migration commands
know it without `orm generate`:


```go
type Entry struct {
	orm.Model[Entry]
	ID   int64
	Note string
}

func (e *Entry) Schema(s *orm.Schema) { s.Model(orm.Meta{Table: "audit_entries"}) }

func init() { orm.Register[Entry]() }
```

## Field attributes

Attributes say what tags say, and win over them. In Go each
is the value given to `s.Field` after the field's address: `@orm.Char{Max: n}`
is `orm.Char{Max: n}`, a bare `@orm.Unique` is `orm.Unique{}`.

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


<!-- go:fragment -->
```go
p := Post{Title: "Go generics", Author: orm.FKOf(&ada)} // from a row
q := Post{Title: "SQL", Author: orm.FKID[User](7)}       // from a key

author, err := p.Author.Get(ctx).Get() // read once, then held
if err != nil {
	return err
}
name := p.Author.Related().Name // nil until loaded
```

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


<!-- go:fragment -->
```go
if err := p.Tags.Add(ctx, &golang, &tools); err != nil {
	return err
}
tags, err := p.Tags.All(ctx).Get() // the rows held, else read
if err != nil {
	return err
}
if err := p.Tags.Remove(ctx, &tools); err != nil {
	return err
}
if err := p.Tags.Set(ctx, &golang); err != nil { // its only links
	return err
}
return p.Tags.Clear(ctx)
```

`All`, `Add`, `Remove`,
`Set`, `Clear` (on a saved row; rows or keys), `Rows` and `Loaded` (what
`PrefetchRelated` loaded).

A model's typed fields go on through them into the related model: `PostFields.Author.Name`, `PostFields.Tags.Name`.

`orm.Rel` describes a relation:

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

<!-- go:context
var ada, golang, tools = User{}, Tag{}, Tag{}
var p Post
-->

An `Option[T]` field is a nullable column: `None` is NULL.


<!-- go:fragment -->
```go
p.Published = vuka.Some(time.Now())
drafts, err := orm.Objects[Post]().Filter(PostFields.Published.IsNull(true)).All(ctx)
```

Its typed field is an `orm.Field[V]` with `IsNull` for its NULLs.

A Go model writes a nullable field as a pointer, `Published *time.Time`
(nil is NULL), the usual Go, or as a `vuka.Option[time.Time]`, what a Vuka
model's `Option[T]` is and what `vuka eject` keeps: the ORM, JSON and an
island's props read both. `orm generate` writes a row scanner for either.

## Plain Go

A Go model embeds `orm.Model[T]` (the type `orm.Base` stands for in a Vuka
model) and says in its `Schema` method what a Vuka model says in
attributes: `s.Model(…)` takes the model's (`orm.Meta`, any
`orm.ModelAttr`), `s.Field(&p.X, …)` a field's — the same values,
`@orm.Char{Max: 200}` on `Title` being `s.Field(&p.Title, orm.Char{Max:
200})`. The statics are plain names:

| Vuka | Go |
|---|---|
| `Post.Objects` | `orm.Objects[Post]()`; `orm.For[Post](opts…)` for a manager with options |
| `Post.Title` (a field reference) | `PostFields.Title`, written by `ormgen` |
| `func Post.Live() orm.QuerySet[Post]` | `func PostLive() orm.QuerySet[Post]` |
| `@orm.Entity` | `orm.Register[T]()` at init; `orm.Check(models…)` takes others |

`PostFields` comes from `ormgen` — `//go:generate go run
github.com/vuka-lang/orm/v2/cmd/ormgen` beside the models — as an
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
