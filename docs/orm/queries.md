# Queries

[[toc]]

<!-- vuka:package -->

## Introduction

Once your [models](/orm/models) are declared, you create, read, change and
delete their rows through queries. `Post.Objects`, the model's manager,
starts an `orm.QuerySet[Post]`: a query you narrow, order and shape, which
becomes one SQL statement when a terminal such as `All`, `Get` or `Count`
runs it.

This page goes from writing rows to reading them, filtering through
relations, aggregating and running your own SQL. [Common
Queries](/orm/cookbook) has short recipes for everyday questions.

Each sample is Vuka, with the Go it compiles to beside it: `Post.Objects`
is `orm.Objects[Post]()`, and a field reference such as `Post.Title` is
`PostColumns.Title`, the typed field `orm generate` writes for the model.
The SQL below a sample is what SQLite runs, with the quoting left out; the
ORM writes the same query for Postgres and MySQL in their dialect.

## The Example Models {#example-models}

The samples use a blog: authors write posts, posts have tags and comments,
and an author may have one profile.

::: code-group
```vuka [Vuka]
type Author struct {
	orm.Base
	ID      int64
	Name    string
	Email   string
	Posts   []Post   // filled by PrefetchRelated and Load
	Profile *Profile // the same, for the one-to-one
}

type Profile struct {
	orm.Base
	ID     int64
	Author orm.FK[Author] @orm.Rel{OnDelete: orm.Cascade, Related: "profile"} @orm.Unique
	Bio    string
}

type Tag struct {
	orm.Base
	ID   int64
	Name string
}

type Post struct {
	orm.Base
	ID        int64
	Title     string
	Slug      string            @orm.Slug{Unique: true}
	Body      string
	Status    string            @orm.Char{Max: 20, Default: "draft"}
	Author    orm.FK[Author]    @orm.Rel{OnDelete: orm.Cascade, Related: "posts"}
	Tags      orm.M2M[Tag]      @orm.Rel{Related: "posts"}
	Published Option[time.Time]
	Views     int
	Likes     int
	Comments  []Comment
}

type Comment struct {
	orm.Base
	ID      int64
	Post    orm.FK[Post] @orm.Rel{OnDelete: orm.Cascade, Related: "comments"}
	Name    string
	Body    string
	Created time.Time    @orm.Datetime{AutoNowAdd: true}
}
```
```go [Generated Go]
type Author struct {
	orm.Model[Author]
	ID      int64
	Name    string
	Email   string
	Posts   []Post   // filled by PrefetchRelated and Load
	Profile *Profile // the same, for the one-to-one
}

type Profile struct {
	orm.Model[Profile]
	ID     int64
	Author orm.FK[Author]
	Bio    string
}

func (p *Profile) Schema(s *orm.Schema) {
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "profile"}, orm.Unique{})
}

type Tag struct {
	orm.Model[Tag]
	ID   int64
	Name string
}

type Post struct {
	orm.Model[Post]
	ID        int64
	Title     string
	Slug      string
	Body      string
	Status    string
	Author    orm.FK[Author]
	Tags      orm.M2M[Tag]
	Published vuka.Option[time.Time]
	Views     int
	Likes     int
	Comments  []Comment
}

func (p *Post) Schema(s *orm.Schema) {
	s.Field(&p.Slug, orm.Slug{Unique: true})
	s.Field(&p.Status, orm.Char{Max: 20, Default: "draft"})
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "posts"})
	s.Field(&p.Tags, orm.Rel{Related: "posts"})
}

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
	s.Field(&c.Created, orm.Datetime{AutoNowAdd: true})
}
```
:::

<!-- vuka:context
var (
	ada           Author
	p             Post
	golang, tools Tag
)

var ErrDuplicateTitle = errors.New("a published post has this title")
-->

A model embeds `orm.Base` and says what its columns are with attributes; in
Go it embeds `orm.Model[Post]` and says it in a `Schema` method. `Related:
"posts"` names the way back from an author to their posts: `Author.Posts` in
a query.

## Creating Rows {#creating}

A row is a struct value. `Save` on a new row inserts it and fills in its key;
`Create` does the same through the manager:

::: code-group
```vuka [Vuka]
func firstPost(ctx context.Context) (Post, error) {
	ada := Author{Name: "Ada", Email: "ada@example.com"}
	ada.Save(ctx)? // INSERT INTO authors …; ada.ID is set

	post := Post{Title: "Go generics", Slug: "go-generics", Author: orm.FKOf(&ada)}
	Post.Objects.Create(ctx, &post)?
	return post, nil // post.Status is "draft", the field's default
}
```
```go [Generated Go]
func firstPost(ctx context.Context) (Post, error) {
	ada := Author{Name: "Ada", Email: "ada@example.com"}
	if err := ada.Save(ctx); err != nil {
		return Post{}, err
	} // INSERT INTO authors …; ada.ID is set

	post := Post{Title: "Go generics", Slug: "go-generics", Author: orm.FKOf(&ada)}
	if err := orm.Objects[Post]().Create(ctx, &post); err != nil {
		return Post{}, err
	}
	return post, nil // post.Status is "draft", the field's default
}
```
:::

