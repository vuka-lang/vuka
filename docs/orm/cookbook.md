# Common Queries

[[toc]]

<!-- vuka:package -->

## Introduction

Short answers to the questions an app asks its database every day: each
recipe is the question, the Vuka, the Go it compiles to, the SQL it runs and
a note. [Queries](/orm/queries) explains each part at length.

The recipes use the blog models of [Queries](/orm/queries#example-models) —
authors, posts, tags, comments and profiles — with two more fields: a post
can be pinned, and a comment can be removed without being deleted. The SQL is
SQLite's, with the quoting left out.

<!-- vuka:context
type Author struct {
	orm.Base
	ID      int64
	Name    string
	Email   string
	Posts   []Post
	Profile *Profile
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
	Name string @orm.Slug{Unique: true}
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
	Pinned    bool
	Comments  []Comment
}

type Comment struct {
	orm.Base
	ID      int64
	Post    orm.FK[Post] @orm.Rel{OnDelete: orm.Cascade, Related: "comments"}
	Name    string
	Body    string
	Created time.Time    @orm.Datetime{AutoNowAdd: true}
	Removed Option[time.Time]
}
-->

## Finding Rows

### Get a Row by Key, or Answer 404 {#get-or-404}

::: code-group
```vuka [Vuka]
func postByID(ctx context.Context, id int64) (Post, error) {
	match Post.Objects.Get(ctx, Post.ID.Eq(id)) {
	case Ok(p):
		return p, nil
	case Err(orm.ErrNotFound):
		return Post{}, web.NotFound
	case Err(e):
		return Post{}, e
	}
}
```
```go [Generated Go]
func postByID(ctx context.Context, id int64) (Post, error) {
	switch p, err := orm.Objects[Post]().Get(ctx, PostColumns.ID.Eq(id)); {
	case err == nil:
		return p, nil
	case errors.Is(err, orm.ErrNotFound):
		return Post{}, web.NotFound
	case err != nil:
		e := err
		return Post{}, e
	default:
		panic("vuka: no case matched")
	}
}
```
:::

```sql
SELECT … FROM posts WHERE id = ? LIMIT 2
```

`Get` reads at most two rows, to tell one from several. Only a missing row
becomes the 404; a database that is down stays an error, a 500.

### Does a Row Exist? {#exists}

::: code-group
```vuka [Vuka]
func slugTaken(ctx context.Context, slug string) (bool, error) {
	return Post.Objects.Filter(Post.Slug.Eq(slug)).Exists(ctx)
}
```
```go [Generated Go]
func slugTaken(ctx context.Context, slug string) (bool, error) {
	return orm.Objects[Post]().Filter(PostColumns.Slug.Eq(slug)).Exists(ctx)
}
```
:::

```sql
SELECT 1 FROM posts WHERE slug = ? LIMIT 1
```

`Exists` is `(false, nil)` when there is no row, never `orm.ErrNotFound`, and
reads no columns.

### The Latest N Rows {#latest}

::: code-group
```vuka [Vuka]
func latestPosts(ctx context.Context, n int) ([]Post, error) {
	return Post.Objects.Filter(Post.Published.IsNull(false)).
		OrderBy(Post.Published.Desc(), Post.ID.Desc()).Limit(n).All(ctx)
}
```
```go [Generated Go]
func latestPosts(ctx context.Context, n int) ([]Post, error) {
	return orm.Objects[Post]().Filter(PostColumns.Published.IsNull(false)).
		OrderBy(PostColumns.Published.Desc(), PostColumns.ID.Desc()).Limit(n).All(ctx)
}
```
:::

```sql
SELECT … FROM posts WHERE published IS NOT NULL
ORDER BY published DESC, id DESC LIMIT ?
```

The key breaks ties, so two posts published together come back in the same
order every time.

### Search Text, Ignoring Case {#search}

::: code-group
```vuka [Vuka]
func search(ctx context.Context, q string) ([]Post, error) {
	return Post.Objects.Filter(orm.Or(Post.Title.IContains(q), Post.Body.IContains(q))).All(ctx)
}
```
```go [Generated Go]
func search(ctx context.Context, q string) ([]Post, error) {
	return orm.Objects[Post]().Filter(orm.Or(PostColumns.Title.IContains(q), PostColumns.Body.IContains(q))).All(ctx)
}
```
:::

```sql
SELECT … FROM posts
WHERE (LOWER(title) LIKE LOWER(?) ESCAPE '!' OR LOWER(body) LIKE LOWER(?) ESCAPE '!')
```

`%` and `_` in what the visitor typed are escaped. For ranking and stemming,
use [full-text search](/orm/search).

### Rows in a Date Range, or This Month {#date-range}

::: code-group
```vuka [Vuka]
func publishedBetween(ctx context.Context, from, to time.Time) ([]Post, error) {
	return Post.Objects.Filter(Post.Published.Gte(from), Post.Published.Lt(to)).All(ctx)
}

func thisMonth(ctx context.Context) ([]Post, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return publishedBetween(ctx, start, start.AddDate(0, 1, 0))
}
```
```go [Generated Go]
func publishedBetween(ctx context.Context, from, to time.Time) ([]Post, error) {
	return orm.Objects[Post]().Filter(PostColumns.Published.Gte(from), PostColumns.Published.Lt(to)).All(ctx)
}

func thisMonth(ctx context.Context) ([]Post, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return publishedBetween(ctx, start, start.AddDate(0, 1, 0))
}
```
:::

```sql
SELECT … FROM posts WHERE (published >= ? AND published < ?)
```

A half-open range, `>=` the start and `<` the end, uses an index on the
column and never counts a row twice at a boundary.
`Post.Published.Gte(orm.Today().AddDays(-30))` asks the database's clock
instead.

## Following Relationships

### An Author's Posts {#author-posts}

::: code-group
```vuka [Vuka]
func postsBy(ctx context.Context, a Author) ([]Post, error) {
	return Post.Objects.Filter(orm.Q{"author": a.ID}).OrderBy(Post.Published.Desc()).All(ctx)
}

func loadPosts(ctx context.Context, a *Author) error {
	return a.Load(ctx, Author.Posts) // fills a.Posts
}
```
```go [Generated Go]
func postsBy(ctx context.Context, a Author) ([]Post, error) {
	return orm.Objects[Post]().Filter(orm.Q{"author": a.ID}).OrderBy(PostColumns.Published.Desc()).All(ctx)
}

func loadPosts(ctx context.Context, a *Author) error {
	return a.Load(ctx, AuthorColumns.Posts()) // fills a.Posts
}
```
:::

```sql
SELECT … FROM posts WHERE author_id = ? ORDER BY published DESC
```

The query goes on like any other: filter, order, count. `Load` needs a
field named after the relation, `Posts []Post` here.

### Authors With a Published Post {#parents-by-children}

::: code-group
```vuka [Vuka]
var publishing = Author.Objects.Filter(Author.Posts.Status.Eq("published"))
```
```go [Generated Go]
var publishing = orm.Objects[Author]().Filter(AuthorColumns.Posts().Status.Eq("published"))
```
:::

```sql
SELECT authors.* FROM authors
WHERE EXISTS (SELECT 1 FROM posts AS p WHERE p.author_id = authors.id AND p.status = ?)
```

An `EXISTS`, so each author comes back once however many posts match.

### Authors With No Posts {#no-related-rows}

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

Not `Filter(Author.Posts.ID.IsNull(true))`, which looks for a post with no
key and finds none.

### Two Conditions on the Same Related Row {#same-related-row}

"Authors with a published post that has over 100 views" — one post meeting
both, not one post for each:

::: code-group
```vuka [Vuka]
var hit = Author.Posts.Where(Author.Posts.Status.Eq("published"), Author.Posts.Views.Gt(100))

var withAHit = Author.Objects.Filter(orm.Typed[bool](hit.Exists()).Eq(true))
```
```go [Generated Go]
var hit = AuthorColumns.Posts().Where(AuthorColumns.Posts().Status.Eq("published"), AuthorColumns.Posts().Views.Gt(100))

var withAHit = orm.Objects[Author]().Filter(orm.Typed[bool](hit.Exists()).Eq(true))
```
:::

`Filter(Author.Posts.Status.Eq("published"), Author.Posts.Views.Gt(100))`
tests each condition on its own, so it also keeps an author whose published
post has few views and whose popular post is a draft. See [Conditions on
Several Related Rows](/orm/queries#multi-valued).

### Posts of a Tag, Tags of a Post {#many-to-many}

::: code-group
```vuka [Vuka]
func taggedPosts(ctx context.Context, tag string) ([]Post, error) {
	return Post.Objects.Filter(Post.Tags.Name.Eq(tag)).All(ctx)
}

func tagsOf(ctx context.Context, p *Post) ([]Tag, error) {
	return p.Tags.All(ctx).Get()
}

func tagsUsedBy(ctx context.Context, author string) ([]Tag, error) {
	return Tag.Objects.Filter(TagColumns.Posts().Author().Name.Eq(author)).All(ctx)
}
```
```go [Generated Go]
func taggedPosts(ctx context.Context, tag string) ([]Post, error) {
	return orm.Objects[Post]().Filter(PostColumns.Tags().Name.Eq(tag)).All(ctx)
}

func tagsOf(ctx context.Context, p *Post) ([]Tag, error) {
	return p.Tags.All(ctx).Get()
}

func tagsUsedBy(ctx context.Context, author string) ([]Tag, error) {
	return orm.Objects[Tag]().Filter(TagColumns.Posts().Author().Name.Eq(author)).All(ctx)
}
```
:::

```sql
SELECT posts.* FROM posts
WHERE EXISTS (SELECT 1 FROM posts_tags AS t JOIN tags ON tags.id = t.tag_id
              WHERE t.post_id = posts.id AND tags.name = ?)
```

`Tag` has no field for its posts, so its side is the generated path,
`TagColumns.Posts()`, named by the many-to-many's `Related`.

### An Author's Profile, From Either Side {#one-to-one}

::: code-group
```vuka [Vuka]
func bioOf(ctx context.Context, authorID int64) (string, error) {
	pr := Profile.Objects.Get(ctx, orm.Q{"author": authorID})?
	return pr.Bio, nil
}

var mathematicians = Author.Objects.Filter(Author.Profile.Bio.IContains("maths"))
```
```go [Generated Go]
func bioOf(ctx context.Context, authorID int64) (string, error) {
	pr, err := orm.Objects[Profile]().Get(ctx, orm.Q{"author": authorID})
	if err != nil {
		return "", err
	}
	return pr.Bio, nil
}

var mathematicians = orm.Objects[Author]().Filter(AuthorColumns.Profile().Bio.IContains("maths"))
```
:::

```sql
SELECT authors.* FROM authors
LEFT JOIN profiles AS profile ON profile.author_id = authors.id
WHERE LOWER(profile.bio) LIKE LOWER(?) ESCAPE '!'
```

A unique foreign key is a one-to-one: its way back is a join, not an
`EXISTS`, and a `Profile *Profile` field on `Author` holds it once loaded.

### Add and Remove Related Rows {#related-rows}

::: code-group
```vuka [Vuka]
func tagPost(ctx context.Context, p *Post, names ...string) error {
	for _, name := range names {
		tag, _, err := Tag.Objects.GetOrCreate(ctx, orm.Q{"name": name}, nil)
		if err != nil {
			return err
		}
		p.Tags.Add(ctx, &tag)?
	}
	return nil
}

func untag(ctx context.Context, tag *Tag, p *Post) error {
	return tag.Remove(ctx, "posts", p) // from the tag's side
}

func moveTo(ctx context.Context, p *Post, a *Author) error {
	p.Author = orm.FKOf(a)
	return p.SaveFields(ctx, "author")
}
```
```go [Generated Go]
func tagPost(ctx context.Context, p *Post, names ...string) error {
	for _, name := range names {
		tag, _, err := orm.Objects[Tag]().GetOrCreate(ctx, orm.Q{"name": name}, nil)
		if err != nil {
			return err
		}
		e1 := p.Tags.Add(ctx, &tag)
		if e1 != nil {
			return e1
		}
	}
	return nil
}

func untag(ctx context.Context, tag *Tag, p *Post) error {
	return tag.Remove(ctx, "posts", p) // from the tag's side
}

func moveTo(ctx context.Context, p *Post, a *Author) error {
	p.Author = orm.FKOf(a)
	return p.SaveFields(ctx, "author")
}
```
:::

A many-to-many link changes from either side: the field on the side that
declares it, the relation's name on the other. A foreign key changes on the
row that holds it.

### Links With Data of Their Own {#link-model}

A link that carries data — who added a tag, and when — is a model of its
own with a foreign key to each side, queried like any other:

::: code-group
```vuka [Vuka]
type Contribution struct {
	orm.Base
	ID     int64
	Post   orm.FK[Post]   @orm.Rel{OnDelete: orm.Cascade, Related: "contributions"}
	Author orm.FK[Author] @orm.Rel{OnDelete: orm.Cascade, Related: "contributions"}
	Role   string         @orm.Char{Max: 20}
}

var editedByAda = Post.Objects.Filter(
	PostColumns.Contributions().Author().Name.Eq("Ada"),
	PostColumns.Contributions().Role.Eq("editor"))

func credit(ctx context.Context, p *Post, a *Author, role string) error {
	return Contribution.Objects.Create(ctx, &Contribution{Post: orm.FKOf(p), Author: orm.FKOf(a), Role: role})
}
```
```go [Generated Go]
type Contribution struct {
	orm.Model[Contribution]
	ID     int64
	Post   orm.FK[Post]
	Author orm.FK[Author]
	Role   string
}

func (c *Contribution) Schema(s *orm.Schema) {
	s.Field(&c.Post, orm.Rel{OnDelete: orm.Cascade, Related: "contributions"})
	s.Field(&c.Author, orm.Rel{OnDelete: orm.Cascade, Related: "contributions"})
	s.Field(&c.Role, orm.Char{Max: 20})
}

var editedByAda = orm.Objects[Post]().Filter(
	PostColumns.Contributions().Author().Name.Eq("Ada"),
	PostColumns.Contributions().Role.Eq("editor"))

func credit(ctx context.Context, p *Post, a *Author, role string) error {
	return orm.Objects[Contribution]().Create(ctx, &Contribution{Post: orm.FKOf(p), Author: orm.FKOf(a), Role: role})
}
```
:::

As with any relation holding many rows, the two conditions above may meet
on different contributions; use [the same related
row](#same-related-row) when they must not.

## Counting and Aggregating

### Posts per Author {#count-per-group}

::: code-group
```vuka [Vuka]
type AuthorPosts struct {
	Name  string
	Posts int64
}

func postsPerAuthor(ctx context.Context) ([]AuthorPosts, error) {
	return Author.Objects.Annotate("posts", Author.Posts.Count()).
		OrderBy(Author.Name.Asc()).Values[AuthorPosts]("name", "posts").All(ctx)
}
```
```go [Generated Go]
type AuthorPosts struct {
	Name  string
	Posts int64
}

func postsPerAuthor(ctx context.Context) ([]AuthorPosts, error) {
	return orm.Objects[Author]().Annotate("posts", AuthorColumns.Posts().Count()).
		OrderBy(AuthorColumns.Name.Asc()).Values[AuthorPosts]("name", "posts").All(ctx)
}
```
:::

```sql
SELECT authors.name, (COUNT(DISTINCT posts.id)) AS posts FROM authors
LEFT JOIN posts ON posts.author_id = authors.id
GROUP BY authors.id, 1 ORDER BY authors.name ASC
```

Authors with no posts are there with 0: the join is a `LEFT JOIN`.

### Annotate Each Row With a Related Count, and Filter on It {#annotate-count}

::: code-group
```vuka [Vuka]
var commentCount = orm.Annotation[int64]("comments", Post.Comments.Count())

var discussed = Post.Objects.Annotate(commentCount).Filter(commentCount.Gte(5)).Order(commentCount.Desc())
```
```go [Generated Go]
var commentCount = orm.Annotation[int64]("comments", PostColumns.Comments().Count())

var discussed = orm.Objects[Post]().Annotate(commentCount).Filter(commentCount.Gte(5)).Order(commentCount.Desc())
```
:::

```sql
SELECT posts.* FROM posts LEFT JOIN comments ON comments.post_id = posts.id
GROUP BY posts.id, …
HAVING (COUNT(DISTINCT comments.id)) >= ?
ORDER BY (COUNT(DISTINCT comments.id)) DESC
```

A filter on an annotation that aggregates is `HAVING`. Read such a query
with `All` or `Values`: its `Count` fails, saying so. `Where` on the path
counts only some rows: `Post.Comments.Where(Post.Comments.Removed.IsNull(true)).Count()`.

### Top N by an Aggregate {#top-n}

The five authors with the most views across their posts:

::: code-group
```vuka [Vuka]
type AuthorViews struct {
	Name  string
	Views Option[int64]
}

func topAuthors(ctx context.Context) ([]AuthorViews, error) {
	views := orm.Annotation[int64]("views", Author.Posts.Sum(Author.Posts.Views))
	return Author.Objects.Annotate(views).Order(views.Desc()).Limit(5).
		Values[AuthorViews]("name", "views").All(ctx)
}
```
```go [Generated Go]
type AuthorViews struct {
	Name  string
	Views vuka.Option[int64]
}

func topAuthors(ctx context.Context) ([]AuthorViews, error) {
	views := orm.Annotation[int64]("views", AuthorColumns.Posts().Sum(AuthorColumns.Posts().Views))
	return orm.Objects[Author]().Annotate(views).Order(views.Desc()).Limit(5).
		Values[AuthorViews]("name", "views").All(ctx)
}
```
:::

An author with no posts has no sum, NULL, so `Views` is an `Option`.

### Authors by Their Latest Post {#order-by-latest}

::: code-group
```vuka [Vuka]
func recentlyActive(ctx context.Context) ([]Author, error) {
	latest := orm.Annotation[time.Time]("latest", Author.Posts.Max(Author.Posts.Published))
	return Author.Objects.Annotate(latest).Filter(Author.Posts.ID.IsNull(false)).Order(latest.Desc()).All(ctx)
}
```
```go [Generated Go]
func recentlyActive(ctx context.Context) ([]Author, error) {
	latest := orm.Annotation[time.Time]("latest", AuthorColumns.Posts().Max(AuthorColumns.Posts().Published))
	return orm.Objects[Author]().Annotate(latest).Filter(AuthorColumns.Posts().ID.IsNull(false)).Order(latest.Desc()).All(ctx)
}
```
:::

The `Filter` keeps authors with a post; without it, those with none sort
by a NULL date, last.

### Distinct Values of a Column {#distinct}

::: code-group
```vuka [Vuka]
func statuses(ctx context.Context) ([]string, error) {
	return Post.Objects.OrderBy(Post.Status.Asc()).Values[string]("status").Distinct().All(ctx)
}
```
```go [Generated Go]
func statuses(ctx context.Context) ([]string, error) {
	return orm.Objects[Post]().OrderBy(PostColumns.Status.Asc()).Values[string]("status").Distinct().All(ctx)
}
```
:::

```sql
SELECT DISTINCT status FROM posts ORDER BY status ASC
```

### Rows per Day and per Month {#per-period}

::: code-group
```vuka [Vuka]
type DayCount struct {
	Day string
	N   int64
}

func postsPerDay(ctx context.Context) ([]DayCount, error) {
	return Post.Objects.Filter(Post.Published.IsNull(false)).
		Annotate("day", orm.Date(Post.Published.Expr())).
		Annotate("n", orm.Count("id")).
		OrderBy("day").Values[DayCount]("day", "n").All(ctx)
}

type MonthCount struct {
	Year, Month int
	N           int64
}

func postsPerMonth(ctx context.Context) ([]MonthCount, error) {
	return Post.Objects.Filter(Post.Published.IsNull(false)).
		Annotate("year", orm.Year(Post.Published.Expr())).
		Annotate("month", orm.Month(Post.Published.Expr())).
		Annotate("n", orm.Count("id")).
		OrderBy("year", "month").Values[MonthCount]("year", "month", "n").All(ctx)
}
```
```go [Generated Go]
type DayCount struct {
	Day string
	N   int64
}

func postsPerDay(ctx context.Context) ([]DayCount, error) {
	return orm.Objects[Post]().Filter(PostColumns.Published.IsNull(false)).
		Annotate("day", orm.Date(PostColumns.Published.Expr())).
		Annotate("n", orm.Count("id")).
		OrderBy("day").Values[DayCount]("day", "n").All(ctx)
}

type MonthCount struct {
	Year, Month int
	N           int64
}

func postsPerMonth(ctx context.Context) ([]MonthCount, error) {
	return orm.Objects[Post]().Filter(PostColumns.Published.IsNull(false)).
		Annotate("year", orm.Year(PostColumns.Published.Expr())).
		Annotate("month", orm.Month(PostColumns.Published.Expr())).
		Annotate("n", orm.Count("id")).
		OrderBy("year", "month").Values[MonthCount]("year", "month", "n").All(ctx)
}
```
:::

```sql
SELECT (DATE(published)) AS day, (COUNT(id)) AS n FROM posts
WHERE NOT (published IS NULL) GROUP BY 1 ORDER BY day
SELECT (CAST(strftime('%Y', published) AS INTEGER)) AS year,
       (CAST(strftime('%m', published) AS INTEGER)) AS month, (COUNT(id)) AS n
FROM posts WHERE NOT (published IS NULL) GROUP BY 1, 2 ORDER BY …
```

`Date` is the day of a timestamp; a month is its year and month together.
Days and months with no rows are missing from the result: fill them in Go.

### Several Counts at Once {#conditional-counts}

::: code-group
```vuka [Vuka]
func dashboard(ctx context.Context) (total, published, popular int64, err error) {
	r := Post.Objects.Aggregate(ctx,
		orm.Count("id").As("total"),
		orm.Count("id").Filter(Post.Status.Eq("published")).As("published"),
		orm.Count("id").Filter(Post.Views.Gte(1000)).As("popular"))?
	return r.Int("total"), r.Int("published"), r.Int("popular"), nil
}
```
```go [Generated Go]
func dashboard(ctx context.Context) (total, published, popular int64, err error) {
	r, e1 := orm.Objects[Post]().Aggregate(ctx,
		orm.Count("id").As("total"),
		orm.Count("id").Filter(PostColumns.Status.Eq("published")).As("published"),
		orm.Count("id").Filter(PostColumns.Views.Gte(1000)).As("popular"))
	if e1 != nil {
		return 0, 0, 0, e1
	}
	return r.Int("total"), r.Int("published"), r.Int("popular"), nil
}
```
:::

```sql
SELECT COUNT(id) AS total,
       COUNT(id) FILTER (WHERE status = ?) AS published,
       COUNT(id) FILTER (WHERE views >= ?) AS popular
FROM posts
```

One query instead of three. MySQL writes each as `COUNT(CASE WHEN … THEN id END)`.

## Selecting

### Only Some Columns {#some-columns}

::: code-group
```vuka [Vuka]
type PostLink struct {
	Title string
	Slug  string
}

func links(ctx context.Context) ([]PostLink, error) {
	return Post.Objects.Filter(Post.Status.Eq("published")).Values[PostLink]("title", "slug").All(ctx)
}
```
```go [Generated Go]
type PostLink struct {
	Title string
	Slug  string
}

func links(ctx context.Context) ([]PostLink, error) {
	return orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).Values[PostLink]("title", "slug").All(ctx)
}
```
:::

```sql
SELECT title, slug FROM posts WHERE status = ?
```

The result is a `PostLink`, not a `Post`: nothing else is read, and there is
no half-filled row to save by mistake.

### The Latest Comment of Each Post {#latest-related-row}

::: code-group
```vuka [Vuka]
type PostLatest struct {
	Title  string
	Latest Option[string]
}

func latestComments(ctx context.Context) ([]PostLatest, error) {
	newest := Comment.Objects.Filter(orm.Q{"post": orm.OuterRef("id")}).
		OrderBy(Comment.Created.Desc()).Limit(1).Values[string]("body")
	return Post.Objects.Annotate("latest", orm.Subquery(newest)).Values[PostLatest]("title", "latest").All(ctx)
}
```
```go [Generated Go]
type PostLatest struct {
	Title  string
	Latest vuka.Option[string]
}

func latestComments(ctx context.Context) ([]PostLatest, error) {
	newest := orm.Objects[Comment]().Filter(orm.Q{"post": orm.OuterRef("id")}).
		OrderBy(CommentColumns.Created.Desc()).Limit(1).Values[string]("body")
	return orm.Objects[Post]().Annotate("latest", orm.Subquery(newest)).Values[PostLatest]("title", "latest").All(ctx)
}
```
:::

```sql
SELECT posts.title,
       ((SELECT c.body FROM comments AS c WHERE c.post_id = posts.id
         ORDER BY c.created DESC LIMIT 1)) AS latest
FROM posts
```

`orm.OuterRef("id")` is the outer post's key; a post with no comment gets
`None`.

### Rows Matching Another Query {#exists-in-filter}

::: code-group
```vuka [Vuka]
var withRecentComments = Post.Objects.Filter(orm.Exists(
	Comment.Objects.Filter(orm.Q{"post": orm.OuterRef("id")}, Comment.Created.Gte(orm.Today().AddDays(-1)))))

var withProfile = Author.Objects.Filter(Author.ID.InQuery(Profile.Objects.Values[int64]("author")))
```
```go [Generated Go]
var withRecentComments = orm.Objects[Post]().Filter(orm.Exists(
	orm.Objects[Comment]().Filter(orm.Q{"post": orm.OuterRef("id")}, CommentColumns.Created.Gte(orm.Today().AddDays(-1)))))

var withProfile = orm.Objects[Author]().Filter(AuthorColumns.ID.InQuery(orm.Objects[Profile]().Values[int64]("author")))
```
:::

`orm.Exists` stops at the first matching row; `InQuery` is `IN (SELECT …)`.

## Loading Related Rows

### One Query Instead of N+1 {#n-plus-one}

::: code-group
```vuka [Vuka]
func feed(ctx context.Context) ([]Post, error) {
	return Post.Objects.
		SelectRelated(Post.Author.String()).
		PrefetchRelated(Post.Tags, Post.Comments).
		OrderBy(Post.Published.Desc()).Limit(20).All(ctx)
}

func authorsWithPosts(ctx context.Context) ([]Author, error) {
	return Author.Objects.PrefetchRelated(Author.Posts, "posts__comments").All(ctx)
}
```
```go [Generated Go]
func feed(ctx context.Context) ([]Post, error) {
	return orm.Objects[Post]().
		SelectRelated(PostColumns.Author().String()).
		PrefetchRelated(PostColumns.Tags(), PostColumns.Comments()).
		OrderBy(PostColumns.Published.Desc()).Limit(20).All(ctx)
}

func authorsWithPosts(ctx context.Context) ([]Author, error) {
	return orm.Objects[Author]().PrefetchRelated(AuthorColumns.Posts(), "posts__comments").All(ctx)
}
```
:::

```sql
SELECT posts.*, author.* FROM posts LEFT JOIN authors AS author ON author.id = posts.author_id …
SELECT … FROM tags JOIN posts_tags … WHERE posts_tags.post_id IN (?, ?, …)
SELECT … FROM comments WHERE post_id IN (?, ?, …)
```

`SelectRelated` joins what each row has one of — a foreign key, a
one-to-one — into the same query. `PrefetchRelated` reads what a row has
many of — the posts back from an author, a many-to-many — in one more query
for all the rows. Three queries for twenty posts, not forty-one.

### A Page of Posts {#page}

::: code-group
```html [Vuka]
<script lang="go">
package blog

import "context"

type PostList struct{ Page int }

func (l PostList) Rows(ctx context.Context) (orm.PageResult[Post], error) {
	return orm.Paginate(ctx, Post.Objects.SelectRelated(Post.Author.String()),
		orm.PageRequest{Page: l.Page, Sort: "-published"}, orm.Sortable("published"))
}
</script>

<section>
  page := Rows()
  <ul>
    for _, p := range page.Items {
      <li key={p.ID}>{p.Title} by {p.Author.Related().Name}</li>
    }
  </ul>
  <p>Page {page.Page} of {page.Pages}</p>
</section>
```
```go [Generated Go]
type PostList struct{ Page int }

func (l PostList) Rows(ctx context.Context) (orm.PageResult[Post], error) {
	return orm.Paginate(ctx, orm.Objects[Post]().SelectRelated(PostColumns.Author().String()),
		orm.PageRequest{Page: l.Page, Sort: "-published"}, orm.Sortable("published"))
}

func (l PostList) View() ui.Node {
	return ui.Ctx(func(ctx context.Context) ui.Node {
		page := sync.OnceValue(func() orm.PageResult[Post] { return ui.Must(l.Rows(ctx)) })
		var lis []ui.Node
		for _, p := range page().Items {
			lis = append(lis, ui.HTML(`<li key={p.ID}>{p.Title} by {p.Author.Related().Name}</li>`, p.ID, p.Title, p.Author.Related().Name))
		}
		return ui.HTML(`
<section>
  <ul>{lis}</ul>
  <p>Page {page().Page} of {page().Pages}</p>
</section>`, lis, page().Page, page().Pages)
	})
}

func (l PostList) Render(ctx context.Context, w io.Writer) error { return l.View().Render(ctx, w) }
```
:::

`orm.Paginate` counts the rows and reads one page; see
[Paginating Results](/orm/queries#pagination).

### Keyset Pages {#keyset}

::: code-group
```vuka [Vuka]
func after(ctx context.Context, cursor string) ([]Post, []string, bool, error) {
	return Post.Objects.OrderBy(Post.ID.Desc()).CursorPage(ctx, cursor, "", 20, 0)
}
```
```go [Generated Go]
func after(ctx context.Context, cursor string) ([]Post, []string, bool, error) {
	return orm.Objects[Post]().OrderBy(PostColumns.ID.Desc()).CursorPage(ctx, cursor, "", 20, 0)
}
```
:::

A cursor is a row's position by the ordered columns, so the next page is a
`WHERE id < ?` rather than an `OFFSET`: it costs the same on page 1 and page
10,000, and a row added meanwhile doesn't shift the pages.

## Writing

### Bulk Insert {#bulk-insert}

::: code-group
```vuka [Vuka]
func importTags(ctx context.Context, names []string) error {
	tags := make([]*Tag, len(names))
	for i, n := range names {
		tags[i] = &Tag{Name: n}
	}
	return Tag.Objects.BulkCreate(ctx, tags)
}
```
```go [Generated Go]
func importTags(ctx context.Context, names []string) error {
	tags := make([]*Tag, len(names))
	for i, n := range names {
		tags[i] = &Tag{Name: n}
	}
	return orm.Objects[Tag]().BulkCreate(ctx, tags)
}
```
:::

One `INSERT` for all the rows, each checked against its fields first; the
keys are filled in.

### Get or Create, Update or Create {#get-or-create}

::: code-group
```vuka [Vuka]
func tagNamed(ctx context.Context, name string) (Tag, error) {
	tag, _, err := Tag.Objects.GetOrCreate(ctx, orm.Q{"name": name}, nil)
	return tag, err
}

func setBio(ctx context.Context, a *Author, bio string) (created bool, err error) {
	_, created, err = Profile.Objects.UpdateOrCreate(ctx, orm.Q{"author": a.ID}, orm.Defaults{"bio": bio})
	return created, err
}
```
```go [Generated Go]
func tagNamed(ctx context.Context, name string) (Tag, error) {
	tag, _, err := orm.Objects[Tag]().GetOrCreate(ctx, orm.Q{"name": name}, nil)
	return tag, err
}

func setBio(ctx context.Context, a *Author, bio string) (created bool, err error) {
	_, created, err = orm.Objects[Profile]().UpdateOrCreate(ctx, orm.Q{"author": a.ID}, orm.Defaults{"bio": bio})
	return created, err
}
```
:::

The lookup's fields and the defaults make the new row. A row another request
inserts first, between the read and the insert, is read rather than
duplicated, so the lookup should be unique.

### Add to a Counter {#counter}

::: code-group
```vuka [Vuka]
func viewed(ctx context.Context, id int64) error {
	Post.Objects.Filter(Post.ID.Eq(id)).Update(ctx, orm.Set{"views": Post.Views.Add(1)})?
	return nil
}

func foldLikes(ctx context.Context) (int64, error) {
	score := orm.SQL("{0} + {1}", Post.Views.Expr(), Post.Likes.Expr())
	return Post.Objects.Filter(Post.Likes.Gt(0)).Update(ctx, orm.Set{"views": score, "likes": 0})
}
```
```go [Generated Go]
func viewed(ctx context.Context, id int64) error {
	if _, err := orm.Objects[Post]().Filter(PostColumns.ID.Eq(id)).Update(ctx, orm.Set{"views": PostColumns.Views.Add(1)}); err != nil {
		return err
	}
	return nil
}

func foldLikes(ctx context.Context) (int64, error) {
	score := orm.SQL("{0} + {1}", PostColumns.Views.Expr(), PostColumns.Likes.Expr())
	return orm.Objects[Post]().Filter(PostColumns.Likes.Gt(0)).Update(ctx, orm.Set{"views": score, "likes": 0})
}
```
:::

```sql
UPDATE posts SET views = (views + ?) WHERE id = ?
UPDATE posts SET views = views + likes, likes = ? WHERE likes > ?
```

The database adds, so two requests at once both count. Reading the row,
adding one in Go and saving would lose one of them. An expression may read
any of the row's columns: `foldLikes` moves each post's likes into its
views.

### Toggle a Flag {#toggle}

::: code-group
```vuka [Vuka]
func togglePin(ctx context.Context, id int64) error {
	flipped := orm.Case(orm.When(Post.Pinned.Eq(true), false)).Else(true)
	Post.Objects.Filter(Post.ID.Eq(id)).Update(ctx, orm.Set{"pinned": flipped})?
	return nil
}
```
```go [Generated Go]
func togglePin(ctx context.Context, id int64) error {
	flipped := orm.Case(orm.When(PostColumns.Pinned.Eq(true), false)).Else(true)
	if _, err := orm.Objects[Post]().Filter(PostColumns.ID.Eq(id)).Update(ctx, orm.Set{"pinned": flipped}); err != nil {
		return err
	}
	return nil
}
```
:::

```sql
UPDATE posts SET pinned = CASE WHEN pinned = ? THEN ? ELSE ? END WHERE id = ?
```

### Label Rows by a Condition {#case-when}

::: code-group
```vuka [Vuka]
type Heat struct {
	Title string
	Heat  string
}

func heats(ctx context.Context) ([]Heat, error) {
	heat := orm.Case(orm.When(Post.Views.Gte(1000), "hot"), orm.When(Post.Views.Gte(100), "warm")).Else("cold")
	return Post.Objects.Annotate("heat", heat).Values[Heat]("title", "heat").All(ctx)
}
```
```go [Generated Go]
type Heat struct {
	Title string
	Heat  string
}

func heats(ctx context.Context) ([]Heat, error) {
	heat := orm.Case(orm.When(PostColumns.Views.Gte(1000), "hot"), orm.When(PostColumns.Views.Gte(100), "warm")).Else("cold")
	return orm.Objects[Post]().Annotate("heat", heat).Values[Heat]("title", "heat").All(ctx)
}
```
:::

```sql
SELECT title, (CASE WHEN views >= ? THEN ? WHEN views >= ? THEN ? ELSE ? END) AS heat FROM posts
```

`orm.Case` works wherever an expression does: in `Annotate`, in `Order`, and
in `orm.Set`, as the flag above shows.

### Hide Rows Instead of Deleting Them {#soft-delete}

Mark a removed comment, and read the comments through a static that leaves
the removed ones out:

::: code-group
```vuka [Vuka]
func Comment.Visible() orm.QuerySet[Comment] {
	return Comment.Objects.Filter(Comment.Removed.IsNull(true))
}

func removeComment(ctx context.Context, c *Comment) error {
	c.Removed = Some(time.Now())
	return c.SaveFields(ctx, Comment.Removed)
}

func commentsOn(ctx context.Context, p Post) ([]Comment, error) {
	return Comment.Visible().Filter(orm.Q{"post": p.ID}).OrderBy(Comment.Created.Asc()).All(ctx)
}
```
```go [Generated Go]
func CommentVisible() orm.QuerySet[Comment] {
	return orm.Objects[Comment]().Filter(CommentColumns.Removed.IsNull(true))
}

func removeComment(ctx context.Context, c *Comment) error {
	c.Removed = vuka.Some(time.Now())
	return c.SaveFields(ctx, CommentColumns.Removed)
}

func commentsOn(ctx context.Context, p Post) ([]Comment, error) {
	return CommentVisible().Filter(orm.Q{"post": p.ID}).OrderBy(CommentColumns.Created.Asc()).All(ctx)
}
```
:::

A query starts from the manager, so use the static wherever a reader should
not see removed rows; `Comment.Objects` still reaches every row, for the
admin and for purging.

### Delete Rows Older Than a Date {#delete-older}

::: code-group
```vuka [Vuka]
func purgeRemoved(ctx context.Context, days int) (int64, error) {
	return Comment.Objects.Filter(Comment.Removed.Lt(time.Now().AddDate(0, 0, -days))).Delete(ctx)
}
```
```go [Generated Go]
func purgeRemoved(ctx context.Context, days int) (int64, error) {
	return orm.Objects[Comment]().Filter(CommentColumns.Removed.Lt(time.Now().AddDate(0, 0, -days))).Delete(ctx)
}
```
:::

```sql
DELETE FROM comments WHERE removed < ?
```

A `NULL` is never less than anything, so comments not removed stay.

### Several Writes as One {#transaction}

::: code-group
```vuka [Vuka]
func publishWithTags(ctx context.Context, p *Post, tags []*Tag) error {
	return orm.Transaction(ctx, func(ctx context.Context) error {
		p.Status, p.Published = "published", Some(time.Now())
		p.Save(ctx)?
		p.Tags.Clear(ctx)?
		for _, t := range tags {
			p.Tags.Add(ctx, t)?
		}
		return nil
	})
}
```
```go [Generated Go]
func publishWithTags(ctx context.Context, p *Post, tags []*Tag) error {
	return orm.Transaction(ctx, func(ctx context.Context) error {
		p.Status, p.Published = "published", vuka.Some(time.Now())
		if err := p.Save(ctx); err != nil {
			return err
		}
		if err := p.Tags.Clear(ctx); err != nil {
			return err
		}
		for _, t := range tags {
			if err := p.Tags.Add(ctx, t); err != nil {
				return err
			}
		}
		return nil
	})
}
```
:::

Every query using the `ctx` the function gets is in the transaction; an
error rolls all of it back. See [Transactions](/orm/transactions).

## Performance {#performance}

### Why Is This Query Slow? {#slow-query}

Ask the database for its plan, the same statement with the same arguments:

::: code-group
```vuka [Vuka]
func explainTopPublished(ctx context.Context) error {
	top := Post.Objects.Filter(Post.Status.Eq("published")).OrderBy(Post.Views.Desc()).Limit(20)
	text, args := top.SQL(ctx)?
	fmt.Println(text, args)
	fmt.Println(top.Explain(ctx)?)
	return nil
}
```
```go [Generated Go]
func explainTopPublished(ctx context.Context) error {
	top := orm.Objects[Post]().Filter(PostColumns.Status.Eq("published")).OrderBy(PostColumns.Views.Desc()).Limit(20)
	text, args, err := top.SQL(ctx)
	if err != nil {
		return err
	}
	fmt.Println(text, args)
	plan, err := top.Explain(ctx)
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

`SCAN posts` reads every post to find the published ones, then sorts them.
On Postgres, `top.Explain(ctx, orm.Analyze())` runs the query and says where
the time went (abridged):

```
Limit  (cost=403.32..403.37 rows=20 width=28) (actual time=0.894..0.896 rows=20 loops=1)
  ->  Sort  (cost=403.32..403.82 rows=200 width=28) (actual time=0.894..0.895 rows=20 loops=1)
        Sort Key: views DESC
        Sort Method: top-N heapsort  Memory: 27kB
        ->  Seq Scan on posts  (cost=0.00..398.00 rows=200 width=28) (actual time=0.004..0.879 rows=200 loops=1)
              Filter: (status = 'published'::text)
              Rows Removed by Filter: 19800
```

`Rows Removed by Filter: 19800` is the cost: 20,000 rows read for 200. An
index on the filtered column fixes it — `@orm.Char{Max: 20, Default:
"draft", Index: true}` on `Status`, then `orm makemigrations` and `orm
migrate` ([Migrations](/orm/migrations)). Explained again, the query
searches the index:

```
SEARCH posts USING INDEX idx_posts_status (status=?)
USE TEMP B-TREE FOR ORDER BY
```

and on Postgres reads only the 200 published rows (abridged):

```
Limit  (cost=36.87..36.92 rows=20 width=28) (actual time=0.075..0.076 rows=20 loops=1)
  ->  Sort  (cost=36.87..37.37 rows=200 width=28) (actual time=0.075..0.075 rows=20 loops=1)
        Sort Key: views DESC
        ->  Index Scan using idx_posts_status on posts  (cost=0.29..31.55 rows=200 width=28) (actual time=0.007..0.061 rows=200 loops=1)
              Index Cond: (status = 'published'::text)
```

A count, an update or a delete is explained the same way, through its
statement: `q.CountSQL(ctx)?.Explain(ctx)?`. `orm.Format(orm.PlanJSON)`
gives the plan as JSON for a tool to read. See [Seeing the
SQL](/orm/queries#seeing-the-sql).

## Escaping to SQL {#raw-sql}

::: code-group
```vuka [Vuka]
type WordCount struct {
	Title string `db:"title"`
	Words int    `db:"words"`
}

func longest(ctx context.Context) ([]WordCount, error) {
	return orm.Raw[WordCount](ctx, `SELECT title,
		LENGTH(body) - LENGTH(REPLACE(body, ' ', '')) + 1 AS words
		FROM posts ORDER BY words DESC LIMIT ?`, 10)
}
```
```go [Generated Go]
type WordCount struct {
	Title string `db:"title"`
	Words int    `db:"words"`
}

func longest(ctx context.Context) ([]WordCount, error) {
	return orm.Raw[WordCount](ctx, `SELECT title,
		LENGTH(body) - LENGTH(REPLACE(body, ' ', '')) + 1 AS words
		FROM posts ORDER BY words DESC LIMIT ?`, 10)
}
```
:::

`?` marks each argument on every database, bound, never written into the
SQL. The query is yours, so its dialect is too. See
[Raw SQL](/orm/databases#raw-sql).
