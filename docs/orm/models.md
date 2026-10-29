# Models

[[toc]]

<!-- vuka:package -->

## Introduction

A model is the single place that says what a table holds: a struct whose
fields are its columns, with attributes saying what each column is. From it
the ORM makes the table and its migrations, checks what is written, and
gives you `Post.Objects` to [query](/orm/queries) its rows.

Each sample is Vuka, with the Go it compiles to beside it; a model written
in Go from the start is the same Go ([Models in Plain Go](#plain-go)).

## A Quick Example {#quick-example}

An author has a name, a unique email and the time they joined:

::: code-group
```vuka [Vuka]
type Author struct {
	orm.Base
	ID     int64
	Name   string    @orm.Char{Max: 100}
	Email  string    @orm.Char{Max: 200, Unique: true, Lower: true}
	Joined time.Time @orm.Datetime{AutoNowAdd: true}
}
```
```go [Generated Go]
type Author struct {
	orm.Model[Author]
	ID     int64
	Name   string
	Email  string
	Joined time.Time
}

func (a *Author) Schema(s *orm.Schema) {
	s.Field(&a.Name, orm.Char{Max: 100})
	s.Field(&a.Email, orm.Char{Max: 200, Unique: true, Lower: true})
	s.Field(&a.Joined, orm.Datetime{AutoNowAdd: true})
}
```
:::

`orm.Base` makes the struct a model. The ORM reads the rest: `ID` is the
primary key, counted by the database; each field is a column named in
snake_case; the table is the type's name in the plural, `authors`. On
SQLite the table is:

```sql
CREATE TABLE authors (
  id     INTEGER PRIMARY KEY AUTOINCREMENT,
  name   VARCHAR(100) NOT NULL,
  email  VARCHAR(200) NOT NULL,
  joined DATETIME NOT NULL,
  CONSTRAINT uniq_authors_email UNIQUE (email)
)
```

`makemigrations` writes the change as a Go migration, and `migrate`
applies it ([Migrations](/orm/migrations)):

```sh
go tool orm makemigrations
go tool orm migrate
```

## Fields {#fields}

### Field Types {#column-types}

A field's Go type decides its column:

| Go type | Postgres | MySQL | SQLite |
|---|---|---|---|
| `int64`, `int` (the key: auto-counted) | `BIGINT` (`BIGSERIAL`) | `BIGINT` (`AUTO_INCREMENT`) | `BIGINT` (`INTEGER … AUTOINCREMENT`) |
| `int32`, `int16` (or `@orm.Int{Size: …}`) | `INTEGER`, `SMALLINT` | the same | the same |
| `float64`, `float32` | `DOUBLE PRECISION`, `REAL` | `DOUBLE`, `REAL` | `REAL` |
| `bool` | `BOOLEAN` | `BOOLEAN` | `BOOLEAN` |
| `string` | `TEXT` | `TEXT` (`VARCHAR(255)` when indexed) | `TEXT` |
| `string` with `@orm.Char{Max: n}` | `VARCHAR(n)` | `VARCHAR(n)` | `VARCHAR(n)` |
| `time.Time` | `TIMESTAMP` | `DATETIME(6)` | `DATETIME` |
| `time.Time` with `@orm.DateField` | `DATE` | `DATE` | `DATE` |
| `string` with `@orm.PK{UUID: true}` | `uuid` | `CHAR(36)` | `CHAR(36)` |
| `[]byte` | `BYTEA` | `LONGBLOB` | `BLOB` |
| `orm.JSON[T]` | `jsonb` | `JSON` | `TEXT` |
| `orm.CSV[E]` | `TEXT`, comma-separated | the same | the same |
| a value enum | its value type's | | |
| `orm.Vector`, `orm.TSVector` | `vector(n)`, `tsvector` ([Search](/orm/search)) | | |

A column is `NOT NULL` unless its field is an `Option[T]` or a pointer
([Nullable Fields](#option-fields)), or a `[]byte`, `orm.JSON` or `orm.CSV`,
whose empty value is NULL. `@orm.DBType("citext")` writes a type
of your own, and a type with `Scan` and `Value` methods is a column too
([Custom Value Types](/orm/fields#value-types)).

This model uses most of them:

::: code-group
```vuka [Vuka]
type Post struct {
	orm.Base @orm.Meta{Ordering: []string{"-published", "id"}}
	ID        int64
	Title     string                  @orm.Char{Max: 200, Label: "Title"}
	Slug      string                  @orm.Slug{Unique: true}
	Body      string
	Status    Status                  @orm.Index
	Author    orm.FK[Author]          @orm.Rel{OnDelete: orm.Cascade, Related: "posts"}
	Tags      orm.M2M[Tag]            @orm.Rel{Related: "posts"}
	Published Option[time.Time]       @orm.Index
	Views     int                     @orm.DefaultTo(0)
	Rating    float64
	Pinned    bool
	Cover     []byte
	Meta      orm.JSON[map[string]string]
	Keywords  orm.CSV[string]
	Edited    time.Time               @orm.Datetime{AutoNow: true}
}

type Tag struct {
	orm.Base
	ID   int64
	Name string @orm.Slug{Unique: true}
}
```
```go [Generated Go]
type Post struct {
	orm.Model[Post]
	ID        int64
	Title     string
	Slug      string
	Body      string
	Status    Status
	Author    orm.FK[Author]
	Tags      orm.M2M[Tag]
	Published vuka.Option[time.Time]
	Views     int
	Rating    float64
	Pinned    bool
	Cover     []byte
	Meta      orm.JSON[map[string]string]
	Keywords  orm.CSV[string]
	Edited    time.Time
}

func (p *Post) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Ordering: []string{"-published", "id"}})
	s.Field(&p.Title, orm.Char{Max: 200, Label: "Title"})
	s.Field(&p.Slug, orm.Slug{Unique: true})
	s.Field(&p.Status, orm.Index{})
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "posts"})
	s.Field(&p.Tags, orm.Rel{Related: "posts"})
	s.Field(&p.Published, orm.Index{})
	s.Field(&p.Views, orm.DefaultTo(0))
	s.Field(&p.Edited, orm.Datetime{AutoNow: true})
}

type Tag struct {
	orm.Model[Tag]
	ID   int64
	Name string
}

func (t *Tag) Schema(s *orm.Schema) {
	s.Field(&t.Name, orm.Slug{Unique: true})
}
```
:::

### Field Options {#field-attributes}

Attributes after a field say what its column is beyond its type. In Go each
is a value passed to `s.Field` in the model's `Schema` method, as the
generated Go shows:

| Attribute | Column |
|---|---|
| `@orm.PK{Manual, UUID, …}` | the primary key: `ID` is one already |
| `@orm.Char{Max, Min, Unique, Index, Default, Choices, Pattern, Trim, Lower, Label, Help, …}` | `VARCHAR(Max)`, checked as written |
| `@orm.Int{Min, Max, Size, Unsigned, Choices, …}` | an integer column, bounded |
| `@orm.DateField{…}`, `@orm.Datetime{Precision, UTC, AutoNow, AutoNowAdd, DefaultNow, …}` | `DATE`, a timestamp |
| `@orm.Slug{Max, Unique}` | an indexed `VARCHAR`, 50 by default |
| `@orm.LongText` | `TEXT` (a string's default) |
| `@orm.Unique`, `@orm.Index` | a unique constraint, an index |
| `@orm.UniqueTogether("name")` | a unique index over every field given that name |
| `@orm.Column("name")` | the column's name; the field's in snake_case otherwise |
| `@orm.DBType("jsonb")` | the database type, written as is |
| `@orm.Null` | NULL allowed, for a type with no empty value of its own (an `orm.FK` that may be unset) |
| `@orm.Skip` | no column |
| `@orm.DefaultTo(v)`, `@orm.DefaultSQL("CURRENT_TIMESTAMP")` | the column default: a Go value, or SQL |
| `@orm.AutoNow`, `@orm.AutoNowAdd` | the time on every save, on create |
| `@orm.Rel{OnDelete, Related, Column, Through, Null, CrossDB}` | a relation's: [below](#relation-options) |
| `@orm.Dims(n)`, `@orm.HNSW{…}`, `@orm.IVFFlat{…}`, `@orm.Trigram`, `@orm.FullText{…}`, `@orm.SearchOf{…}` | [search](/orm/search) columns and indexes |

Attributes are Go values, type-checked: a misspelt setting is a compile
error on the attribute. They apply left to right. [Fields](/orm/fields) has
each setting: defaults, the checks a write makes, comments, collations.

- **Null:** an `Option[T]` field (or a pointer) is a nullable column;
  `None` is NULL.
- **Defaults:** `Default` in `Char` and `Int`, or `@orm.DefaultTo(v)`, is
  both the table's `DEFAULT` and the value a new row gets when the field is
  left empty.
- **Choices:** `Choices` in `Char` or `Int` restricts the values, or the
  field is a [value enum](#enums).
- **Labels:** `Label` and `Help` are what forms and tools show for a field
  ([Field Info](/orm/fields#field-info)).
- **Keys:** `ID int64` is the key unless a field has `@orm.PK`.
  `@orm.PK{UUID: true}` on a string makes a random UUID on create;
  `@orm.PK{Manual: true}` a key the program sets.
- **Timestamps:** `AutoNowAdd` sets the time on create, `AutoNow` on every
  save, both by the ORM's clock (`orm.Clock`).

## Relationships {#relationships}

Relations are fields too. The field types hold both the key and, once
loaded, the related rows.

### Many-to-One: Foreign Keys {#foreign-keys}

`orm.FK[T]` is a foreign key: the column `<field>_id` and the row it points
at once loaded. A comment belongs to a post:

::: code-group
```vuka [Vuka]
type Comment struct {
	orm.Base
	ID      int64
	Post    orm.FK[Post] @orm.Rel{OnDelete: orm.Cascade, Related: "comments"}
	Name    string       @orm.Char{Max: 80}
	Body    string
	Created time.Time    @orm.Datetime{AutoNowAdd: true}
}
```
```go [Generated Go]
type Comment struct {
	orm.Model[Comment]
	ID      int64
	Post    orm.FK[Post]
	Name    string
	Body    string
	Created time.Time
}

func (c *Comment) Schema(s *orm.Schema) {
	s.Field(&c.Post, orm.Rel{OnDelete: orm.Cascade, Related: "comments"})
	s.Field(&c.Name, orm.Char{Max: 80})
	s.Field(&c.Created, orm.Datetime{AutoNowAdd: true})
}
```
:::

`OnDelete` says what deleting a post does to its comments, written into the
table's foreign key: `orm.Cascade` deletes them, `orm.SetNull` clears their
key (the field then needs `Null: true`), and `orm.Restrict` refuses to
delete a post that still has comments.

| `orm.FK` | |
|---|---|
| `orm.FKOf(&row)`, `orm.FKID[T](key)` | an FK to a row, to a key |
| `f.Get(ctx)` | the related row: the one held, else read and held |
| `f.Related()`, `f.Loaded()` | the row held (nil when not loaded); whether one is |
| `f.ID()`, `f.Key()`, `f.IsSet()` | the key as `int64`, as it is; whether there is one |
| `f.Set(&row)`, `f.SetID(key)` | point it elsewhere |

Its JSON is the key.

### Many-to-Many {#many-to-many}

`orm.M2M[T]` is a many-to-many through a table between, `posts_tags`
(`<table>_<field>`, or `Rel`'s `Through`). `Post.Tags` above declares one;
its methods are `All`, `Add`, `Remove`, `Set` and `Clear` on a saved row,
and `Rows` and `Loaded` for what `PrefetchRelated` loaded. See
[Adding and Removing Related Rows](/orm/queries#adding-related-rows).

The table between holds the two keys and nothing else. A link that carries
data of its own — a role, the time it was made — is a model with a foreign
key to each side, and `@orm.UniqueTogether` to keep each pair once:

::: code-group
```vuka [Vuka]
type Contribution struct {
	orm.Base
	ID     int64
	Post   orm.FK[Post]   @orm.Rel{OnDelete: orm.Cascade, Related: "contributions"} @orm.UniqueTogether("post_author")
	Author orm.FK[Author] @orm.Rel{OnDelete: orm.Cascade, Related: "contributions"} @orm.UniqueTogether("post_author")
	Role   string         @orm.Char{Max: 20, Choices: []string{"author", "editor", "reviewer"}}
	Added  time.Time      @orm.Datetime{AutoNowAdd: true}
}
```
```go [Generated Go]
type Contribution struct {
	orm.Model[Contribution]
	ID     int64
	Post   orm.FK[Post]
	Author orm.FK[Author]
	Role   string
	Added  time.Time
}

func (c *Contribution) Schema(s *orm.Schema) {
	s.Field(&c.Post, orm.Rel{OnDelete: orm.Cascade, Related: "contributions"}, orm.UniqueTogether("post_author"))
	s.Field(&c.Author, orm.Rel{OnDelete: orm.Cascade, Related: "contributions"}, orm.UniqueTogether("post_author"))
	s.Field(&c.Role, orm.Char{Max: 20, Choices: []string{"author", "editor", "reviewer"}})
	s.Field(&c.Added, orm.Datetime{AutoNowAdd: true})
}
```
:::

Queries go through it from either side: `PostColumns.Contributions().Role`,
`AuthorColumns.Contributions().Post().Title`.

### One-to-One {#one-to-one}

A foreign key that is also unique is a one-to-one: an author has at most
one profile.

::: code-group
```vuka [Vuka]
type Profile struct {
	orm.Base
	ID     int64
	Author orm.FK[Author] @orm.Rel{OnDelete: orm.Cascade, Related: "profile"} @orm.Unique
	Bio    string
}
```
```go [Generated Go]
type Profile struct {
	orm.Model[Profile]
	ID     int64
	Author orm.FK[Author]
	Bio    string
}

func (p *Profile) Schema(s *orm.Schema) {
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "profile"}, orm.Unique{})
}
```
:::

From the profile it is a foreign key like any other. From the author,
`AuthorColumns.Profile()` is a join rather than an `EXISTS`, since there is
at most one row, and an `Author` field `Profile *Profile` holds it once
loaded.

### Relations Backward {#reverse-relations}

`Related` names the relation from the other side. `Related: "posts"` on
`Post.Author` gives authors their posts:

- in queries, a typed path: `AuthorColumns.Posts()` in Go, and its fields,
  `AuthorColumns.Posts().Title`; every relation hop is a method, and
  columns are fields (`CategoryColumns.Shelves().Products().SaleEnds`);
- in `orm.Q` keys and `PrefetchRelated`, the name: `"posts__title"`;
- on the row, when the model has a field of that name, `Posts []Post` (or
  `Profile *Profile` for a one-to-one), the rows `Load` and
  `PrefetchRelated` read.

A many-to-many's `Related` names its other side the same way:
`TagColumns.Posts()` for `Post.Tags`. A `Related` name must not clash with a
column or another relation of the target model; `orm generate` names both
declarations when it does. In a `.vuka` file a field reference reaches a
relation backward through the field that holds it (`Author.Posts.Title`
needs `Posts []Post` on `Author`); without one, use the generated path,
`AuthorColumns.Posts()`. See [Following a Relation
Backward](/orm/queries#following-relationships-backward).

### Relation Options {#relation-options}

`@orm.Rel{…}` describes a relation:

| `orm.Rel` field | |
|---|---|
| `OnDelete` | `orm.Cascade`, `orm.SetNull` (with `Null`) or `orm.Restrict`: what deleting the target does to the rows pointing at it |
| `Related` | the name of the relation from the other side |
| `Column` | the foreign key's column; `<field>_id` otherwise |
| `Through` | a many-to-many's table between; `<table>_<field>` otherwise |
| `Null` | the key may be unset (SQL NULL) |
| `CrossDB` | the target is on [another database](/orm/databases#cross-database-relations): no foreign key constraint, no joins; `PrefetchRelated` reads it |

## Model Options {#meta}

`@orm.Meta{…}` on `orm.Base` describes the model as a whole, as `Post`'s
`Ordering` above does:

| `orm.Meta` field | |
|---|---|
| `Table` | the table; the plural of the type's name in snake_case otherwise (`Post` → `posts`) |
| `Ordering` | the order of a query that sets none: `[]string{"-published", "id"}` |
| `Indexes` | its [search and vector indexes](/orm/search) (`orm.FullTextIndex`, `orm.GinIndex`, …), as an `Indexes()` method returns them |
| `DB` | the [database](/orm/databases#multiple-databases) it lives on; the default otherwise |
| `Read`, `Write` | where its reads and its writes go, over `DB` |
| `Mirror` | a database its writes are repeated on |
| `Unmanaged` | the database's own table: never created or migrated |

A `Meta() orm.Meta` method works too, and wins. A one-column index is
`@orm.Index` on the field, a unique pair `@orm.UniqueTogether`.

## Model Methods {#orm-model}

### Row Methods

`orm.Base` gives the rows their methods:

| | |
|---|---|
| `p.Save(ctx)` | inserts a new row (its key filled in), updates a saved one |
| `p.SaveFields(ctx, Post.Title, …)` | updates only those columns |
| `p.Refresh(ctx)` | reads the row again |
| `p.Delete(ctx)` | deletes it |
| `p.Load(ctx, Post.Tags, …)` | reads relations into the row |
| `p.Add(ctx, "tags", rows…)`, `Remove`, `Set` | changes a many-to-many's links, from either side |

### Methods, Statics and Hooks {#methods}

A model is a struct, so it has methods of its own: what a row computes
from its fields. A query used in many places is a static: `Post.Live()`, as
`Post.Objects` is. A row's `BeforeSave`, `AfterSave`, `BeforeCreate`,
`AfterCreate`, `BeforeDelete` and `AfterDelete`, when it has them, run
around each write of that row; an error from a `Before` stops it:

::: code-group
```vuka [Vuka]
func (p Post) URL() string { return "/posts/" + p.Slug }

func (p Post) IsLive() bool { return p.Status == Status.Published && p.Published.IsSome() }

func Post.Live() orm.QuerySet[Post] {
	return Post.Objects.Filter(Post.Status.Eq(Status.Published), Post.Published.IsNull(false))
}

func (p *Post) BeforeSave(ctx context.Context) error {
	if p.Slug == "" {
		p.Slug = strings.ToLower(strings.ReplaceAll(p.Title, " ", "-"))
	}
	return nil
}
```
```go [Generated Go]
func (p Post) URL() string { return "/posts/" + p.Slug }

func (p Post) IsLive() bool { return p.Status == StatusPublished && p.Published.IsSome() }

func PostLive() orm.QuerySet[Post] {
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq(StatusPublished), PostColumns.Published.IsNull(false))
}

func (p *Post) BeforeSave(ctx context.Context) error {
	if p.Slug == "" {
		p.Slug = strings.ToLower(strings.ReplaceAll(p.Title, " ", "-"))
	}
	return nil
}
```
:::

`Post.Live()` is used like the manager: `Post.Live().Filter(…).Count(ctx)`.
A query's `Update` and `Delete` write many rows in one statement and run no
hooks.

## Shared Fields: Embedding {#embedding}

Fields several models share go in a struct of their own, embedded in each.
Its fields, attributes included, become every embedding model's columns:

::: code-group
```vuka [Vuka]
type Stamps struct {
	Created time.Time @orm.Datetime{AutoNowAdd: true}
	Updated time.Time @orm.Datetime{AutoNow: true}
}

type Page struct {
	orm.Base
	Stamps
	ID    int64
	Path  string @orm.Char{Max: 200, Unique: true}
	Title string
}
```
```go [Generated Go]
type Stamps struct {
	Created time.Time
	Updated time.Time
}

type Page struct {
	orm.Model[Page]
	Stamps
	ID    int64
	Path  string
	Title string
}

func (p *Page) Schema(s *orm.Schema) {
	s.Field(&p.Path, orm.Char{Max: 200, Unique: true})
}

func init() {
	vuka.RegisterFields[Stamps](map[string][]any{
		"Created": {orm.Datetime{AutoNowAdd: true}},
		"Updated": {orm.Datetime{AutoNow: true}},
	})
}
```
:::

`pages` has `created` and `updated` columns, and `PageColumns.Created`
filters on them. The embedded struct is no model and has no table. The
model base itself, `orm.Base`, is a generic type whose `Self` is the model
embedding it: that is how each model gets its own `Post.Objects`
([Statics](/features/statics#through-embedding-with-self)).

## Enums as Fields {#enums}

A value enum is a column of its value's type, and only its variants can be
written: the generated `Value` refuses anything else. Methods declared per
variant make one method on the enum, which a template or JSON can call:

::: code-group
```vuka [Vuka]
enum Status string {
	Draft     = "draft"
	Published = "published"
	Archived  = "archived"
}

func (Status.Draft) Label() string     { return "Draft" }
func (Status.Published) Label() string { return "Live" }
func (Status.Archived) Label() string  { return "Archived" }
```
```go [Generated Go]
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusArchived  Status = "archived"
)

func statusDraftLabel() string     { return "Draft" }
func statusPublishedLabel() string { return "Live" }
func statusArchivedLabel() string  { return "Archived" }

// …

// Value stores s in a database column as its value.
//
func (s Status) Value() (driver.Value, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid Status %q", string(s))
	}
	return string(s), nil
}

// Label calls the Label of s's variant: Status.Draft, Status.Published, Status.Archived.
//
func (s Status) Label() string {
	switch s {
	case StatusDraft:
		return statusDraftLabel()
	case StatusPublished:
		return statusPublishedLabel()
	case StatusArchived:
		return statusArchivedLabel()
	}
	panic("vuka: Status.Label called on an invalid Status: \"" + string(s) + "\"")
}
```
:::

`Post.Status` above is one: a `TEXT` column holding `draft`,
`published` or `archived`, compared with `Post.Status.Eq(Status.Published)`
and shown with `p.Status.Label()`. A numbered enum (`enum Priority uint8 {…}`)
is an integer column. An enum with data — variants carrying fields — is not
a column; store its parts in columns of their own. See
[Enums](/features/enums#enums-in-json-and-the-database).

## Nullable Fields {#option-fields}

An `Option[T]` field is a nullable column: `None` is NULL. You set it and
filter on its NULLs like this:

::: code-group
```vuka [Vuka]
func publishNow(ctx context.Context, p *Post) error {
	p.Published = Some(time.Now())
	return p.SaveFields(ctx, Post.Published)
}

var drafts = Post.Objects.Filter(Post.Published.IsNull(true))
```
```go [Generated Go]
func publishNow(ctx context.Context, p *Post) error {
	p.Published = vuka.Some(time.Now())
	return p.SaveFields(ctx, PostColumns.Published)
}

var drafts = orm.Objects[Post]().Filter(PostColumns.Published.IsNull(true))
```
:::

A Go model may write a nullable field as a pointer, `Published *time.Time`
(nil is NULL), or as a `vuka.Option[time.Time]`, what a Vuka model's
`Option[T]` is: the ORM, JSON and an island's props read both, and `orm
generate` writes a row scanner for either.

## Validation {#validation}

Every write checks each value against its field before it reaches the
database: `Char`'s `Max`, `Min`, `Choices` and `Pattern`, `Int`'s bounds, an
enum's variants, and a type's own `Validate()`. What fails is an
`orm.ErrInvalid` naming the fields:

::: code-group
```vuka [Vuka]
func credit(ctx context.Context, c *Contribution) string {
	match Contribution.Objects.Create(ctx, c) {
	case nil:
		return "saved"
	case orm.ErrInvalid:
		return "check the role"
	case orm.ErrConflict:
		return "already credited"
	case e:
		return e.Error()
	}
}
```
```go [Generated Go]
func credit(ctx context.Context, c *Contribution) string {
	switch m1 := orm.Objects[Contribution]().Create(ctx, c); {
	case m1 == nil:
		return "saved"
	case errors.Is(m1, orm.ErrInvalid):
		return "check the role"
	case errors.Is(m1, orm.ErrConflict):
		return "already credited"
	default:
		e := m1
		return e.Error()
	}
}
```
:::

`errors.As(err, &e)` with an `*orm.Error` gives each field's messages,
`e.Fields`; a web app answers them with a 422. See
[Validation](/orm/fields#validation).

## Models in Plain Go {#plain-go}

A Go model embeds `orm.Model[T]` (what `orm.Base` stands for) and says in
its `Schema` method what a Vuka model says in attributes — the Generated Go
tabs above are such models. `s.Model(…)` takes the model's attributes
(`orm.Meta`), and `s.Field(&p.X, …)` a field's; `Schema` names each field
by its address, so a renamed field is a compile error too. The ORM calls it
once, on a zero value. A model written in Go is registered at init when the
migration commands must know it without `orm generate`:

```go
type Entry struct {
	orm.Model[Entry]
	ID   int64
	Note string
}

func (e *Entry) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Table: "audit_entries"})
	s.Field(&e.Note, orm.Char{Max: 500})
}

func init() { orm.Register[Entry]() }
```

The statics are plain names in Go:

| Vuka | Go |
|---|---|
| `Post.Objects` | `orm.Objects[Post]()`; `orm.For[Post](opts…)` for a manager with options |
| `Post.Title` (a field reference) | `PostColumns.Title`, written by `orm generate` |
| `Author.Posts.Title` | `AuthorColumns.Posts().Title`: every relation hop is a method |
| `func Post.Live() orm.QuerySet[Post]` | `func PostLive() orm.QuerySet[Post]` |
| `@orm.Entity` | `orm.Register[T]()` at init; `orm.Check(models…)` takes others |

### Generated Fields

`PostColumns` comes from `orm generate` — `//go:generate go run
github.com/vuka-lang/orm/v2/cmd/ormgen` beside Go models; `vuka build` does
it for Vuka models — as an `orm.Field[V]` per column (an
`orm.TextField[V]` for strings, `orm.NumberField[V]` for numbers,
`orm.TimeField` for times) and a method per relation. `PostFields` is its
type, the type of every path ending at a post, and the receiver for
[named conditions](/orm/queries#model-conditions); `PostPath` remains an
alias. Without generated fields every query takes the engine's strings,
`orm.Q{"name__startswith": "a"}` and `OrderBy("-views")`, and an unknown
lookup is a runtime error.

### Mixing Go and Vuka Models

The engine is the same, so Go and Vuka models live side by side, share
databases and relate to each other. `orm.Check()` reports models the ORM
can't map — a `Schema` given something other than the address of one of the
receiver's fields among them; [`vukavet`](/tools/vukavet) finds those in
the source. `orm:` struct tags and `orm.SetFieldOptions(fn)` still work,
deprecated for `Schema`.
