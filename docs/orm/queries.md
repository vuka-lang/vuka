# Queries

[[toc]]

## Introduction

You read and write rows through QuerySets. `orm.Objects[Post]()`, the model's
manager, starts an `orm.QuerySet[Post]`: a lazy, immutable query. Each method
returns a new one, and nothing runs until a terminal (`All`, `Get`, `Count`, …)
takes a context.

<!-- go:context
type User struct {
	orm.Model[User]
	ID    int64
	Name  string
	Posts []Post
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
	Status    string
	Author    orm.FK[User]
	Tags      orm.M2M[Tag]
	Published vuka.Option[time.Time]
	Views     int
}

func (p *Post) Schema(s *orm.Schema) {
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "posts"})
}

func PostLive() orm.QuerySet[Post] {
	return orm.Objects[Post]().Filter(PostColumns.Published.IsNull(false))
}

var (
	slug              = "hello-go"
	p                 Post
	golang, tools, db Tag
	r                 *http.Request
)
-->

The samples use `PostColumns` (`f`), the typed fields `ormgen` writes
([Models](/orm/models#plain-go)). Every condition is also an engine string,
`orm.Q{"title__contains": "Go"}`, and terminals return `(T, error)`. Values,
aggregates and pagination take the engine's names in Go:
`orm.Objects[Post]().Annotate("n", orm.Count("id")).Values[Stats]("author__name", "n").All(ctx)`,
`orm.Paginate(ctx, qs, r, orm.Sortable("views"))`.

## Your First Query

This fragment builds a query of recent posts, then reads them and counts them:

<!-- go:fragment -->
```go
f := PostColumns
recent := orm.Objects[Post]().
	Filter(f.Title.Contains("Go"), f.Author().Name.Eq("ada")).
	Exclude(f.Published.IsNull(true)).
	OrderBy(f.Views.Desc(), f.ID.Asc()).
	Limit(10)

posts, err := recent.All(ctx) // []Post
if err != nil {
	return err
}
n, err := recent.Count(ctx) // int64
```

## Filtering Rows {#conditions}

A model's typed field, `PostColumns.Title`, is written beside the model by
`orm generate` (`//go:generate go run github.com/vuka-lang/orm/v2/cmd/ormgen`). It gives conditions (`orm.Cond`) and orderings:

| Field | Conditions |
|---|---|
| any | `Eq`, `Ne`, `In(vs…)`, `Range(lo, hi)`, `IsNull(bool)`, `InQuery(q)` |
| numbers, times | those, and `Lt`, `Lte`, `Gt`, `Gte` |
| strings | those, and `Contains`, `IContains`, `StartsWith`, `IStartsWith`, `EndsWith`, `IEndsWith`, `IExact` |
| any | `Asc()`, `Desc()` for `OrderBy`; `Name()`, the lookup path; `Expr()`, the column as an [expression](/orm/expressions) |

### Combining Conditions

Fields go on through relations — `PostColumns.Author().Name`, `PostColumns.Tags().Name` (all
relation hops are methods: `PostColumns.Author().Posts().Title`) — and the query joins
what they cross. Combine conditions with `orm.And`, `orm.Or` and
`orm.Not`; several in one `Filter` are and-ed; an `orm.Q{"title__icontains":
"go"}` is a condition too:

<!-- go:fragment -->
```go
f := PostColumns
q := orm.Objects[Post]().Filter(orm.Or(f.Views.Gt(100), orm.And(f.Tags().Name.Eq("go"), f.Published.IsNull(false))))
```

### Shaping the Query

These methods narrow, order and cut the query:

| | |
|---|---|
| `Filter(conds…)`, `Exclude(conds…)` | rows matching, not matching |
| `OrderBy(``PostColumns.Views.Desc()``, …)`, `Order(terms…)` | the order (names, or [expressions](/orm/expressions)' `Asc`/`Desc`); the model's `Meta.Ordering` otherwise |
| `Limit(n)`, `Offset(n)`, `Distinct()` | |
| `Unfiltered()` | without the conditions so far |

`orm.Objects[Post](schema)` is the same manager on another schema (a
tenant's names set).

## Reading Rows {#reading}

A terminal runs the query:

| Terminal | Returns |
|---|---|
| `All(ctx)` | `([]T, error)` |
| `Get(ctx, conds…)` | `(T, error)`: the one row matching; fails with `orm.ErrNotFound` or `orm.ErrMultipleFound` |
| `First(ctx)` | `(T, error)`: the first row in order; `orm.ErrNotFound` when there is none |
| `Count(ctx)`, `Exists(ctx)` | `(int64, error)`, `(bool, error)` |
| `Iter(ctx)` | `iter.Seq2[T, error]`: rows one at a time |

`orm.Optional(q.First(ctx))` (or of `Get`) is the row as a `vuka.Option[T]`
and an error: `None` where there is no row, other errors as they are.

### Handling Missing Rows

`errors.Is` tells the kinds apart:

<!-- go:fragment -->
```go
f := PostColumns
p, err := orm.Objects[Post]().Get(ctx, f.Slug.Eq(slug))
switch {
case errors.Is(err, orm.ErrNotFound):
	fmt.Println("no such post")
case err != nil:
	return err
default:
	fmt.Println(p.Title)
}

first, err := PostLive().First(ctx) // orm.ErrNotFound when there is none
switch {
case errors.Is(err, orm.ErrNotFound):
	fmt.Println("nothing published")
case err != nil:
	return err
default:
	fmt.Println("latest published:", first.Title)
}

for p, err := range orm.Objects[Post]().Filter(f.Views.Gte(50)).Iter(ctx) {
	if err != nil {
		return err
	}
	fmt.Println(p.Title)
}
```

## Values and Aggregates {#values-and-aggregates}

`Values[R](names…)` reads columns, paths and annotations into `R`: a
struct (its fields matched by name, or by an `orm:"author__name"` path),
`map[string]any`, `[]any`, or a scalar for one name. An aggregate
annotation groups by the other names. This function reads per-author totals
into a struct:

```go
type Stats struct {
	Author string `orm:"author__name"`
	Posts  int64
	Total  int
}

func authorStats(ctx context.Context) ([]Stats, error) {
	return orm.Objects[Post]().Annotate("posts", orm.Count("id")).Annotate("total", orm.Sum("views")).
		OrderBy(PostColumns.Author().Name.Asc()).Values[Stats]("author__name", "posts", "total").All(ctx)
}
```

`Aggregate` computes values over the whole query:

<!-- go:fragment -->
```go
totals, err := orm.Objects[Post]().Aggregate(ctx, orm.Count("author").Distinct().As("authors"), orm.Max("views").As("top"))
if err != nil {
	return err
}
fmt.Println(totals.Int("authors"), totals.Int("top"))
```

### Grouping

`GroupBy` and `Having` group by fields and filter on an aggregate:

<!-- go:fragment -->
```go
prolific, err := orm.Objects[Post]().Annotate("n", orm.Count("id")).GroupBy(PostColumns.Author().String()).
	Having(orm.Q{"n__gt": 1}).Values[int64]("n").All(ctx)
```

Aggregates are `orm.Count`, `orm.Sum`, `orm.Avg`, `orm.Min`, `orm.Max`, each
with `.Filter(cond)` (over the matching rows) and `.Distinct()`; see
[Expressions](/orm/expressions).

## Paginating Results {#pagination}

`orm.Paginate` reads a page and the total, sorting and searching only by the
fields you allow:

<!-- go:fragment -->
```go
f := PostColumns
page, err := orm.Paginate(ctx, orm.Objects[Post]().QuerySet, orm.PageFrom(r.URL.Query()),
	orm.Sortable(f.Views.Name(), f.Published.Name()),
	orm.Searchable(f.Title.Name()),
	orm.DefaultSort(f.Views.Desc()))
// page.Items, page.Total, page.Page, page.Size, page.Pages
```

`orm.PageFrom` reads `page`, `size`, `sort` (`-views` for descending) and
`q` from query parameters; `orm.PageRequest{Page, Size, Sort, Search}` is the
same by hand. Sizes default to 20, at most 100 (`orm.DefaultSize(n)`,
`orm.MaxSize(n)`).

`qs.CursorPage(ctx, after, before, first, last)` reads a page by cursor
instead — the first rows after a row's cursor, or the last before one —
returning the rows, each row's opaque cursor and whether rows go on past
the page. Its cursors are keyset cursors when the query orders by the
model's own non-null columns (the primary key added to break ties), so a
page costs the same however deep, and positions otherwise; a cursor of
another ordering is `orm.ErrCursor`. It is what a GraphQL connection pages
with ([Pagination](/web/graphql#pagination)).

## Writing Rows {#writing}

These calls insert, update and delete:

| | |
|---|---|
| `p.Save(ctx)` | insert or update one row |
| `orm.Objects[Post]().Create(ctx, &p)`, `BulkCreate(ctx, []*Post{…})` | insert |
| `GetOrCreate(ctx, orm.Q{…}, orm.Defaults{…})` | `(row, created, err)`: the row matching, else one inserted with the defaults |
| `UpdateOrCreate(ctx, orm.Q{…}, orm.Defaults{…})` | the same, updating a found row with the defaults |
| `Update(ctx, orm.Set{…})` | update the matching rows; `(int64, error)` |
| `Delete(ctx)` | delete the matching rows; `(int64, error)` |

This fragment gets or creates a tag, then updates and deletes posts in bulk:

<!-- go:fragment -->
```go
golang, created, err := orm.Objects[Tag]().GetOrCreate(ctx, orm.Q{"name": "go"}, nil)
if err != nil {
	return err
}
fmt.Println(golang.ID, created)

f, posts := PostColumns, orm.Objects[Post]()
if _, err := posts.Filter(f.Views.Lt(5)).Update(ctx, orm.Set{"status": "archived"}); err != nil {
	return err
}
if _, err := posts.Filter(f.Tags().Name.Eq("go")).Update(ctx, orm.Set{"views": f.Views.Add(1)}); err != nil {
	return err
}
if _, err := posts.Filter(f.Published.IsNull(true)).Delete(ctx); err != nil {
	return err
}
```

### Updating With Expressions

`orm.Set` maps columns to values or expressions of the row's own columns,
computed by the database. A number field's `Add`, `Sub`, `Mul` and `Div`
are such expressions, typed (`orm.NumExpr`): `f.Views.Add(1).Mul(2)` is
`(views + 1) * 2`, and `f.Views.Add("x")` doesn't compile.

### Watching Writes

`orm.Objects[Post]().OnChange(fn)` calls `fn` after each write to the model's
rows, and returns the function that stops it.

## Working With Relations {#relations}

A many-to-many's links change through the field or the row:

<!-- go:fragment -->
```go
if err := p.Tags.Add(ctx, &golang, &tools); err != nil {
	return err
}
if err := p.Tags.Remove(ctx, &tools); err != nil {
	return err
}
if err := p.Tags.Set(ctx, &golang); err != nil { // its only links
	return err
}
if err := p.Add(ctx, "tags", &db); err != nil { // the same, through the row
	return err
}
```

### Eager Loading

Reading a relation row by row is an N+1: one query for the posts, one per
post for its author. `SelectRelated` joins a foreign key into the query;
`PrefetchRelated` reads a relation's rows in one more query, for every row
at once:

<!-- go:fragment -->
```go
f := PostColumns
posts, err := orm.Objects[Post]().
	SelectRelated(f.Author().String()). // JOIN users
	PrefetchRelated(f.Tags()).          // one query for every post's tags
	All(ctx)
if err != nil {
	return err
}
for _, p := range posts {
	fmt.Println(p.Author.Related().Name, len(p.Tags.Rows()))
}

goTags := orm.Objects[Post]().PrefetchRelated(orm.Prefetch("tags", orm.Objects[Tag]().Filter(TagColumns.Name.Eq("go"))))
```

`orm.Prefetch(path, query)` prefetches only the rows of `query`.
`p.Load(ctx, PostColumns.Tags())` loads a relation into one row.
`orm.LoadRelated(ctx, posts, "author", "tags")` loads relations into rows
read already (a slice of pointers to them), one query per relation for all
of them: what a GraphQL field holding a relation resolves through.
[`orm.WatchRepeats`](/orm/observability#n-1-queries) finds the N+1s you
missed.

## Raw SQL

`orm.Objects[Post]().Raw(sql, args…)` reads the model's rows from your own SQL,
and `orm.Raw[R]` reads any struct. See [Raw SQL](/orm/databases#raw-sql) for
more:

<!-- go:fragment -->
```go
popular, err := orm.Objects[Post]().Raw("SELECT * FROM posts WHERE views > ?", 100).All(ctx)
```


## Related Counts and Model Conditions

This query keeps every category, including categories with no products on sale.
Reverse paths come from the foreign keys' `related` tags:

```go
type Category struct {
    orm.Model[Category]
    ID int64
    Name string
}
type Shelf struct {
    orm.Model[Shelf]
    ID int64
    Category orm.FK[Category] `orm:"related:shelves"`
}
type Product struct {
    orm.Model[Product]
    ID int64
    Shelf orm.FK[Shelf] `orm:"related:products"`
    Status int
    SaleEnds time.Time
}
type CategoryCount struct {
    ID int64
    Name string
    OnSaleCount int64 `db:"on_sale_count"`
}
func (p ProductFields) OnSale() orm.Cond {
    return orm.And(p.Status.Between(2, 3), p.SaleEnds.Gte(orm.Today().AddDays(-3)))
}
func CategoryCounts(ctx context.Context) ([]CategoryCount, error) {
    c := CategoryColumns
    products := c.Shelves().Products()
    return orm.Objects[Category]().
        Annotate("on_sale_count", products.Where(products.OnSale()).Count()).
        OrderBy(c.Name.Asc()).
        Values[CategoryCount]("id", "name", "on_sale_count").All(ctx)
}
```

MySQL uses `CASE WHEN`; Postgres uses `FILTER`. Aliases are shortened here:

::: code-group
```sql [MySQL]
SELECT c.id, c.name,
       COUNT(DISTINCT CASE WHEN p.status BETWEEN ? AND ?
           AND p.sale_ends >= DATE_SUB(CURDATE(), INTERVAL 3 DAY)
           THEN p.id END) AS on_sale_count
FROM categories c
LEFT JOIN shelves s ON s.category_id = c.id
LEFT JOIN products p ON p.shelf_id = s.id
GROUP BY c.id, c.name
ORDER BY c.name
```
```sql [Postgres]
SELECT c.id, c.name,
       COUNT(DISTINCT p.id) FILTER (WHERE p.status BETWEEN $1 AND $2
           AND p.sale_ends >= CURRENT_DATE - 3) AS on_sale_count
FROM categories c
LEFT JOIN shelves s ON s.category_id = c.id
LEFT JOIN products p ON p.shelf_id = s.id
GROUP BY c.id, c.name
ORDER BY c.name
```
:::

Both bind `2, 3`. SQLite uses `CASE WHEN` and `date(date('now'), '-3 day')`.
`Count` counts distinct target primary keys. `CountRows` counts raw joined
rows, including the empty row of a LEFT JOIN. `Exists` uses a correlated
`EXISTS` query. Relation `Where` narrows the aggregate; query `Filter`
narrows parent rows.

`Sum(field)`, `Avg(field)`, `Min(field)`, and `Max(field)` aggregate each
related primary key once. With two or more relation aggregates, each gets
its own correlated subquery, so one relation cannot multiply another's
rows. Value aggregates deduplicate primary keys in their own subquery.

A typed annotation handle keeps its name with its expression:

<!-- go:fragment -->
```go
products := CategoryColumns.Shelves().Products()
n := orm.Annotation[int64]("on_sale_count", products.Where(products.OnSale()).Count())
q := orm.Objects[Category]().Annotate(n).Filter(n.Gt(0)).Order(n.Desc())
query, args, err := q.Values[CategoryCount]("id", "name", "on_sale_count").SQL(ctx)
_, _, _ = query, args, err
```

`orm.Ref[int64]("on_sale_count")` refers to an existing annotation. Filters
on aggregate annotations become `HAVING`. Unknown annotation names report
spelling suggestions. `SQL(ctx)` builds SQL and arguments for the context's
database without executing it. `Values` checks both directions: every
selected column needs a destination field, and every destination field
needs a selected column. The existing model-term annotation interface is
now named `orm.ModelAnnotation[T]`.
