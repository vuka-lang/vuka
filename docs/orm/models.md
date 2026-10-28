# Models

[[toc]]

## Introduction

A model is the Go type that stands for a table. You write it as a struct
embedding `orm.Model[T]`: its fields are its columns, its `Schema` method says
what the columns are, and `orm.Objects[Post]()` is its manager.

## Defining Models

Here are three models — users, tags and posts — with their `Schema` methods:


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
composite literal, type-checked, so a wrong setting is a compile error on the
attribute.

`Schema` names each field by its address (`&p.Title`), so a renamed field is a
compile error too. A field with nothing to say isn't listed. The ORM calls
`Schema` once, on a zero value.

## Row Methods {#orm-model}

`orm.Model[Post]`, embedded by value, gives the rows their methods:

| | |
|---|---|
| `p.Save(ctx)` | inserts a new row (its key filled in), updates a saved one |
| `p.SaveFields(ctx, PostColumns.Title, …)` | updates only those columns (or names: `"title"`) |
| `p.Refresh(ctx)` | reads the row again |
| `p.Delete(ctx)` | deletes it |
| `p.Load(ctx, PostColumns.Tags(), …)` | reads relations into the row |
| `p.Add(ctx, "tags", rows…)`, `Remove`, `Set` | changes a many-to-many's links |

### Named Queries

`orm.Objects[Post]()` is the model's [manager](/orm/queries). A named query is
a package-level function returning a QuerySet:


```go
// PostLive is the published posts.
func PostLive() orm.QuerySet[Post] {
	return orm.Objects[Post]().Filter(PostColumns.Published.IsNull(false))
}
```

You call it like any other QuerySet:

<!-- go:fragment -->
```go
n, err := PostLive().Count(ctx)
```

## Model Options {#meta}

`s.Model(orm.Meta{…})` in `Schema` describes the model as a whole:

