# Queries

`Post.Objects` is an `orm.Query[Post]`: a lazy, immutable query. Each method
returns a new one; nothing runs until a terminal (`All`, `Get`, `Count`, …)
takes a context.

```vuka
recent := Post.Objects.
	Filter(Post.Title.Contains("Go"), Post.Author.Name.Eq("ada")).
	Exclude(Post.Published.IsNil()).
	OrderBy(Post.Views.Desc(), Post.ID.Asc()).
	Limit(10)

posts := recent.All(ctx)?          // []Post
n := recent.Count(ctx)?            // int64
```

## Predicates

A [field reference](/features/fields#field-references) gives typed
predicates — a `vuka.Pred[Post]` — and orderings:

| Reference | Predicates |
|---|---|
| any field | `Eq`, `Ne`, `In(vs…)` |
| numbers | those, and `Lt`, `Le`, `Gt`, `Ge` |
| strings | those, and `Contains`, `StartsWith`, `EndsWith` |
| `time.Time` | `Eq`, `Ne`, `In`, `Lt`, `Le`, `Gt`, `Ge` |
| `Option`, pointers | `IsNil`, `NotNil` |
| any | `Asc()`, `Desc()` for `OrderBy` |

References go on through relations — `Post.Author.Name`, `Post.Tags.Name` —
and the query joins what they cross. Combine predicates with `vuka.And`,
`vuka.Or` and `vuka.Not`; several in one `Filter` are and-ed:

```vuka
q := Post.Objects.Filter(vuka.Or(Post.Views.Gt(100), vuka.And(Post.Tags.Name.Eq("go"), Post.Published.NotNil())))
```

A predicate is typed by its model, so `Post.Objects.Filter(User.Name.Eq("x"))`
doesn't compile. Case-insensitive and computed conditions come from
[`orm/fn`](/orm/expressions): `fn.Str(Post.Title).IContains("go")`,
`fn.Lower(Post.Title).Eq("go")`.

| | |
|---|---|
| `Filter(preds…)`, `Exclude(preds…)` | rows matching, not matching |
| `OrderBy(Post.Views.Desc(), …)` | the order; the model's `Meta.Ordering` otherwise |
| `Limit(n)`, `Offset(n)`, `Distinct()` | |
| `Unfiltered()` | without the conditions so far |
| `On(schema)` | the same query on another schema (a tenant's names set) |

## Reading

| Terminal | Returns |
|---|---|
| `All(ctx)` | `Result[[]T]` |
| `Get(ctx, preds…)` | `Result[T]`: the one row matching; fails with `orm.NotFound` or `orm.MultipleFound` |
| `First(ctx)` | `Result[Option[T]]`: the first row in order; an error fails, no row is `None` |
| `Count(ctx)`, `Exists(ctx)` | `Result[int64]`, `Result[bool]` |
| `Iter(ctx)` | `iter.Seq2[T, error]`: rows one at a time |

`Get` fails with the error kind itself, so `match` takes it directly; other
errors are `*orm.Error` values, matched with a guard:

```vuka
match Post.Objects.Get(ctx, Post.Slug.Eq(slug)) {
case Ok(p):
	fmt.Println(p.Title)
case Err(orm.NotFound):
	fmt.Println("no such post")
case Err(e):
	return e
}

match Post.Live().First(ctx) {
case Ok(first):
	match first {
	case Some(p):
		fmt.Println("latest published:", p.Title)
	case None:
		fmt.Println("nothing published")
	}
case Err(e):
	return e
}

for p, err := range Post.Objects.Filter(Post.Views.Ge(50)).Iter(ctx) {
	if err != nil {
		return err
	}
	fmt.Println(p.Title)
}
```

## Values and aggregates

`Values[R]` reads columns into a struct of your own, each field filled by
reference with `orm.Into(dst, src)` — typed end to end: an `int64` field
can't take a string column. A query with an aggregate groups by the other
columns:

```vuka
type Stats struct {
	Author string
	Posts  int64
	Views  int
}

stats := Post.Objects.OrderBy(Post.Author.Name.Asc()).Values[Stats](ctx,
	orm.Into(Stats.Author, Post.Author.Name),   // string ← string
	orm.Into(Stats.Posts, fn.Count(Post.ID)),   // int64 ← COUNT
	orm.Into(Stats.Views, fn.Sum(Post.Views)))? // int ← SUM(int)

total := Post.Objects.Aggregate[Stats](ctx,
	orm.Into(Stats.Posts, fn.Count(Post.Author).Distinct()),
	orm.Into(Stats.Views, fn.Max(Post.Views)))?  // one Stats
```

`Annotate`, `GroupBy` and `Having` name an aggregate and filter on it:

```vuka
n := fn.Count(Post.ID).As("n")
prolific := Post.Objects.Annotate(n).GroupBy(Post.Author).Having(n.Gt(1)).
	Values[Stats](ctx, orm.Into(Stats.Author, Post.Author.Name), orm.Into(Stats.Posts, n))?
```

Aggregates are `fn.Count`, `fn.Sum`, `fn.Avg`, `fn.Min`, `fn.Max`, each with
`.Filter(pred)` (an aggregate over the matching rows) and `.Distinct()`; see
[Expressions](/orm/expressions).

## Pagination

`Paginate` reads a page and the total, sorting and searching only by the
fields you allow:

```vuka
page := Post.Objects.Paginate(ctx, orm.PageFrom(r.URL.Query()),
	orm.SortableBy(Post.Views, Post.Published),
	orm.SearchableBy(Post.Title),
	orm.DefaultSortBy(Post.Views.Desc()))?
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
| `Post.Objects.Create(ctx, &p)`, `BulkCreate(ctx, &a, &b, …)` | insert |
| `GetOrCreate(ctx, row, match…)` | the row whose `match` fields equal `row`'s, else `row` inserted |
| `UpdateOrCreate(ctx, row, match…)` | the same, updating a found row with `row`'s values |
| `Update(ctx, assignments…)` | update the matching rows; `Result[int64]` |
| `Delete(ctx)` | delete the matching rows; `Result[int64]` |

```vuka
golang := Tag.Objects.GetOrCreate(ctx, Tag{Name: "go"}, Tag.Name)?   // orm.Outcome[Tag]
fmt.Println(golang.Row.ID, golang.Created)

Post.Objects.Filter(Post.Views.Lt(5)).Update(ctx, orm.Assign(Post.Status, "archived"))?
Post.Objects.Filter(Post.Tags.Name.Eq("go")).Update(ctx, orm.AssignF(Post.Views, fn.Num(Post.Views).Add(1)))?
Post.Objects.Filter(Post.Published.IsNil()).Delete(ctx)?
```

`orm.Assign(field, value)` sets a value; `orm.AssignF(field, term)` an
expression of the row's own columns, computed by the database.
`Post.Objects.OnChange(fn)` calls `fn` after each write to the model's
rows, and returns the function that stops it.

## Relations

A many-to-many's links change through the field or the row:

```vuka
p.Tags.Add(ctx, &golang, &tools)?
p.Tags.Remove(ctx, &tools)?
p.Tags.Set(ctx, &golang)?               // its only links
p.Add(ctx, Post.Tags, &db)?             // the same, through the row
```

Reading a relation row by row is an N+1: one query for the posts, one per
post for its author. `SelectRelated` joins a foreign key into the query;
`PrefetchRelated` reads a relation's rows in one more query, for every row
at once:

```vuka
posts := Post.Objects.
	SelectRelated(Post.Author).                 // JOIN users
	PrefetchRelated(Post.Tags).                 // one query for every post's tags
	All(ctx)?
for _, p := range posts {
	fmt.Println(p.Author.Related().Name, len(p.Tags.Rows()))
}

goTags := Post.Objects.PrefetchRelated(orm.PrefetchBy(Post.Tags, Tag.Objects.Filter(Tag.Name.Eq("go"))))
```

`orm.PrefetchBy(rel, query)` prefetches only the rows of `query`.
`p.Load(ctx, Post.Tags)` loads a relation into one row.
[`orm.WatchRepeats`](/orm/observability#n-1-queries) finds the N+1s you
missed.

## Raw SQL

`Post.Objects.Raw(ctx, sql, args…)` reads the model's rows from your own SQL;
`orm.Raw[R]` reads any struct. See [Raw SQL](/orm/databases#raw-sql).

```vuka
popular := Post.Objects.Raw(ctx, "SELECT * FROM posts WHERE views > ?", 100)?
```

## The engine's forms

`orm.Query[T]` is the engine's QuerySet as Vuka reads it. Its string forms
stay at hand for what has no reference — reverse relations, an annotation's
name:

| | |
|---|---|
| `orm.P[T](cond)` | any engine condition as a predicate: `orm.P[Post](orm.Q{"likes__lt": Post.Views})` |
| `Where(conds…)`, `Order(terms…)`, `AnnotateAs(aliases…)` | take the engine's conditions, orderings and named expressions |
| `With(func(orm.QuerySet[T]) orm.QuerySet[T])`, `QuerySet()` | every engine method |
| `orm.FieldName(ref)`, `orm.ExprOf(ref)` | a reference's lookup path (`"author__name"`), its expression |

Engine conditions take field references as values: `orm.Q{"likes__lt":
Post.Views}` compares two columns.