A foreign key takes a row, `orm.FKOf(&ada)`, or a key you already have,
`orm.FKID[Author](7)`. Fields left at their zero value take the defaults
their attributes declare ([Defaults](/orm/fields#defaults)), and every value
is checked against its field before it is written
([Validation](/orm/fields#validation)).

`BulkCreate` inserts many rows in one statement and fills in each key:

::: code-group
```vuka [Vuka]
func series(ctx context.Context, author *Author) ([]*Post, error) {
	posts := []*Post{
		{Title: "Part 1", Slug: "part-1", Author: orm.FKOf(author)},
		{Title: "Part 2", Slug: "part-2", Author: orm.FKOf(author)},
	}
	Post.Objects.BulkCreate(ctx, posts)?
	return posts, nil
}
```
```go [Generated Go]
func series(ctx context.Context, author *Author) ([]*Post, error) {
	posts := []*Post{
		{Title: "Part 1", Slug: "part-1", Author: orm.FKOf(author)},
		{Title: "Part 2", Slug: "part-2", Author: orm.FKOf(author)},
	}
	if err := orm.Objects[Post]().BulkCreate(ctx, posts); err != nil {
		return nil, err
	}
	return posts, nil
}
```
:::

A many-to-many link needs both rows saved first; see [Adding and Removing
Related Rows](#adding-related-rows).

## Saving Changes {#saving}

`Save` on a row read from the database updates it: every column, from the
values the struct holds. `SaveFields` updates only the columns you name, so a
concurrent change to another column survives:

::: code-group
```vuka [Vuka]
func retitle(ctx context.Context, slug, title string) error {
	p := Post.Objects.Get(ctx, Post.Slug.Eq(slug))?
	p.Title = title
	p.Save(ctx)? // UPDATE posts SET title = ?, slug = ?, … WHERE id = ?

	p.Views++
	p.SaveFields(ctx, Post.Views)? // UPDATE posts SET views = ? WHERE id = ?
	return p.Refresh(ctx)          // read the row again
}
```
```go [Generated Go]
func retitle(ctx context.Context, slug, title string) error {
	p, err := orm.Objects[Post]().Get(ctx, PostColumns.Slug.Eq(slug))
	if err != nil {
		return err
	}
	p.Title = title
	if err := p.Save(ctx); err != nil {
		return err
	} // UPDATE posts SET title = ?, slug = ?, … WHERE id = ?

	p.Views++
	if err := p.SaveFields(ctx, PostColumns.Views); err != nil {
		return err
	} // UPDATE posts SET views = ? WHERE id = ?
	return p.Refresh(ctx) // read the row again
}
```
:::

Changing a foreign key is assigning it and saving: `p.Author =
orm.FKOf(&ada)`. To change a value from what the database holds rather than
what you read, such as a counter, update with an expression ([Updating With
Expressions](#updating-with-expressions)).

## Retrieving Rows {#reading}

### All Rows

`All` reads every row the query matches:

::: code-group
```vuka [Vuka]
func allPosts(ctx context.Context) ([]Post, error) {
	return Post.Objects.All(ctx) // SELECT … FROM posts
}
```
```go [Generated Go]
func allPosts(ctx context.Context) ([]Post, error) {
	return orm.Objects[Post]().All(ctx) // SELECT … FROM posts
}
```
:::

### Filtering {#conditions}

`Filter` keeps the rows matching its conditions, `Exclude` drops them.
Conditions come from the field references:

::: code-group
```vuka [Vuka]
var (
	published = Post.Objects.Filter(Post.Status.Eq("published"))
	notDrafts = Post.Objects.Exclude(Post.Status.Eq("draft"))
	popularGo = Post.Objects.Filter(Post.Title.Contains("Go"), Post.Views.Gte(100))
)
```
```go [Generated Go]
var (
	published = orm.Objects[Post]().Filter(PostColumns.Status.Eq("published"))
	notDrafts = orm.Objects[Post]().Exclude(PostColumns.Status.Eq("draft"))
	popularGo = orm.Objects[Post]().Filter(PostColumns.Title.Contains("Go"), PostColumns.Views.Gte(100))
)
```
:::

```sql
SELECT … FROM posts WHERE status = ?
SELECT … FROM posts WHERE NOT (status = ?)
SELECT … FROM posts WHERE (title LIKE ? ESCAPE '!' AND views >= ?)   -- '%Go%', 100
```

Several conditions in one `Filter` are and-ed. Values are always bound as
parameters, never written into the SQL. Every condition can also be written
as an engine string, `orm.Q{"title__contains": "Go"}`, which is what the
field references produce.

### Chaining Filters {#chaining}

Every method returns a new query and leaves the one it was called on as it
was, so you can keep a query in a variable and build on it:

::: code-group
```vuka [Vuka]
func recentCount(ctx context.Context) (int64, error) {
	shown := Post.Objects.Filter(Post.Status.Eq("published"))
	recent := shown.Filter(Post.Published.Gte(time.Now().AddDate(0, 0, -7)))
	_ = shown.Filter(Post.Views.Gte(1000)) // shown itself is unchanged
	return recent.Count(ctx)              // the query runs here
}
```
```go [Generated Go]
func recentCount(ctx context.Context) (int64, error) {
	shown := orm.Objects[Post]().Filter(PostColumns.Status.Eq("published"))
	recent := shown.Filter(PostColumns.Published.Gte(time.Now().AddDate(0, 0, -7)))
	_ = shown.Filter(PostColumns.Views.Gte(1000)) // shown itself is unchanged
	return recent.Count(ctx)                      // the query runs here
}
```
:::

Building a query never touches the database. It runs when a terminal takes a
context, and each terminal runs it again — a query holds no rows between
calls:

| Terminal | Runs |
|---|---|
| `All(ctx)` | `([]T, error)`: every row |
| `Get(ctx, conds…)` | `(T, error)`: the one row matching |
| `First(ctx)` | `(T, error)`: the first row in order |
| `Iter(ctx)` | `iter.Seq2[T, error]`: rows one at a time, without holding them all |
| `Count(ctx)`, `Exists(ctx)` | `(int64, error)`, `(bool, error)` |
| `Aggregate(ctx, aggs…)` | one row of aggregates |
| `Values[R](names…).All(ctx)` | the columns you name |
| `Update(ctx, set)`, `Delete(ctx)` | a write to every matching row |
| `SQL(ctx)` | runs nothing: the SQL and arguments the query would send |

### One Row: Get and First {#handling-missing-rows}

`Get` is the one row matching its conditions. It fails with `orm.ErrNotFound`
when there is none and `orm.ErrMultipleFound` when there are several, so use
it for a key or a unique column. `First` is the first row in the query's
order (the primary key's when it has none) and fails only with
`orm.ErrNotFound`. A `match` tells the outcomes apart:

::: code-group
```vuka [Vuka]
func postTitle(ctx context.Context, slug string) string {
	match Post.Objects.Get(ctx, Post.Slug.Eq(slug)) {
	case Ok(p):
		return p.Title
	case Err(orm.ErrNotFound):
		return "no such post"
	case Err(orm.ErrMultipleFound):
		return "slug is not unique"
	case Err(e):
		return "error: " + e.Error()
	}
}

func latest(ctx context.Context) (Option[Post], error) {
	return orm.Optional(Post.Objects.OrderBy(Post.Published.Desc()).First(ctx))
}
```
```go [Generated Go]
func postTitle(ctx context.Context, slug string) string {
	switch get, err := orm.Objects[Post]().Get(ctx, PostColumns.Slug.Eq(slug)); {
	case err == nil:
		p := get
		return p.Title
	case errors.Is(err, orm.ErrNotFound):
		return "no such post"
	case errors.Is(err, orm.ErrMultipleFound):
		return "slug is not unique"
	case err != nil:
		e := err
		return "error: " + e.Error()
	default:
		panic("vuka: no case matched")
	}
}

func latest(ctx context.Context) (vuka.Option[Post], error) {
	return orm.Optional(orm.Objects[Post]().OrderBy(PostColumns.Published.Desc()).First(ctx))
}
```
:::

`Err(orm.ErrNotFound)` is tested with `errors.Is`, so it matches an error
wrapped on the way. `orm.Optional` turns a missing row into `None`, leaving
other errors as errors.

### Limiting and Offset {#limit}

`Limit` and `Offset` cut the result, as `LIMIT` and `OFFSET`:

::: code-group
```vuka [Vuka]
var (
	top5  = Post.Objects.OrderBy(Post.Views.Desc()).Limit(5)
	next5 = Post.Objects.OrderBy(Post.Views.Desc()).Limit(5).Offset(5)
)
```
```go [Generated Go]
var (
	top5  = orm.Objects[Post]().OrderBy(PostColumns.Views.Desc()).Limit(5)
	next5 = orm.Objects[Post]().OrderBy(PostColumns.Views.Desc()).Limit(5).Offset(5)
)
```
:::

```sql
SELECT … FROM posts ORDER BY views DESC LIMIT 5
SELECT … FROM posts ORDER BY views DESC LIMIT 5 OFFSET 5
```

Give a limited query an order, or the database picks which rows. A limited
query can't be updated or deleted; filter by key instead. For pages, see
[Paginating Results](#pagination).

### Reading Row by Row {#iterating}

`Iter` reads rows as the database sends them, for a result too large to hold:

::: code-group
```vuka [Vuka]
func printTitles(ctx context.Context) error {
	for p, err := range Post.Objects.Filter(Post.Views.Gte(50)).Iter(ctx) {
		if err != nil {
			return err
		}
		fmt.Println(p.Title)
	}
	return nil
}
```
```go [Generated Go]
func printTitles(ctx context.Context) error {
	for p, err := range orm.Objects[Post]().Filter(PostColumns.Views.Gte(50)).Iter(ctx) {
		if err != nil {
			return err
		}
		fmt.Println(p.Title)
	}
	return nil
}
```
:::

## Field Lookups {#lookups}

A lookup is how a condition compares a column. Each field has the lookups its
type allows; the engine name is what `orm.Q` keys end in (`"views__gte"`):

| Field reference | Engine name | SQL |
|---|---|---|
| `Post.Status.Eq("draft")` | `exact` (or the bare name) | `status = ?` |
| `Post.Status.Ne("draft")` | | `NOT (status = ?)` |
| `Post.Title.IExact("go")` | `iexact` | `LOWER(title) = LOWER(?)` |
| `Post.Title.Contains("Go")`, `IContains` | `contains`, `icontains` | `title LIKE '%Go%'`; case-insensitive |
| `Post.Title.StartsWith("Go")`, `IStartsWith` | `startswith`, `istartswith` | `title LIKE 'Go%'` |
| `Post.Title.EndsWith("!")`, `IEndsWith` | `endswith`, `iendswith` | `title LIKE '%!'` |
| `Post.Views.Gt(10)`, `Gte`, `Lt`, `Lte` | `gt`, `gte`, `lt`, `lte` | `views > ?` … |
| `Post.ID.In(1, 2, 3)` | `in` | `id IN (?, ?, ?)` |
| `Post.Views.Range(10, 50)`, `Between` | `range` | `views BETWEEN ? AND ?` (both ends included) |
| `Post.Published.IsNull(true)` | `isnull` | `published IS NULL` |
| `Post.ID.InQuery(q)` | `in` with a query | `id IN (SELECT …)` |
| [Search](/orm/search) | `search`, `trigram_similar` | full-text and trigram search |

Text lookups escape `%` and `_` in the value, so `Contains("50%")` finds the
text `50%`. The case-insensitive ones lower both sides.

### Dates and Transforms {#transforms}

A transform turns the column into something else before the lookup compares
it. In `orm.Q` it goes between the field and the lookup; as an expression it
is a function of the column:

| Transform | `orm.Q` | Expression |
|---|---|---|
| year, month, day (numbers) | `"published__year": 2026` | `orm.Year(Post.Published.Expr()).Eq(2026)` |
| the date of a timestamp | `"published__date": "2026-10-29"` | `orm.Date(Post.Published.Expr())` |
| lower, upper, trim | `"title__lower__startswith": "go"` | `orm.Lower(Post.Title)` |
| length | `"title__length__gt": 80` | `orm.Length(Post.Title.Expr()).Gt(80)` |
| abs | `"views__abs__lt": 10` | `orm.Abs(Post.Views.Expr())` |

A time field also compares with dates the database computes:
`Post.Published.Gte(orm.Today().AddDays(-7))` is the last week by the
database's clock. See [Portable Dates](/orm/expressions#portable-dates).

::: code-group
```vuka [Vuka]
var (
	in2026   = Post.Objects.Filter(orm.Q{"published__year": 2026})
	october  = Post.Objects.Filter(orm.Year(Post.Published.Expr()).Eq(2026), orm.Month(Post.Published.Expr()).Eq(10))
	lastWeek = Post.Objects.Filter(Post.Published.Gte(orm.Today().AddDays(-7)))
)
```
```go [Generated Go]
var (
	in2026   = orm.Objects[Post]().Filter(orm.Q{"published__year": 2026})
	october  = orm.Objects[Post]().Filter(orm.Year(PostColumns.Published.Expr()).Eq(2026), orm.Month(PostColumns.Published.Expr()).Eq(10))
	lastWeek = orm.Objects[Post]().Filter(PostColumns.Published.Gte(orm.Today().AddDays(-7)))
)
```
:::

```sql
-- SQLite; Postgres and MySQL use EXTRACT and YEAR()
SELECT … FROM posts WHERE CAST(strftime('%Y', published) AS INTEGER) = ?
SELECT … FROM posts WHERE published >= date(date('now'), '-7 day')
```

`orm.RegisterLookup` and `.Transform()` on a function add lookups of your own
([Custom Lookups](/orm/expressions#custom-lookups)).

## Lookups That Span Relationships {#spanning-relationships}

A field reference goes on through a relation, and the condition reaches the
related table: `Post.Author.Name`, `Comment.Post.Author.Name`. In Go each
relation hop is a method, `PostColumns.Author().Name`.

### Following a Foreign Key {#forward}

A condition on the model a foreign key points at joins it:

::: code-group
```vuka [Vuka]
var (
	byAda       = Post.Objects.Filter(Post.Author.Name.Eq("Ada"))
	onAdasPosts = Comment.Objects.Filter(Comment.Post.Author.Name.Eq("Ada"))
)
```
```go [Generated Go]
var (
	byAda       = orm.Objects[Post]().Filter(PostColumns.Author().Name.Eq("Ada"))
	onAdasPosts = orm.Objects[Comment]().Filter(CommentColumns.Post().Author().Name.Eq("Ada"))
)
```
:::

```sql
SELECT posts.* FROM posts
LEFT JOIN authors AS author ON author.id = posts.author_id
WHERE author.name = ?

SELECT comments.* FROM comments
LEFT JOIN posts AS post ON post.id = comments.post_id
LEFT JOIN authors AS post__author ON post__author.id = post.author_id
WHERE post__author.name = ?
```

To match a key you already have, compare the foreign key column itself,
which needs no join: `orm.Q{"author": ada.ID}` is `WHERE author_id = ?`.

### Following a Relation Backward {#following-relationships-backward}

A foreign key with `Related: "posts"` gives the model it points at a path
back: `Author.Posts` is an author's posts, and a condition on it keeps the
authors who have such a post. Because an author may have many posts, the
condition is an `EXISTS`, and each author comes back once:

::: code-group
```vuka [Vuka]
var (
	publishing = Author.Objects.Filter(Author.Posts.Status.Eq("published"))
	discussed  = Post.Objects.Filter(Post.Comments.Body.IContains("thanks"))
	mathsy     = Author.Objects.Filter(Author.Profile.Bio.Contains("maths"))
)
```
```go [Generated Go]
var (
	publishing = orm.Objects[Author]().Filter(AuthorColumns.Posts().Status.Eq("published"))
	discussed  = orm.Objects[Post]().Filter(PostColumns.Comments().Body.IContains("thanks"))
	mathsy     = orm.Objects[Author]().Filter(AuthorColumns.Profile().Bio.Contains("maths"))
)
```
:::

```sql
SELECT authors.* FROM authors
WHERE EXISTS (SELECT 1 FROM posts AS p
              WHERE p.author_id = authors.id AND p.status = ?)
```

A one-to-one — a foreign key that is also unique, like `Profile.Author` — is
followed backward with a join, since there is at most one row:
`Author.Profile.Bio` is `LEFT JOIN profiles AS profile ON profile.author_id =
authors.id`.

Paths go on as far as you like, forward and back:
`Post.Author.Posts.Comments.Name` is "posts by an author who has a comment
from this name on any of their posts".

### Many-to-Many, Both Ways {#many-to-many}

A many-to-many is followed the same way from either side. The side that
declares it uses the field, `Post.Tags`; the other uses the name in
`Related`. A field reference names a model's fields, and `Tag` has no field
for its posts, so that side is written with the generated path,
`TagColumns.Posts()` — as any relation back with no field to hold it is:

::: code-group
```vuka [Vuka]
var (
	goPosts  = Post.Objects.Filter(Post.Tags.Name.Eq("go"))
	adasTags = Tag.Objects.Filter(TagColumns.Posts().Author().Name.Eq("Ada"))
)
```
```go [Generated Go]
var (
	goPosts  = orm.Objects[Post]().Filter(PostColumns.Tags().Name.Eq("go"))
	adasTags = orm.Objects[Tag]().Filter(TagColumns.Posts().Author().Name.Eq("Ada"))
)
```
:::

```sql
SELECT posts.* FROM posts
WHERE EXISTS (SELECT 1 FROM posts_tags AS t
              JOIN tags ON tags.id = t.tag_id
              WHERE t.post_id = posts.id AND tags.name = ?)
```

### Conditions on Several Related Rows {#multi-valued}

When conditions name a relation holding many rows, each condition is tested
on its own: it asks whether *some* related row matches it. So this query
reads "authors with a published post, and with a post over 100 views" — which
may be two different posts:

::: code-group
```vuka [Vuka]
var either = Author.Objects.Filter(Author.Posts.Status.Eq("published"), Author.Posts.Views.Gt(100))
```
```go [Generated Go]
var either = orm.Objects[Author]().Filter(AuthorColumns.Posts().Status.Eq("published"), AuthorColumns.Posts().Views.Gt(100))
```
:::

```sql
WHERE EXISTS (SELECT 1 FROM posts AS p1 WHERE p1.author_id = authors.id AND p1.status = ?)
  AND EXISTS (SELECT 1 FROM posts AS p2 WHERE p2.author_id = authors.id AND p2.views > ?)
```

Calling `Filter` twice, or wrapping the two in `orm.And`, means the same. To
require one post that meets both, put the conditions on the relation with
`Where` and ask whether it has such a row:

::: code-group
```vuka [Vuka]
var sameCondition = Author.Posts.Where(Author.Posts.Status.Eq("published"), Author.Posts.Views.Gt(100))

var bothOnOnePost = Author.Objects.Filter(orm.Typed[bool](sameCondition.Exists()).Eq(true))
```
```go [Generated Go]
var sameCondition = AuthorColumns.Posts().Where(AuthorColumns.Posts().Status.Eq("published"), AuthorColumns.Posts().Views.Gt(100))

var bothOnOnePost = orm.Objects[Author]().Filter(orm.Typed[bool](sameCondition.Exists()).Eq(true))
```
:::

```sql
WHERE EXISTS (SELECT 1 FROM authors AS a2
              LEFT JOIN posts ON posts.author_id = a2.id
              WHERE a2.id = authors.id
                AND (posts.status = ? AND posts.views > ?)
                AND posts.id IS NOT NULL) = ?   -- true
```

`orm.Exists` with a query of your own says the same, written as the
subquery: see [Subqueries](#subqueries).

`Exclude` with a condition on many rows drops the parents having *any* such
row: `Exclude(Author.Posts.Status.Eq("draft"))` is the authors with no draft.

### Rows With No Related Rows {#no-related-rows}

To find authors who have written nothing, exclude those with a post:

::: code-group
```vuka [Vuka]
var silent = Author.Objects.Exclude(Author.Posts.ID.IsNull(false))
```
```go [Generated Go]
var silent = orm.Objects[Author]().Exclude(AuthorColumns.Posts().ID.IsNull(false))
```
:::

```sql
SELECT authors.* FROM authors
WHERE NOT (EXISTS (SELECT 1 FROM posts AS p
                   WHERE p.author_id = authors.id AND p.id IS NOT NULL))
```

> [!WARNING]
> `Filter(Author.Posts.ID.IsNull(true))` asks for an author with a post whose
> key is NULL, which no post has, so it matches nothing. Write the question as
> an `Exclude`, or count the posts and filter on the count
> ([Annotating Rows](#annotate)).

## Comparing Fields With Each Other {#comparing-fields}

A condition can compare a column with another column of the same row, or of
a related row, instead of a value. Give the lookup the other field's
expression:

::: code-group
```vuka [Vuka]
var (
	liked     = Post.Objects.Filter(orm.Q{"likes__gt": Post.Views.Expr()})
	halfLiked = Post.Objects.Filter(orm.Q{"likes__gte": Post.Views.Div(2)})
	early     = Comment.Objects.Filter(orm.Q{"created__lt": Comment.Post.Published.Expr()})
)
```
```go [Generated Go]
var (
	liked     = orm.Objects[Post]().Filter(orm.Q{"likes__gt": PostColumns.Views.Expr()})
	halfLiked = orm.Objects[Post]().Filter(orm.Q{"likes__gte": PostColumns.Views.Div(2)})
	early     = orm.Objects[Comment]().Filter(orm.Q{"created__lt": CommentColumns.Post().Published.Expr()})
)
```
:::

```sql
SELECT … FROM posts WHERE likes > views
SELECT … FROM posts WHERE likes >= (views / ?)
SELECT comments.* FROM comments
LEFT JOIN posts AS post ON post.id = comments.post_id
WHERE comments.created < post.published
```

A number field's `Add`, `Sub`, `Mul` and `Div` are typed expressions, so
`Post.Views.Add("x")` doesn't compile. `orm.Where(expr, lookup, value)`
compares any [expression](/orm/expressions).

## Complex Conditions {#combining-conditions}

`orm.Or`, `orm.And` and `orm.Not` combine conditions into one, as deep as
you need:

::: code-group
```vuka [Vuka]
var hotOrTaggedGo = Post.Objects.Filter(orm.Or(
	Post.Views.Gt(100),
	orm.And(Post.Tags.Name.Eq("go"), orm.Not(Post.Published.IsNull(true))),
))
```
```go [Generated Go]
var hotOrTaggedGo = orm.Objects[Post]().Filter(orm.Or(
	PostColumns.Views.Gt(100),
	orm.And(PostColumns.Tags().Name.Eq("go"), orm.Not(PostColumns.Published.IsNull(true))),
))
```
:::

```sql
WHERE (views > ?
       OR (EXISTS (SELECT 1 FROM posts_tags … WHERE … tags.name = ?)
           AND NOT (published IS NULL)))
```

An `orm.Q` map is a condition too, its keys and-ed:
`orm.Or(orm.Q{"status": "draft"}, orm.Q{"views__lt": 5})`.

### Named Conditions {#model-conditions}

A condition you use in many places belongs on the model's field references,
as a method of the generated `PostFields` type. It then works at the root and
at the end of any relation:

::: code-group
```vuka [Vuka]
func (p PostFields) Popular() orm.Cond {
	return orm.Or(p.Views.Gte(1000), p.Likes.Gte(100))
}

var (
	popular        = Post.Objects.Filter(PostColumns.Popular())
	popularAuthors = Author.Objects.Filter(AuthorColumns.Posts().Popular())
)
```
```go [Generated Go]
func (p PostFields) Popular() orm.Cond {
	return orm.Or(p.Views.Gte(1000), p.Likes.Gte(100))
}

var (
	popular        = orm.Objects[Post]().Filter(PostColumns.Popular())
	popularAuthors = orm.Objects[Author]().Filter(AuthorColumns.Posts().Popular())
)
```
:::

`PostColumns` is the generated root value; `PostFields` is its type, which
every path ending at a post shares. A named condition is called on them by
their Go names.

## Ordering and Distinct {#ordering}

`OrderBy` takes fields' `Asc()` and `Desc()`, on the model or across a
foreign key; `Order` takes [expressions](/orm/expressions) and annotations:

::: code-group
```vuka [Vuka]
var (
	byViews       = Post.Objects.OrderBy(Post.Views.Desc(), Post.ID.Asc())
	byAuthor      = Post.Objects.OrderBy(Post.Author.Name.Asc(), Post.Published.Desc())
	byTitleLength = Post.Objects.Order(orm.Length(Post.Title.Expr()).Desc())
	statuses      = Post.Objects.Values[string]("status").Distinct()
)
```
```go [Generated Go]
var (
	byViews       = orm.Objects[Post]().OrderBy(PostColumns.Views.Desc(), PostColumns.ID.Asc())
	byAuthor      = orm.Objects[Post]().OrderBy(PostColumns.Author().Name.Asc(), PostColumns.Published.Desc())
	byTitleLength = orm.Objects[Post]().Order(orm.Length(PostColumns.Title.Expr()).Desc())
	statuses      = orm.Objects[Post]().Values[string]("status").Distinct()
)
```
:::

```sql
SELECT … FROM posts ORDER BY views DESC, id ASC
SELECT posts.* FROM posts
LEFT JOIN authors AS author ON author.id = posts.author_id
ORDER BY author.name ASC, posts.published DESC
SELECT DISTINCT status FROM posts
```

A query with no order of its own uses the model's `Ordering`
([Model Options](/orm/models#meta)); `OrderBy()` with nothing clears both.
`Distinct()` drops duplicate rows, which matters most when you select
columns.

## Counting and Existence {#counting}

`Count` runs `SELECT COUNT(*)`, and `Exists` asks whether there is any row,
without reading one. With `?` either goes straight into a condition:

::: code-group
```vuka [Vuka]
func publish(ctx context.Context, slug string) error {
	p := Post.Objects.Get(ctx, Post.Slug.Eq(slug))?("loading post %s", slug)
	if Post.Objects.Filter(Post.Title.Eq(p.Title), Post.Status.Eq("published")).Exists(ctx)? {
		return ErrDuplicateTitle
	}
	p.Status = "published"
	return p.Save(ctx)
}
```
```go [Generated Go]
func publish(ctx context.Context, slug string) error {
	p, err := orm.Objects[Post]().Get(ctx, PostColumns.Slug.Eq(slug))
	if err != nil {
		return fmt.Errorf("loading post %s: %w", slug, err)
	}
	exists, err := orm.Objects[Post]().Filter(PostColumns.Title.Eq(p.Title), PostColumns.Status.Eq("published")).Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return ErrDuplicateTitle
	}
	p.Status = "published"
	return p.Save(ctx)
}
```
:::

`Exists` answers `false` with a nil error when there is no row; it never
fails with `orm.ErrNotFound`. Use it, not `Count(ctx) > 0` and not a `Get`
whose error you ignore, when the question is yes or no. `?("loading post
%s", slug)` adds context to the error it returns, which stays
`errors.Is(err, orm.ErrNotFound)` through the wrapping ([The ?
Operator](/features/try)).

## Aggregation and Annotation {#values-and-aggregates}

### Over the Whole Query {#aggregate}

`Aggregate` computes values over every matching row and returns them by name:

::: code-group
```vuka [Vuka]
func totals(ctx context.Context) (posts, authors, views int64, err error) {
	r := Post.Objects.Filter(Post.Status.Eq("published")).Aggregate(ctx,
		orm.Count("id").As("posts"),
		orm.Count("author").Distinct().As("authors"),
		orm.Sum("views").As("views"))?
	return r.Int("posts"), r.Int("authors"), r.Int("views"), nil
}
```
```go [Generated Go]
func totals(ctx context.Context) (posts, authors, views int64, err error) {
	r, e1 := orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).Aggregate(ctx,
		orm.Count("id").As("posts"),
		orm.Count("author").Distinct().As("authors"),
		orm.Sum("views").As("views"))
	if e1 != nil {
		return 0, 0, 0, e1
	}
	return r.Int("posts"), r.Int("authors"), r.Int("views"), nil
}
```
:::

```sql
SELECT COUNT(id) AS posts, COUNT(DISTINCT author_id) AS authors, SUM(views) AS views
FROM posts WHERE status = ?
```

The aggregates are `orm.Count`, `orm.Sum`, `orm.Avg`, `orm.Min` and
`orm.Max`, each with `.Distinct()`, `.Filter(cond)` and `.As(name)`.

### Annotating Rows {#annotate}

`Annotate` adds a computed value to each row. Over a relation holding many
rows it is an aggregate per row: here, each author's number of posts.
`orm.Annotation[T]` names the value and gives it typed comparisons, so you
can filter and order on it:

::: code-group
```vuka [Vuka]
var postCount = orm.Annotation[int64]("posts", Author.Posts.Count())

var prolific = Author.Objects.Annotate(postCount).Filter(postCount.Gte(10)).Order(postCount.Desc())
```
```go [Generated Go]
var postCount = orm.Annotation[int64]("posts", AuthorColumns.Posts().Count())

var prolific = orm.Objects[Author]().Annotate(postCount).Filter(postCount.Gte(10)).Order(postCount.Desc())
```
:::

```sql
SELECT authors.* FROM authors
LEFT JOIN posts ON posts.author_id = authors.id
GROUP BY authors.id, authors.name, authors.email
HAVING (COUNT(DISTINCT posts.id)) >= ?
ORDER BY (COUNT(DISTINCT posts.id)) DESC
```

A filter on an aggregate becomes `HAVING`; a filter on a plain column stays
in `WHERE`. To read the value, select it with [`Values`](#values);
`orm.Ref[int64]("posts")` refers to an annotation added earlier by name.

### Aggregating Related Rows {#related-counts-and-model-conditions}

A relation path aggregates the related rows of each parent: `Count()`,
`Exists()`, and `Sum`, `Avg`, `Min` and `Max` of one of their fields.
`Where` narrows the rows the aggregate sees, without dropping parents that
have none — those get 0 or NULL. This reads every author with their
published posts' count and latest date, newest first:

::: code-group
```vuka [Vuka]
type AuthorStats struct {
	Name      string
	Published int64
	Latest    Option[time.Time]
}

func authorStats(ctx context.Context) ([]AuthorStats, error) {
	shown := Author.Posts.Where(Author.Posts.Status.Eq("published"))
	latest := orm.Annotation[time.Time]("latest", shown.Max(Author.Posts.Published))
	return Author.Objects.
		Annotate("published", shown.Count()).
		Annotate(latest).
		Order(latest.Desc()).
		Values[AuthorStats]("name", "published", "latest").All(ctx)
}
```
```go [Generated Go]
type AuthorStats struct {
	Name      string
	Published int64
	Latest    vuka.Option[time.Time]
}

func authorStats(ctx context.Context) ([]AuthorStats, error) {
	shown := AuthorColumns.Posts().Where(AuthorColumns.Posts().Status.Eq("published"))
	latest := orm.Annotation[time.Time]("latest", shown.Max(AuthorColumns.Posts().Published))
	return orm.Objects[Author]().
		Annotate("published", shown.Count()).
		Annotate(latest).
		Order(latest.Desc()).
		Values[AuthorStats]("name", "published", "latest").All(ctx)
}
```
:::

`Count` counts distinct related rows, so joining two relations never
multiplies one by the other; `CountRows` counts joined rows as they are.
With two or more relation aggregates each gets its own subquery. On Postgres
a narrowed count is `COUNT(DISTINCT posts.id) FILTER (WHERE …)`, on MySQL
and SQLite `COUNT(DISTINCT CASE WHEN … THEN posts.id END)`.

### Grouping {#grouping}

`Values` with an aggregate groups by the other columns it selects, as SQL's
`GROUP BY` does. `GroupBy(names…)` groups explicitly, and `Having(conds…)`
filters the groups:

::: code-group
```vuka [Vuka]
type StatusCount struct {
	Status string
	N      int64
}

func postsPerStatus(ctx context.Context) ([]StatusCount, error) {
	return Post.Objects.Annotate("n", orm.Count("id")).
		OrderBy(Post.Status.Asc()).Values[StatusCount]("status", "n").All(ctx)
}

func authorsWithSeveral(ctx context.Context) ([]int64, error) {
	return Post.Objects.Annotate("n", orm.Count("id")).GroupBy(Post.Author.String()).
		Having(orm.Q{"n__gt": 1}).Values[int64]("author").All(ctx)
}
```
```go [Generated Go]
type StatusCount struct {
	Status string
	N      int64
}

func postsPerStatus(ctx context.Context) ([]StatusCount, error) {
	return orm.Objects[Post]().Annotate("n", orm.Count("id")).
		OrderBy(PostColumns.Status.Asc()).Values[StatusCount]("status", "n").All(ctx)
}

func authorsWithSeveral(ctx context.Context) ([]int64, error) {
	return orm.Objects[Post]().Annotate("n", orm.Count("id")).GroupBy(PostColumns.Author().String()).
		Having(orm.Q{"n__gt": 1}).Values[int64]("author").All(ctx)
}
```
:::

```sql
SELECT status, (COUNT(id)) AS n FROM posts GROUP BY 1 ORDER BY status ASC
SELECT author_id FROM posts GROUP BY 1 HAVING (COUNT(id)) > ?
```

### Filtered Aggregates {#filtered-aggregates}

An aggregate's `Filter` counts only the rows matching its condition, so one
query can count several things:

::: code-group
```vuka [Vuka]
type Activity struct {
	Author    string `orm:"author__name"`
	Posts     int64
	Published int64
}

func activity(ctx context.Context) ([]Activity, error) {
	return Post.Objects.Annotate("posts", orm.Count("id")).
		Annotate("published", orm.Count("id").Filter(Post.Status.Eq("published"))).
		Values[Activity]("author__name", "posts", "published").All(ctx)
}
```
```go [Generated Go]
type Activity struct {
	Author    string `orm:"author__name"`
	Posts     int64
	Published int64
}

func activity(ctx context.Context) ([]Activity, error) {
	return orm.Objects[Post]().Annotate("posts", orm.Count("id")).
		Annotate("published", orm.Count("id").Filter(PostColumns.Status.Eq("published"))).
		Values[Activity]("author__name", "posts", "published").All(ctx)
}
```
:::

::: code-group
```sql [Postgres / SQLite]
SELECT author.name, COUNT(posts.id) AS posts,
       COUNT(posts.id) FILTER (WHERE posts.status = ?) AS published
FROM posts LEFT JOIN authors AS author ON author.id = posts.author_id
GROUP BY 1
```
```sql [MySQL]
SELECT author.name, COUNT(posts.id) AS posts,
       COUNT(CASE WHEN posts.status = ? THEN posts.id END) AS published
FROM posts LEFT JOIN authors AS author ON author.id = posts.author_id
GROUP BY 1
```
:::

## Selecting Columns: Values {#values}

`Values[R](names…)` reads only the columns you name, into `R`:

- a struct, each field matched to a name by its own name in snake_case, or by
  an `orm:"author__name"` tag for a path;
- `map[string]any` or `[]any`, one per row;
- a scalar such as `string` or `int64`, for one name.

Names are columns, paths across relations, and annotations:

::: code-group
```vuka [Vuka]
type PostLine struct {
	Title  string
	Author string `orm:"author__name"`
	Views  int
}

func postLines(ctx context.Context) ([]PostLine, error) {
	return Post.Objects.OrderBy(Post.Views.Desc()).Values[PostLine]("title", "author__name", "views").All(ctx)
}

func draftIDs(ctx context.Context) ([]int64, error) {
	return Post.Objects.Filter(Post.Status.Eq("draft")).Values[int64]("id").All(ctx)
}
```
```go [Generated Go]
type PostLine struct {
	Title  string
	Author string `orm:"author__name"`
	Views  int
}

func postLines(ctx context.Context) ([]PostLine, error) {
	return orm.Objects[Post]().OrderBy(PostColumns.Views.Desc()).Values[PostLine]("title", "author__name", "views").All(ctx)
}

func draftIDs(ctx context.Context) ([]int64, error) {
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq("draft")).Values[int64]("id").All(ctx)
}
```
:::

```sql
SELECT posts.title, author.name, posts.views FROM posts
LEFT JOIN authors AS author ON author.id = posts.author_id
ORDER BY posts.views DESC
```

The check goes both ways: a name with no field to hold it, or a struct field
with no name selected, is an error naming it. `Values` queries take
`Filter`, `Exclude`, `OrderBy`, `Limit`, `Distinct` and `Iter` like any
other.

## Updating and Deleting Many Rows {#writing}

### Updating {#updating}

`Update` changes every row the query matches in one statement and returns
how many it changed. `orm.Set` maps columns to new values:

::: code-group
```vuka [Vuka]
func archiveQuietDrafts(ctx context.Context) (int64, error) {
	return Post.Objects.Filter(Post.Status.Eq("draft"), Post.Views.Lt(5)).
		Update(ctx, orm.Set{"status": "archived"})
}
```
```go [Generated Go]
func archiveQuietDrafts(ctx context.Context) (int64, error) {
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq("draft"), PostColumns.Views.Lt(5)).
		Update(ctx, orm.Set{"status": "archived"})
}
```
:::

```sql
UPDATE posts SET status = ? WHERE status = ? AND views < ?
```

A query with no condition refuses to update or delete, so a forgotten
`Filter` can't reach every row: `Unfiltered()` says you mean all of them,
`Post.Objects.Unfiltered().Update(ctx, orm.Set{"views": 0})`. `Update`
doesn't call the rows' `Save` hooks.

### Updating With Expressions {#updating-with-expressions}

A value in `orm.Set` may be an expression of the row's own columns, which
the database computes — so a counter goes up by one however many requests
do it at once:

::: code-group
```vuka [Vuka]
func countView(ctx context.Context, slug string) error {
	Post.Objects.Filter(Post.Slug.Eq(slug)).Update(ctx, orm.Set{"views": Post.Views.Add(1)})?
	return nil
}

func rate(ctx context.Context) (int64, error) {
	heat := orm.Case(orm.When(Post.Views.Gte(1000), "hot"), orm.When(Post.Views.Gte(100), "warm")).Else("cold")
	return Post.Objects.Filter(Post.Status.Eq("published")).Update(ctx, orm.Set{"status": heat})
}
```
```go [Generated Go]
func countView(ctx context.Context, slug string) error {
	if _, err := orm.Objects[Post]().Filter(PostColumns.Slug.Eq(slug)).Update(ctx, orm.Set{"views": PostColumns.Views.Add(1)}); err != nil {
		return err
	}
	return nil
}

func rate(ctx context.Context) (int64, error) {
	heat := orm.Case(orm.When(PostColumns.Views.Gte(1000), "hot"), orm.When(PostColumns.Views.Gte(100), "warm")).Else("cold")
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).Update(ctx, orm.Set{"status": heat})
}
```
:::

```sql
UPDATE posts SET views = (views + ?) WHERE slug = ?
UPDATE posts SET status = CASE WHEN views >= ? THEN ? WHEN views >= ? THEN ? ELSE ? END
WHERE status = ?
```

`orm.Case` picks a value by the first condition that holds; see
[Conditional Expressions](/orm/expressions#case-and-switch).

### Deleting {#deleting}

`Delete` removes the matching rows; a row's own `Delete` removes it:

::: code-group
```vuka [Vuka]
func purge(ctx context.Context) (int64, error) {
	return Post.Objects.Filter(Post.Status.Eq("archived"), Post.Views.Lt(5)).Delete(ctx)
}
```
```go [Generated Go]
func purge(ctx context.Context) (int64, error) {
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq("archived"), PostColumns.Views.Lt(5)).Delete(ctx)
}
```
:::

What happens to the rows pointing at a deleted row is the relation's
`OnDelete` ([Relationships](/orm/models#relationships)), enforced by the
database's foreign key: deleting an author deletes their posts, and those
posts' comments, under `orm.Cascade`. SQLite enforces foreign keys only with
`_pragma=foreign_keys(1)` in the connection string.

### Watching Writes {#watching-writes}

`Post.Objects.OnChange(fn)` calls `fn` after each write to the model's
rows, and returns the function that stops it.

## Related Rows {#relations}

### Following a Foreign Key {#following-a-foreign-key}

An `orm.FK` holds the key and, once loaded, the row. `Get` reads the row the
first time and keeps it:

::: code-group
```vuka [Vuka]
func authorName(ctx context.Context, p *Post) (string, error) {
	author := p.Author.Get(ctx)? // read once, then held
	return author.Name, nil
}
```
```go [Generated Go]
func authorName(ctx context.Context, p *Post) (string, error) {
	author, err := p.Author.Get(ctx).Get()
	if err != nil {
		return "", err
	} // read once, then held
	return author.Name, nil
}
```
:::

`p.Author.Related()` is the row held, nil until something loads it, and
`p.Author.ID()` the key.

### Following a Relation Backward {#reverse-relations}

An author's posts are the posts whose foreign key holds the author's key.
Ask for them with a query, which you can filter and order like any other.
To hold them on the row itself, declare a field named after the relation —
`Posts []Post` on `Author` for `Related: "posts"`, `Profile *Profile` for the
one-to-one — and `Load` it:

::: code-group
```vuka [Vuka]
func recentPosts(ctx context.Context, a Author) ([]Post, error) {
	return Post.Objects.Filter(orm.Q{"author": a.ID}).OrderBy(Post.Published.Desc()).Limit(5).All(ctx)
}

func withPosts(ctx context.Context, a *Author) error {
	a.Load(ctx, Author.Posts, Author.Profile)?
	fmt.Println(len(a.Posts), "posts")
	if a.Profile != nil {
		fmt.Println(a.Profile.Bio)
	}
	return nil
}
```
```go [Generated Go]
func recentPosts(ctx context.Context, a Author) ([]Post, error) {
	return orm.Objects[Post]().Filter(orm.Q{"author": a.ID}).OrderBy(PostColumns.Published.Desc()).Limit(5).All(ctx)
}

func withPosts(ctx context.Context, a *Author) error {
	if err := a.Load(ctx, AuthorColumns.Posts(), AuthorColumns.Profile()); err != nil {
		return err
	}
	fmt.Println(len(a.Posts), "posts")
	if a.Profile != nil {
		fmt.Println(a.Profile.Bio)
	}
	return nil
}
```
:::

A reverse relation with no field to hold it is still a path for conditions
and aggregates; `Load` and `PrefetchRelated` on it say which field to
declare.

A many-to-many's rows are on its field, `p.Tags.All(ctx)`, which reads them
unless they were loaded. From the other side, filter the posts:
`Post.Objects.Filter(Post.Tags.ID.Eq(golang.ID))`.

### Adding and Removing Related Rows {#adding-related-rows}

A row joins a foreign-key relation by pointing its key at the other row and
saving — from either side, it is the post's key that changes. A
many-to-many's links change through its field, or through the row by the
relation's name, which works from the other side as well:

::: code-group
```vuka [Vuka]
func comment(ctx context.Context, post *Post, name, body string) (Comment, error) {
	c := Comment{Post: orm.FKOf(post), Name: name, Body: body}
	c.Save(ctx)?
	return c, nil
}

func retag(ctx context.Context) error {
	p.Tags.Add(ctx, &golang, &tools)?
	p.Tags.Remove(ctx, &tools)?
	p.Tags.Set(ctx, &golang)?        // its only links now
	golang.Add(ctx, "posts", &p)?    // the same link, from the tag
	return p.Tags.Clear(ctx)
}
```
```go [Generated Go]
func comment(ctx context.Context, post *Post, name, body string) (Comment, error) {
	c := Comment{Post: orm.FKOf(post), Name: name, Body: body}
	if err := c.Save(ctx); err != nil {
		return Comment{}, err
	}
	return c, nil
}

func retag(ctx context.Context) error {
	if err := p.Tags.Add(ctx, &golang, &tools); err != nil {
		return err
	}
	if err := p.Tags.Remove(ctx, &tools); err != nil {
		return err
	}
	if err := p.Tags.Set(ctx, &golang); err != nil {
		return err
	} // its only links now
	if err := golang.Add(ctx, "posts", &p); err != nil {
		return err
	} // the same link, from the tag
	return p.Tags.Clear(ctx)
}
```
:::

`Add`, `Remove` and `Set` take rows or keys, and only change the table between;
the rows themselves must be saved.

### Avoiding N+1 Queries {#eager-loading}

Reading a relation row by row is one query for the posts and then one more
per post for its author: N+1 queries. Load the relations with the rows
instead:

- `SelectRelated` joins a foreign key (or a one-to-one, either way) into the
  same query, and fills each row's `FK` from the joined columns.
- `PrefetchRelated` reads a relation holding many rows — reverse foreign keys
  and many-to-manys — in one more query for all the rows at once, `IN (…)`
  their keys.

::: code-group
```vuka [Vuka]
func feed(ctx context.Context) error {
	posts := Post.Objects.
		SelectRelated(Post.Author.String()).    // JOIN authors
		PrefetchRelated(Post.Tags, Post.Comments). // one query each, for every post
		All(ctx)?
	for _, p := range posts {
		fmt.Println(p.Author.Related().Name, len(p.Tags.Rows()), len(p.Comments))
	}
	return nil
}
```
```go [Generated Go]
func feed(ctx context.Context) error {
	posts, err := orm.Objects[Post]().
		SelectRelated(PostColumns.Author().String()).                // JOIN authors
		PrefetchRelated(PostColumns.Tags(), PostColumns.Comments()). // one query each, for every post
		All(ctx)
	if err != nil {
		return err
	}
	for _, p := range posts {
		fmt.Println(p.Author.Related().Name, len(p.Tags.Rows()), len(p.Comments))
	}
	return nil
}
```
:::

```sql
SELECT posts.*, author.* FROM posts
LEFT JOIN authors AS author ON author.id = posts.author_id
SELECT … FROM tags JOIN posts_tags … WHERE posts_tags.post_id IN (?, ?, …)
SELECT … FROM comments WHERE post_id IN (?, ?, …)
```

Paths go on through relations, `"posts__comments"` loading each author's
posts and each post's comments. `orm.Prefetch(path, query)` loads only the
rows of a query of yours, filtered and ordered:

::: code-group
```vuka [Vuka]
func authorsWithLivePosts(ctx context.Context) ([]Author, error) {
	shown := Post.Objects.Filter(Post.Status.Eq("published")).OrderBy(Post.Published.Desc())
	return Author.Objects.
		SelectRelated(Author.Profile.String()).
		PrefetchRelated(orm.Prefetch("posts", shown), "posts__comments").
		All(ctx)
}
```
```go [Generated Go]
func authorsWithLivePosts(ctx context.Context) ([]Author, error) {
	shown := orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).OrderBy(PostColumns.Published.Desc())
	return orm.Objects[Author]().
		SelectRelated(AuthorColumns.Profile().String()).
		PrefetchRelated(orm.Prefetch("posts", shown), "posts__comments").
		All(ctx)
}
```
:::

`orm.LoadRelated(ctx, rows, "author", "tags")` does the same for rows read
already (a slice of pointers to them). [`orm.WatchRepeats`](/orm/observability#n-1-queries)
reports the N+1s left.

## Subqueries {#subqueries}

`orm.Exists(query)` is a condition that holds when another query has a row,
and `orm.OuterRef(name)` in that query refers to the outer row. This is the
authors with a published post over 100 views — one post meeting both — and
the authors with no post at all:

::: code-group
```vuka [Vuka]
var (
	theirs = Post.Objects.Filter(orm.Q{"author": orm.OuterRef("id")}, Post.Status.Eq("published"), Post.Views.Gt(100))

	popularAuthors2 = Author.Objects.Filter(orm.Exists(theirs))
	noPosts         = Author.Objects.Filter(orm.Not(orm.Exists(Post.Objects.Filter(orm.Q{"author": orm.OuterRef("id")}))))
)
```
```go [Generated Go]
var (
	theirs = orm.Objects[Post]().Filter(orm.Q{"author": orm.OuterRef("id")}, PostColumns.Status.Eq("published"), PostColumns.Views.Gt(100))

	popularAuthors2 = orm.Objects[Author]().Filter(orm.Exists(theirs))
	noPosts         = orm.Objects[Author]().Filter(orm.Not(orm.Exists(orm.Objects[Post]().Filter(orm.Q{"author": orm.OuterRef("id")}))))
)
```
:::

```sql
SELECT authors.* FROM authors
WHERE EXISTS (SELECT 1 FROM posts AS p
              WHERE p.author_id = authors.id AND p.status = ? AND p.views > ?)
```

`orm.Subquery(values)` is a column of another query's first row, and
`field.InQuery(values)` is `IN (SELECT …)`. See
[Subqueries](/orm/expressions#subqueries) and the
[latest comment of each post](/orm/cookbook#latest-related-row).

## Paginating Results {#pagination}

`orm.Paginate` reads a page and the total, sorting and searching only by the
fields you allow:

::: code-group
```vuka [Vuka]
func postPage(ctx context.Context, r *http.Request) (orm.PageResult[Post], error) {
	return orm.Paginate(ctx, Post.Objects.QuerySet, orm.PageFrom(r.URL.Query()),
		orm.Sortable(Post.Views.Name(), Post.Published.Name()),
		orm.Searchable(Post.Title.Name()),
		orm.DefaultSort(Post.Views.Desc()))
}
```
```go [Generated Go]
func postPage(ctx context.Context, r *http.Request) (orm.PageResult[Post], error) {
	return orm.Paginate(ctx, orm.Objects[Post]().QuerySet, orm.PageFrom(r.URL.Query()),
		orm.Sortable(PostColumns.Views.Name(), PostColumns.Published.Name()),
		orm.Searchable(PostColumns.Title.Name()),
		orm.DefaultSort(PostColumns.Views.Desc()))
}
```
:::

`page.Items`, `page.Total`, `page.Page`, `page.Size` and `page.Pages` describe
the page. `orm.PageFrom` reads `page`, `size`, `sort` (`-views` for
descending) and `q` from query parameters; `orm.PageRequest{Page, Size,
Sort, Search}` is the same by hand. Sizes default to 20, at most 100
(`orm.DefaultSize(n)`, `orm.MaxSize(n)`).

`qs.CursorPage(ctx, after, before, first, last)` reads a page by cursor
instead — the first rows after a row's cursor, or the last before one —
returning the rows, each row's opaque cursor and whether rows go on past
the page. Its cursors are keyset cursors when the query orders by the
model's own non-null columns (the primary key added to break ties), so a
page costs the same however deep, and positions otherwise; a cursor of
another ordering is `orm.ErrCursor`. It is what a GraphQL connection pages
with ([Pagination](/web/graphql#pagination)).

## Rendering Results {#rendering}

A page's markup calls a query method like any other, and the render's `ctx`
is passed for it ([The Render's Context](/ui/templates#implicit-ctx)):

::: code-group
```html [Vuka]
<script lang="go">
package blog

import "context"

type LatestPosts struct{ Limit int }

func (l LatestPosts) Posts(ctx context.Context) ([]Post, error) {
	return Post.Objects.Filter(Post.Status.Eq("published")).
		SelectRelated(Post.Author.String()).
		OrderBy(Post.Published.Desc()).Limit(l.Limit).All(ctx)
}
</script>

<ul>
  for _, p := range Posts() {
    <li key={p.ID}>{p.Title} by {p.Author.Related().Name}</li>
  }
</ul>
```
```go [Generated Go]
type LatestPosts struct{ Limit int }

func (l LatestPosts) Posts(ctx context.Context) ([]Post, error) {
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).
		SelectRelated(PostColumns.Author().String()).
		OrderBy(PostColumns.Published.Desc()).Limit(l.Limit).All(ctx)
}

func (l LatestPosts) View() ui.Node {
	return ui.Ctx(func(ctx context.Context) ui.Node {
		var lis []ui.Node
		for _, p := range ui.Must(l.Posts(ctx)) {
			lis = append(lis, ui.HTML(`<li key={p.ID}>{p.Title} by {p.Author.Related().Name}</li>`, p.ID, p.Title, p.Author.Related().Name))
		}
		return ui.HTML(`<ul>{lis}</ul>`, lis)
	})
}

func (l LatestPosts) Render(ctx context.Context, w io.Writer) error { return l.View().Render(ctx, w) }
```
:::

A failing query fails the render, as an error from any call in markup does.

## Seeing the SQL {#seeing-the-sql}

A query is built for the context's database without running it. `SQL`
gives a query's text and arguments, and each terminal has its own
`…SQL` taking the same arguments: `CountSQL`, `ExistsSQL`, `AggregateSQL`,
`UpdateSQL` and `DeleteSQL` return the `orm.Statement` that `Count`,
`Exists`, `Aggregate`, `Update` and `Delete` would run, refused as they
would refuse it:

::: code-group
```vuka [Vuka]
func showSQL(ctx context.Context) error {
	popular := Post.Objects.Filter(Post.Title.IContains("go"), Post.Views.Gte(100))
	text, args := popular.OrderBy(Post.Views.Desc()).Limit(10).SQL(ctx)?
	fmt.Println(text, args)
	count := popular.CountSQL(ctx)?
	fmt.Println(count)
	feature := popular.UpdateSQL(ctx, orm.Set{"status": "featured"})?
	fmt.Println(feature.Text, feature.Args)
	return nil
}
```
```go [Generated Go]
func showSQL(ctx context.Context) error {
	popular := orm.Objects[Post]().Filter(PostColumns.Title.IContains("go"), PostColumns.Views.Gte(100))
	text, args, err := popular.OrderBy(PostColumns.Views.Desc()).Limit(10).SQL(ctx)
	if err != nil {
		return err
	}
	fmt.Println(text, args)
	count, err := popular.CountSQL(ctx)
	if err != nil {
		return err
	}
	fmt.Println(count)
	feature, err := popular.UpdateSQL(ctx, orm.Set{"status": "featured"})
	if err != nil {
		return err
	}
	fmt.Println(feature.Text, feature.Args)
	return nil
}
```
:::

```sql
SELECT COUNT(*) FROM posts WHERE LOWER(posts.title) LIKE LOWER(?) ESCAPE '!' AND posts.views >= ?
-- args: '%go%', 100
```

A `Statement` is its `Text` and its `Args`, kept apart: the arguments are
bound to the placeholders when the statement runs, never written into the
text. Printed, a Statement shows them in a comment after it, for reading.

`Explain` asks the database how it would run the query — which indexes it
uses, what it scans and sorts — sending the same statement with the same
arguments. A query has `Explain`, and so has every `Statement`:

::: code-group
```vuka [Vuka]
func explainTop(ctx context.Context) error {
	top := Post.Objects.Filter(Post.Status.Eq("published")).OrderBy(Post.Views.Desc()).Limit(20)
	fmt.Println(top.Explain(ctx)?)
	measured := top.Explain(ctx, orm.Analyze(), orm.Format(orm.PlanJSON))?
	fmt.Println(string(measured.JSON))
	return nil
}

func explainPurge(ctx context.Context) error {
	purge := Post.Objects.Filter(Post.Status.Eq("archived")).DeleteSQL(ctx)?
	fmt.Println(purge.Explain(ctx, orm.Analyze())?)
	return nil
}
```
```go [Generated Go]
func explainTop(ctx context.Context) error {
	top := orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).OrderBy(PostColumns.Views.Desc()).Limit(20)
	plan, err := top.Explain(ctx)
	if err != nil {
		return err
	}
	fmt.Println(plan)
	measured, err := top.Explain(ctx, orm.Analyze(), orm.Format(orm.PlanJSON))
	if err != nil {
		return err
	}
	fmt.Println(string(measured.JSON))
	return nil
}

func explainPurge(ctx context.Context) error {
	purge, err := orm.Objects[Post]().Filter(PostColumns.Status.Eq("archived")).DeleteSQL(ctx)
	if err != nil {
		return err
	}
	plan, err := purge.Explain(ctx, orm.Analyze())
	if err != nil {
		return err
	}
	fmt.Println(plan)
	return nil
}
```
:::

```
SCAN posts
USE TEMP B-TREE FOR ORDER BY
```

An `orm.Plan` holds the plan as the database writes it in `Text` (what it
prints as), and in `JSON` with `orm.Format(orm.PlanJSON)`. `orm.Analyze()`
runs the statement and reports what it did — rows read and time taken at
each step — inside a transaction that is always rolled back, so explaining an
`UpdateSQL` or `DeleteSQL` with it changes nothing. What each database
gives:

| Database | `Explain(ctx)` | `orm.Analyze()` | `orm.Format(orm.PlanJSON)` |
|---|---|---|---|
| Postgres | `EXPLAIN` | `EXPLAIN (ANALYZE, BUFFERS)` | `FORMAT JSON` |
| MySQL | `EXPLAIN`, as a table | `EXPLAIN ANALYZE` of a `SELECT`, as text | `FORMAT=JSON` |
| SQLite | `EXPLAIN QUERY PLAN`, as a tree | — | the plan's rows as JSON |

An option a database lacks is an error saying what it gives instead:
SQLite has no `Analyze`, and MySQL analyzes a `SELECT` only, so
`explainPurge` measures its `DELETE` on Postgres.
[Why Is This Query Slow?](/orm/cookbook#slow-query) reads a plan and fixes
what it shows.

## Raw SQL {#raw-sql}

When a query is easier in SQL, `orm.Raw[R]` reads its rows into a struct (by
`db` tag, column or field name) or a scalar, and `orm.Exec` runs a
statement. `Post.Objects.Raw(sql, args…)` reads the model's own rows:

::: code-group
```vuka [Vuka]
type MonthViews struct {
	Month string `db:"month"`
	Views int64  `db:"views"`
}

func monthly(ctx context.Context) ([]MonthViews, error) {
	return orm.Raw[MonthViews](ctx, `SELECT strftime('%Y-%m', published) AS month, SUM(views) AS views
		FROM posts WHERE published IS NOT NULL GROUP BY month ORDER BY month`)
}

func resetArchived(ctx context.Context) error {
	orm.Exec(ctx, "UPDATE posts SET views = 0 WHERE status = ?", "archived")?
	popular := Post.Objects.Raw("SELECT * FROM posts WHERE views > ?", 100).All(ctx)?
	fmt.Println(len(popular))
	return nil
}
```
```go [Generated Go]
type MonthViews struct {
	Month string `db:"month"`
	Views int64  `db:"views"`
}

func monthly(ctx context.Context) ([]MonthViews, error) {
	return orm.Raw[MonthViews](ctx, `SELECT strftime('%Y-%m', published) AS month, SUM(views) AS views
		FROM posts WHERE published IS NOT NULL GROUP BY month ORDER BY month`)
}

func resetArchived(ctx context.Context) error {
	if _, err := orm.Exec(ctx, "UPDATE posts SET views = 0 WHERE status = ?", "archived"); err != nil {
		return err
	}
	popular, err := orm.Objects[Post]().Raw("SELECT * FROM posts WHERE views > ?", 100).All(ctx)
	if err != nil {
		return err
	}
	fmt.Println(len(popular))
	return nil
}
```
:::

Write `?` for arguments on every database. See
[Raw SQL](/orm/databases#raw-sql) for `RawRow`, timeouts and choosing the
database.

## Transactions

`orm.Transaction(ctx, fn)` runs `fn`'s queries in one transaction, committed
when `fn` returns nil and rolled back otherwise; queries made with the `ctx`
it passes to `fn` take part. The call is plain Go in a `.vuka` file too. See
[Transactions](/orm/transactions).