| `orm.Meta` field | |
|---|---|
| `Table` | the table; the plural of the type's name in snake_case otherwise (`Post` → `posts`) |
| `Ordering` | the order of a query that sets none, as `OrderBy` strings: `[]string{"-published", "id"}` |
| `Indexes` | its indexes, as an `Indexes()` method returns them |
| `DB` | the [database](/orm/databases#multiple-databases) it lives on; the default otherwise |
| `Read`, `Write` | where its reads and its writes go, over `DB` |
| `Mirror` | a database its writes are repeated on |
| `Unmanaged` | the database's own table: never created or migrated |

A `Meta() orm.Meta` method works too, and wins.

### Registering Models

`orm.Register[Entry]()` at init registers a model at start-up, so `orm.Check`
and the migration commands know it without `orm generate`:


```go
type Entry struct {
	orm.Model[Entry]
	ID   int64
	Note string
}

func (e *Entry) Schema(s *orm.Schema) { s.Model(orm.Meta{Table: "audit_entries"}) }

func init() { orm.Register[Entry]() }
```

## Field Attributes {#field-attributes}

Attributes say what tags say, and win over them. In Go each is the value given
to `s.Field` after the field's address: `@orm.Char{Max: n}` is
`orm.Char{Max: n}`, and a bare `@orm.Unique` is `orm.Unique{}`. The table
lists them in their Vuka spelling:

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

Any number of attributes may follow a field. [Fields](/orm/fields) has every
setting of the column attributes — defaults, the checks a write makes, comments
and collations — plus value types and attributes of your own.

### Column Types

Column types follow the Go type: integers, floats, `bool`, `string`,
`time.Time`, `[]byte`; `orm.JSON[T]` (a JSON column), `orm.CSV[E]` (a
comma-separated list), `orm.Vector`, `orm.TSVector`.

## Relations

Relations are fields too. A post has an author and a set of tags; the field
types below hold both the key and, once loaded, the related rows.

### Foreign Keys

`orm.FK[T]` is a foreign key as one field: the column `<field>_id`, and the
related row once it is loaded (`SelectRelated`, `PrefetchRelated`, `Get`). This
fragment builds two posts and reads an author:

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

### Many-to-Many Relations

`orm.M2M[T]` is a many-to-many, through a table between (`<table>_<field>`,
`posts_tags`, or `Rel`'s `Through`). This fragment adds, reads, removes and
replaces a post's tags:

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

Its methods are `All`, `Add`, `Remove`, `Set`, `Clear` (on a saved row; rows
or keys), `Rows` and `Loaded` (what `PrefetchRelated` loaded).

A model's typed fields go on through relations into the related model:
`PostColumns.Author().Name`, `PostColumns.Tags().Name`.

### Relation Options

`orm.Rel` describes a relation:

| `orm.Rel` field | How it works | Example |
|---|---|---|
| `OnDelete` | When the target row is deleted, `orm.Cascade` deletes referring rows, `orm.SetNull` clears their keys, and `orm.Restrict` prevents deletion while references remain. Use `Null: true` with `SetNull`. | `@orm.Rel{OnDelete: orm.SetNull, Null: true}` keeps a shelf after its category is deleted. |
| `Related` | Names the inverse on the target model for string lookups, prefetching and generated typed paths. It must not clash with a target column or another inverse. | `Shelf.Category` with `@orm.Rel{Related: "shelves"}` creates `CategoryColumns.Shelves()` and the `"shelves"` lookup. |
| `Column` | Overrides the foreign key's database column; `Category` otherwise uses `category_id`. | `@orm.Rel{Column: "category_ref"}` stores the category key in `category_ref`. |
| `Through` | Names an M2M join table; the default is `<owner table>_<field name>`. | `Product.Tags` with `@orm.Rel{Through: "product_tags"}` uses `product_tags`. |
| `Null` | Allows the foreign key column to hold SQL NULL, so an `orm.FK` may be unset. | `@orm.Rel{Null: true}` allows a shelf without a category. |
| `CrossDB` | The target is on [another database](/orm/databases#cross-database-relations). Omits the foreign key constraint; use `PrefetchRelated` to read rows. Joins, `SelectRelated`, and filtering or ordering through it are unavailable; filtering its local key still works. | `@orm.Rel{CrossDB: true}` marks a warehouse relation stored on another database. |

Options can be combined on one relation:

```vuka
type Shelf struct {
    orm.Base @orm.Meta{Table: "shelves"}
    ID int64 @orm.PK
    Category orm.FK[Category] @orm.Rel{Related: "shelves", Column: "category_ref", OnDelete: orm.SetNull, Null: true}
}
```

Here `shelves.category_ref` holds the category key and may be NULL.
Deleting a category clears that key while keeping its shelves.
`CategoryColumns.Shelves()` reaches the inverse; for example,
`CategoryColumns.Shelves().Count()` counts a category's shelves.

An M2M relation uses `Through` for its join table and `Related` for its inverse:

```vuka
Tags orm.M2M[Tag] @orm.Rel{Through: "product_tags", Related: "products"}
```

On `Product`, this uses `product_tags` for links and generates
`TagColumns.Products()` for the inverse.

A foreign key tagged `orm:"related:posts"` (or `@orm.Rel{Related: "posts"}`
in Vuka) generates `UserColumns.Posts()`, a typed path to `PostFields`.
Many-to-many relations generate their named inverse too. Generation reports
both declarations when inverse names clash. Columns are fields and every relation hop is a method:
`CategoryColumns.Shelves().Products().SaleEnds`.

### Conditions on Models

`ProductFields` is the generated receiver type; `ProductColumns` is the root
lookup value. A relation carries the same type with a different prefix:

```go
func (p PostFields) Popular() orm.Cond {
    return p.Views.Gte(100)
}
```

Use `PostColumns.Popular()` at the root or `UserColumns.Posts().Popular()`
through a relation. `And`, `Or`, and `Not` combine conditions. A relation's
`Where` accepts conditions made from its own path and applies them inside
the aggregate, preserving parents with no matching rows.

## Nullable Fields {#option-fields}

<!-- go:context
var ada, golang, tools = User{}, Tag{}, Tag{}
var p Post
-->

An `Option[T]` field is a nullable column: `None` is NULL. You set it and
filter on its NULLs like this:

<!-- go:fragment -->
```go
p.Published = vuka.Some(time.Now())
drafts, err := orm.Objects[Post]().Filter(PostColumns.Published.IsNull(true)).All(ctx)
```

Its typed field is an `orm.Field[V]` with `IsNull` for its NULLs.

A Go model writes a nullable field as a pointer, `Published *time.Time`
(nil is NULL), the usual Go, or as a `vuka.Option[time.Time]`, what a Vuka
model's `Option[T]` is and what `vuka eject` keeps: the ORM, JSON and an
island's props read both. `orm generate` writes a row scanner for either.

## Models in Plain Go {#plain-go}

A Go model embeds `orm.Model[T]` (the type `orm.Base` stands for in a Vuka
model) and says in its `Schema` method what a Vuka model says in attributes.
`s.Model(…)` takes the model's (`orm.Meta`, any `orm.ModelAttr`), and
`s.Field(&p.X, …)` a field's — the same values: `@orm.Char{Max: 200}` on
`Title` is `s.Field(&p.Title, orm.Char{Max: 200})`.

The statics are plain names:

| Vuka | Go |
|---|---|
| `Post.Objects` | `orm.Objects[Post]()`; `orm.For[Post](opts…)` for a manager with options |
| `Post.Title` (a field reference) | `PostColumns.Title`, written by `ormgen` |
| `func Post.Live() orm.QuerySet[Post]` | `func PostLive() orm.QuerySet[Post]` |
| `@orm.Entity` | `orm.Register[T]()` at init; `orm.Check(models…)` takes others |

### Generated Fields

`PostColumns` comes from `ormgen` — `//go:generate go run
github.com/vuka-lang/orm/v2/cmd/ormgen` beside the models — as an
`orm.Field[V]` (an `orm.TextField[V]` for strings) per column and a method
per relation (`PostColumns.Author().Name`, `PostColumns.Tags()`). `PostFields` is
the receiver type; `PostPath` remains an alias. Regenerate fields after
upgrading and rename Go lookup values from `PostFields` to `PostColumns`.
It is a
convenience: without it every query takes the engine's strings,
`orm.Q{"name__startswith": "a"}` and `OrderBy("-views")`, and an unknown
lookup is a runtime error.

### Mixing Go and Vuka Models

The engine is the same, so Go and Vuka models live side by side, share
databases and relate to each other. `orm.Check()` reports models the ORM
can't map — a `Schema` given something other than the address of one of the
receiver's fields among them; [`vukavet`](/tools/vukavet) finds those in
the source. `orm:` struct tags and `orm.SetFieldOptions(fn)` still work,
deprecated for `Schema`.

Vuka field sugar keeps its source form, `Post.Author.Name`; the compiler
lowerings are `PostColumns.Author().Name` with the new ORM and
`PostFields.Author.Name` with older generators, selected from the app's
ORM API and generated roots. Eject and gen make the same choice.
