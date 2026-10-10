# Models

A model is a struct embedding `orm.Base`. Its fields are its columns, typed
[field attributes](/features/fields#field-attributes) say what the columns
are, and `Post.Objects` is its query.

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

**Go:** a struct tag says it — `` `orm:"unique"` `` — as a string the compiler
never reads; a typo is a column nobody asked for.

**Vuka adds** attributes that are Go values: `@orm.Char{Max: 200}` is a
composite literal, type-checked, so a wrong field name is a compile error on
the attribute.

## orm.Base

`orm.Base` is a Vuka model's base; Vuka fills in the type, so `orm.Base` in
`Post` is `orm.Base[Post]` — its type parameter is named
[`Self`](/features/statics#through-embedding-with-self), and its static
`Objects` has one value per model (`Post.Objects` is
`orm.Base_Objects[Post]().V` in Go). It gives the rows their methods:

| | |
|---|---|
| `p.Save(ctx)` | inserts a new row (its key filled in), updates a saved one |
| `p.SaveFields(ctx, Post.Title, …)` | updates only those columns |
| `p.Refresh(ctx)` | reads the row again |
| `p.Delete(ctx)` | deletes it |
| `p.Load(ctx, Post.Tags, …)` | reads relations into the row |
| `p.Add(ctx, Post.Tags, rows…)`, `Remove`, `Set` | changes a many-to-many's links |

and a static, `Post.Objects`: the model's [`orm.Query[Post]`](/orm/queries).
A [static method](/features/statics) names a query:

```vuka
// Live is the published posts.
func Post.Live() orm.Query[Post] { return Post.Objects.Filter(Post.Published.NotNil()) }

n := Post.Live().Count(ctx)?
```

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

```vuka
@orm.Meta{Table: "audit_entries"}
@orm.Entity
type Entry struct {
	orm.Base
	ID   int64
	Note string
}
```

## Field attributes

Attributes say what tags say, and win over them:

| Attribute | Column |
|---|---|
| `@orm.PK` | the primary key (`ID` is one by default) |
| `@orm.Char{Max: n, Unique, Index}` | `VARCHAR(n)` |
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

Any number may follow a field. Column types follow the Go type: integers,
floats, `bool`, `string`, `time.Time`, `[]byte`; `orm.JSON[T]` (a JSON
column), `orm.CSV[E]` (a comma-separated list), `orm.Vector`,
`orm.TSVector`.

## Relations

**`orm.FK[T]`** is a foreign key as one field: the column `<field>_id`, and
the related row once it is loaded (`SelectRelated`, `PrefetchRelated`,
`Get`).

```vuka
p := Post{Title: "Go generics", Author: orm.FKOf(&ada)}   // from a row
q := Post{Title: "SQL", Author: orm.FKID[User](7)}         // from a key

author := p.Author.Get(ctx)?        // read once, then held
name := p.Author.Related().Name     // nil until loaded
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

```vuka
p.Tags.Add(ctx, &golang, &tools)?
tags := p.Tags.All(ctx)?             // the rows held, else read
p.Tags.Remove(ctx, &tools)?
p.Tags.Set(ctx, &golang)?            // its only links
p.Tags.Clear(ctx)?
```

`All`, `Query` (the related rows as an `orm.Query[T]`), `Add`, `Remove`,
`Set`, `Clear` (on a saved row; rows or keys), `Rows` and `Loaded` (what
`PrefetchRelated` loaded).

Both implement `vuka.Related[T]`, so a [field reference](/features/fields#field-references)
goes on through them into the related model: `Post.Author.Name`,
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

```vuka
p.Published = Some(time.Now())
drafts := Post.Objects.Filter(Post.Published.IsNil()).All(ctx)?
```

Its reference is a `vuka.NullableRef`: `IsNil`, `NotNil`, and `Eq`, `Ne`,
`In`.

## Plain Go

A Go model embeds `orm.Model[T]`, and its manager is a package-level value;
tags say what attributes say:

```go
type User struct {
	orm.Model[User]
	ID    int64
	Name  string
	Email string `orm:"unique"`
	Posts []Post `orm:"rel:author_id"`
}

type Post struct {
	orm.Model[Post]
	ID       int64
	Title    string
	AuthorID int64
	Author   *User
	Tags     []Tag `gorm:"many2many:post_tags"` // GORM's relation tags are read too
}

var (
	Users = orm.For[User]()
	Posts = orm.For[Post]()
)

adults, err := Users.Filter(orm.Q{"age__gte": 18}).OrderBy("-age").All(ctx)
```

The engine is the same, so Go and Vuka models live side by side, share
databases and relate to each other. `orm.Check()` reports models the ORM
can't map; `orm.SetFieldOptions(fn)` supplies a field's options from outside
the struct, over tags and attributes.
