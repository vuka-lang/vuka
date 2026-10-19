# Queries

<span class="vuka-only">`Post.Objects` is the model's manager, `orm.Objects[Post]()` in Go: its
methods start a lazy, immutable `orm.QuerySet[Post]`.</span><span class="go-only">`Posts`,
the model's `orm.For[Post]()`, starts an `orm.QuerySet[Post]`: a lazy,
immutable query.</span> Each method returns a new one; nothing runs until a
terminal (`All`, `Get`, `Count`, …) takes a context.

::: go
The Go samples use `PostFields` (`f`), the typed fields `ormgen` writes
([Models](/orm/models#plain-go)); every condition is also an engine string,
`orm.Q{"title__contains": "Go"}`. Terminals return `(T, error)`. Values,
aggregates and pagination take the engine's names in Go:
`Posts.Annotate("n", orm.Count("id")).Values[Stats]("author__name", "n").All(ctx)`, `orm.Paginate(ctx, qs, r,
orm.Sortable("views"))`.
:::

::: vuka
```vuka
recent := Post.Objects.
	Filter(Post.Title.Contains("Go"), Post.Author.Name.Eq("ada")).
	Exclude(Post.Published.IsNull(true)).
	OrderBy(Post.Views.Desc(), Post.ID.Asc()).
	Limit(10)

posts := recent.All(ctx)?          // []Post
n := recent.Count(ctx)?            // int64
```
:::

::: go
```go
f := PostFields
recent := Posts.
	Filter(f.Title.Contains("Go"), f.Author.Name.Eq("ada")).
	Exclude(f.Published.IsNull(true)).
	OrderBy(f.Views.Desc(), f.ID.Asc()).
	Limit(10)

posts, err := recent.All(ctx) // []Post
n, err := recent.Count(ctx)   // int64
```
:::

## Conditions

A model's field, `Post.Title`, is its typed field — `PostFields.Title` in
Go, which `orm generate` writes beside the model (the vuka command
overlays it until then). It gives conditions (`orm.Cond`) and orderings:

| Field | Conditions |
|---|---|
| any | `Eq`, `Ne`, `In(vs…)`, `Range(lo, hi)`, `IsNull(bool)`, `InQuery(q)` |
| numbers, times | those, and `Lt`, `Lte`, `Gt`, `Gte` |
| strings | those, and `Contains`, `IContains`, `StartsWith`, `IStartsWith`, `EndsWith`, `IEndsWith`, `IExact` |
| any | `Asc()`, `Desc()` for `OrderBy`; `Name()`, the lookup path; `Expr()`, the column as an [expression](/orm/expressions) |

Fields go on through relations — `Post.Author.Name`, `Post.Tags.Name` (a
second hop is a method: `Post.Author.Posts().Title`) — and the query joins
what they cross. Combine conditions with `orm.And`, `orm.Or` and
`orm.Not`; several in one `Filter` are and-ed; an `orm.Q{"title__icontains":
"go"}` is a condition too:

::: vuka
```vuka
q := Post.Objects.Filter(orm.Or(Post.Views.Gt(100), orm.And(Post.Tags.Name.Eq("go"), Post.Published.IsNull(false))))
```
:::

::: go
```go
q := Posts.Filter(orm.Or(f.Views.Gt(100), orm.And(f.Tags.Name.Eq("go"), f.Published.IsNull(false))))
```
:::

| | |
|---|---|
| `Filter(conds…)`, `Exclude(conds…)` | rows matching, not matching |
| `OrderBy(Post.Views.Desc(), …)`, `Order(terms…)` | the order (names, or [expressions](/orm/expressions)' `Asc`/`Desc`); the model's `Meta.Ordering` otherwise |
| `Limit(n)`, `Offset(n)`, `Distinct()` | |
| `Unfiltered()` | without the conditions so far |

`orm.Objects[Post](schema)` is the same manager on another schema (a
tenant's names set).

## Reading

| Terminal | Returns |
|---|---|
| `All(ctx)` | `([]T, error)` |
| `Get(ctx, conds…)` | `(T, error)`: the one row matching; fails with `orm.ErrNotFound` or `orm.ErrMultipleFound` |
| `First(ctx)` | `(T, error)`: the first row in order; `orm.ErrNotFound` when there is none |
| `Count(ctx)`, `Exists(ctx)` | `(int64, error)`, `(bool, error)` |
| `Iter(ctx)` | `iter.Seq2[T, error]`: rows one at a time |

`?` takes them; `errors.Is` tells the kinds apart:

::: vuka
```vuka
p, err := Post.Objects.Get(ctx, Post.Slug.Eq(slug))
switch {
case errors.Is(err, orm.ErrNotFound):
	fmt.Println("no such post")
case err != nil:
	return err
default:
	fmt.Println(p.Title)
}

for p, err := range Post.Objects.Filter(Post.Views.Gte(50)).Iter(ctx) {
	if err != nil {
		return err
	}
	fmt.Println(p.Title)
}
```
:::

::: go
```go
p, err := Posts.Get(ctx, f.Slug.Eq(slug))
switch {
case errors.Is(err, orm.NotFound):
	fmt.Println("no such post")
case err != nil:
	return err
default:
	fmt.Println(p.Title)
}

first, err := PostLive().First(ctx) // orm.NotFound when there is none
switch {
case errors.Is(err, orm.NotFound):
	fmt.Println("nothing published")
case err != nil:
	return err
default:
	fmt.Println("latest published:", first.Title)
}

for p, err := range Posts.Filter(f.Views.Gte(50)).Iter(ctx) {
	if err != nil {
		return err
	}
	fmt.Println(p.Title)
}
```
:::

## Values and aggregates

`Values[R](names…)` reads columns, paths and annotations into `R`: a
struct (its fields matched by name, or by an `orm:"author__name"` path),
`map[string]any`, `[]any`, or a scalar for one name. An aggregate
annotation groups by the other names:

```vuka
type Stats struct {
	Author string `orm:"author__name"`
	Posts  int64
	Total  int
}

stats := Post.Objects.Annotate("posts", orm.Count("id")).Annotate("total", orm.Sum("views")).
	OrderBy(Post.Author.Name.Asc()).Values[Stats]("author__name", "posts", "total").All(ctx)?

totals := Post.Objects.Aggregate(ctx, orm.Count("author").Distinct().As("authors"), orm.Max("views").As("top"))?
fmt.Println(totals.Int("authors"), totals.Int("top"))
```

`GroupBy` and `Having` group by fields and filter on an aggregate:

```vuka
prolific := Post.Objects.Annotate("n", orm.Count("id")).GroupBy(Post.Author.String()).
	Having(orm.Q{"n__gt": 1}).Values[int64]("n").All(ctx)?
```

Aggregates are `orm.Count`, `orm.Sum`, `orm.Avg`, `orm.Min`, `orm.Max`, each
with `.Filter(cond)` (over the matching rows) and `.Distinct()`; see
[Expressions](/orm/expressions).

## Pagination

`orm.Paginate` reads a page and the total, sorting and searching only by the
fields you allow:

```vuka
page := orm.Paginate(ctx, Post.Objects.QuerySet, orm.PageFrom(r.URL.Query()),
	orm.Sortable(Post.Views.Name(), Post.Published.Name()),
	orm.Searchable(Post.Title.Name()),
	orm.DefaultSort(Post.Views.Desc()))?
// page.Items, page.Total, page.Page, page.Size, page.Pages
```

`orm.PageFrom` reads `page`, `size`, `sort` (`-views` for descending) and
`q` from query parameters; `orm.PageRequest{Page, Size, Sort, Search}` is the
same by hand. Sizes default to 20, at most 100 (`orm.DefaultSize(n)`,
`orm.MaxSize(n)`).

## Writing

| | |
|---|---|
| `p.Save(ctx)` | insert or update one row |
| `Post.Objects.Create(ctx, &p)`, `BulkCreate(ctx, []*Post{…})` | insert |
| `GetOrCreate(ctx, orm.Q{…}, orm.Defaults{…})` | `(row, created, err)`: the row matching, else one inserted with the defaults |
| `UpdateOrCreate(ctx, orm.Q{…}, orm.Defaults{…})` | the same, updating a found row with the defaults |
| `Update(ctx, orm.Set{…})` | update the matching rows; `(int64, error)` |
| `Delete(ctx)` | delete the matching rows; `(int64, error)` |

::: vuka
```vuka
golang, created, err := Tag.Objects.GetOrCreate(ctx, orm.Q{"name": "go"}, nil)

Post.Objects.Filter(Post.Views.Lt(5)).Update(ctx, orm.Set{"status": "archived"})?
Post.Objects.Filter(Post.Tags.Name.Eq("go")).Update(ctx, orm.Set{"views": orm.SQL("{0} + 1", Post.Views.Expr())})?
Post.Objects.Filter(Post.Published.IsNull(true)).Delete(ctx)?
```
:::

::: go
```go
golang, created, err := Tags.GetOrCreate(ctx, orm.Q{"name": "go"}, nil)
fmt.Println(golang.ID, created)

_, err = Posts.Filter(f.Views.Lt(5)).Update(ctx, orm.Set{"status": "archived"})
_, err = Posts.Filter(f.Published.IsNull(true)).Delete(ctx)
```
:::

`orm.Set` maps columns to values or expressions of the row's own columns,
computed by the database. `Post.Objects.OnChange(fn)` calls `fn` after each
write to the model's rows, and returns the function that stops it.

## Relations

A many-to-many's links change through the field or the row:

::: vuka
```vuka
p.Tags.Add(ctx, &golang, &tools)?
p.Tags.Remove(ctx, &tools)?
p.Tags.Set(ctx, &golang)?               // its only links
p.Add(ctx, Post.Tags, &db)?             // the same, through the row
```
:::

::: go
```go
err := p.Tags.Add(ctx, &golang, &tools)
err = p.Tags.Remove(ctx, &tools)
err = p.Tags.Set(ctx, &golang) // its only links
err = p.Add(ctx, "tags", &db)  // the same, through the row
```
:::

Reading a relation row by row is an N+1: one query for the posts, one per
post for its author. `SelectRelated` joins a foreign key into the query;
`PrefetchRelated` reads a relation's rows in one more query, for every row
at once:

::: vuka
```vuka
posts := Post.Objects.
	SelectRelated("author").                    // JOIN users
	PrefetchRelated(Post.Tags).                 // one query for every post's tags
	All(ctx)?
for _, p := range posts {
	fmt.Println(p.Author.Related().Name, len(p.Tags.Rows()))
}

goTags := Post.Objects.PrefetchRelated(orm.Prefetch("tags", Tag.Objects.Filter(Tag.Name.Eq("go"))))
```
:::

::: go
```go
posts, err := Posts.
	SelectRelated(f.Author.String()). // JOIN users
	PrefetchRelated(f.Tags).          // one query for every post's tags
	All(ctx)
for _, p := range posts {
	fmt.Println(p.Author.Related().Name, len(p.Tags.Rows()))
}
```
:::

`orm.Prefetch(path, query)` prefetches only the rows of `query`.
`p.Load(ctx, Post.Tags)` loads a relation into one row.
[`orm.WatchRepeats`](/orm/observability#n-1-queries) finds the N+1s you
missed.

## Raw SQL

`Post.Objects.Raw(sql, args…)` reads the model's rows from your own SQL;
`orm.Raw[R]` reads any struct. See [Raw SQL](/orm/databases#raw-sql).

::: vuka
```vuka
popular := Post.Objects.Raw("SELECT * FROM posts WHERE views > ?", 100).All(ctx)?
```
:::

::: go
```go
popular, err := Posts.Raw("SELECT * FROM posts WHERE views > ?", 100).All(ctx)
```
:::

## Earlier releases

Vuka once had a query layer of its own: `Post.Objects` an `orm.Query[Post]`,
field references typed by their model (`vuka.Pred[Post]`), package `orm/fn`,
`orm.Into`, terminals returning `Result`. It still compiles for code that
names it, and goes in the next minor release; the forms above replace it.
