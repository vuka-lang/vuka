# Expressions & Functions

[[toc]]

## Introduction

Expressions let a query compute in SQL: lower-case a title, add two columns,
bucket rows with `Case`, compare with a subquery, or call a function of your
own database. You write them in Go, typed, and the ORM writes the SQL for the
dialect the query runs on.

<!-- go:context
type User struct {
	orm.Model[User]
	ID   int64
	Name string
}

type Post struct {
	orm.Model[Post]
	ID        int64
	Title     string
	Slug      string
	Body      string
	Author    orm.FK[User]
	Published time.Time
	Views     int
	Likes     int
}

func (p *Post) Schema(s *orm.Schema) {
	s.Field(&p.Author, orm.Rel{OnDelete: orm.Cascade, Related: "posts"})
}

var slug = "hello-go"
-->

## Writing Expressions {#expressions}

SQL's expressions are the engine's: a field's `Expr()` is its column
(`orm.F("title")`), and functions, `Case`, `Switch`, `Cast`, `Value` and
`SQL` take expressions, values or other calls — and typed fields as they
are, `orm.Lower(PostColumns.Title)`. A typed call compares by its
result's type, so `orm.Length(…).Eq("x")` doesn't compile. This fragment
uses a few of them:

<!-- go:fragment -->
```go
f, posts := PostColumns, orm.Objects[Post]()
title := f.Title.Expr()
named := posts.Filter(orm.Lower(title).Eq("go tooling"))
liked := posts.Filter(orm.Q{"likes__gt": f.Views.Div(2)}) // likes > views / 2
of2025 := posts.Filter(orm.Year(f.Published.Expr()).Eq(2025))
byLength := posts.Order(orm.Length(title).Desc())
if _, err := posts.Filter(f.Slug.Eq(slug)).Update(ctx, orm.Set{"views": f.Views.Add(1)}); err != nil {
	return err
}
```

### Arithmetic

A number field (an `orm.NumberField`, what `orm generate` writes for an
integer or float column) does arithmetic: `Add`, `Sub`, `Mul` and `Div`
take a value of the field's type and give an `orm.NumExpr`, an
`OrderedExpr` that goes on (`f.Views.Add(1).Mul(2)`); `orm.Num[int](e)`
makes one of any expression. `Div` divides as the database does: whole
numbers for integers on SQLite and Postgres.

### Functions

The `orm` package provides these:

| `orm.` | |
|---|---|
| `F(name)`, `Value(v)` | a column, or an annotation; a value |
| `Lower`, `Upper`, `Trim` | text, a `TextExpr` |
| `Length` | `int64` |
| `Abs` | numbers |
| `Year`, `Month`, `Day` | `int` |
| `Date` | the date as `YYYY-MM-DD` text |
| `Coalesce(e, rest…)` | the first that isn't NULL |
| `Cast(e, orm.AsText)` | a cast |
| `SQL(template, args…)` | your own SQL: `orm.SQL("{0} * {1}", PostColumns.Likes.Expr(), PostColumns.Views.Expr())` |
| `Where(e, lookup, v)` | a condition on any expression: `orm.Where(orm.SQL("{0} - {1}", views, likes), "gte", 49)` |

### Typed Calls

A typed call is a `TypedExpr[R]` (`Eq`, `Ne`, `In`, `IsNull`, `NotNull`,
`Asc`, `Desc`, `As`), an `OrderedExpr[R]` (and `Lt`, `Le`, `Gt`, `Ge`,
`Between`) or a `TextExpr` (and `Contains`, `IContains`, `StartsWith`,
`EndsWith`, `Like`). `Asc` and `Desc` go in `Order`; `orm.Asc(e)` and
`orm.Desc(e)` make any expression a term.

## Aggregates

`orm.Count(field)`, `orm.Sum`, `orm.Avg`, `orm.Min`, `orm.Max`, each with
`.Filter(cond)`, `.Distinct()` and `.As(name)`. This function counts each author's
posts, and separately the liked ones:

```go
type Stats struct {
	Author string `orm:"author__name"`
	Posts  int64
	Liked  int64
}

func likedStats(ctx context.Context) ([]Stats, error) {
	return orm.Objects[Post]().Annotate("posts", orm.Count("id")).
		Annotate("liked", orm.Count("id").Filter(PostColumns.Likes.Gte(5))).
		Values[Stats]("author__name", "posts", "liked").All(ctx)
}
```

See [Values and Aggregates](/orm/queries#values-and-aggregates).

## Conditional Expressions {#case-and-switch}

`orm.Case` picks a value by the first condition that holds, and `orm.Switch`
by the value of an expression. The fragment below reads its results into this
struct:

```go
type Heat struct {
	Title string
	Heat  string
}
```

Then it annotates each post with its heat and filters on a switch:

<!-- go:fragment -->
```go
f, posts := PostColumns, orm.Objects[Post]()
heat := orm.Case(orm.When(f.Views.Gte(100), "hot"), orm.When(f.Views.Gte(20), "warm")).Else("cold")
score := orm.Switch(f.Author.Name.Expr()).Case("ada", 1).Case("bob", 2).Else(0)

hs, err := posts.Annotate("heat", heat).Values[Heat]("title", "heat").All(ctx)
if err != nil {
	return err
}
twos, err := posts.Filter(orm.Where(score, "exact", 2)).All(ctx)
if err != nil {
	return err
}
```

`orm.When(cond, value)` takes any condition.

## Subqueries

| | |
|---|---|
| `orm.Subquery(values)` | a column of another query's first row |
| `orm.Exists(query)` | whether another query has rows |
| `field.InQuery(values)` | `field IN (SELECT …)` |
| `orm.OuterRef(name)` | inside a subquery: the outer row's column |

The fragment below reads each user's most-viewed post into this struct:

```go
type Top struct {
	Name string
	Top  string
}
```

It also finds users through `Exists` and `InQuery`:

<!-- go:fragment -->
```go
posts, users := orm.Objects[Post](), orm.Objects[User]()
// each user's most-viewed post
latest := posts.Filter(orm.Q{"author": orm.OuterRef("id")}).OrderBy(PostColumns.Views.Desc()).Limit(1).Values[string]("title")

tops, err := users.Annotate("top", orm.Subquery(latest)).Values[Top]("name", "top").All(ctx)
if err != nil {
	return err
}

liked := users.Filter(orm.Exists(posts.Filter(orm.Q{"author": orm.OuterRef("id")}, PostColumns.Likes.Gte(20))))
viewed := users.Filter(UserColumns.ID.InQuery(posts.Filter(PostColumns.Views.Gte(50)).Values[int64]("author")))
```

## Custom Functions {#custom-functions}

A database function is defined once, with its SQL per dialect, and called
like a Go function. The constructor picks the result type's comparisons:

| Constructor | Call's type | Comparisons |
|---|---|---|
| `orm.Func[R](name, opts…)` | `TypedExpr[R]` | `Eq`, `Ne`, `In`, `IsNull`, `NotNull`, `Asc`, `Desc`, `As`, `Over` |
| `orm.OrderedFunc[R](name, opts…)` | `OrderedExpr[R]` | those, and `Lt`, `Le`, `Gt`, `Ge`, `Between` |
| `orm.TextFunc(name, opts…)` | `TextExpr` | those, and `Contains`, `IContains`, `StartsWith`, `EndsWith`, `Like` |
| `orm.Aggregate[R](name, opts…)` | an aggregate's | as `Func` |

Each constructor takes these options:

| Option | |
|---|---|
| `orm.Template(dialect, sql)` | its SQL on a dialect: `{0}`, `{1}` the arguments, `{*}` all; `""` every dialect not named |
| `orm.Arity(n)` | checks the number of arguments |
| `orm.IsAggregate()` | an aggregate: a condition on it is `HAVING`'s |
| `.Transform()` | (on a one-argument function) a key step before any lookup: `orm.Q{"name__unaccent__icontains": "jose"}` |

These declarations define an edit-distance function and two more:

```go
var (
	EditDist = orm.OrderedFunc[int]("editdist", orm.Arity(2),
		orm.Template("postgres", "levenshtein({0}, {1})"), // fuzzystrmatch
		orm.Template("sqlite", "editdist({0}, {1})"))       // a Go function, below
	Unaccent = orm.TextFunc("unaccent", orm.Arity(1),
		orm.Template("postgres", "unaccent({0})"), orm.Template("", "{0}")).Transform()
)
```

A function with no `Template` is called as `name(args…)`; one with templates
is written only where one applies — elsewhere its query fails with
`function editdist has no SQL on mysql`. Arguments are expressions (a
field's `Expr()`) or plain values, which are always bound as parameters,
never written into the SQL. Its comparisons are conditions, its `Asc` and
`Desc` terms of `Order`, and `Annotate` names it:

<!-- go:fragment -->
```go
dist := EditDist(PostColumns.Title.Expr(), "go")
close, err := orm.Objects[Post]().Filter(dist.Le(1), PostColumns.Views.Gt(50)).Order(dist.Asc()).All(ctx)
```

### Window Functions

`.Over(orm.Window{PartitionBy: []string{"author_id"}, OrderBy: []string{"-views"}})`
makes a call a window function's (`row_number`, `rank`, or an aggregate over
a window).

## Custom Lookups {#custom-lookups}

`orm.RegisterLookup` adds a lookup to `orm.Q` keys and `Where`. Its function writes the condition per dialect from the
left side's SQL and the value (`rhs()` binds it); `false` means no SQL on
that dialect:

<!-- go:fragment -->
```go
orm.RegisterLookup("unaccent_icontains", func(lhs string, rhs func() string, d orm.Dialect) (string, bool) {
	if d.Name() != "postgres" {
		return "", false
	}
	return "unaccent(" + lhs + ") ILIKE unaccent(" + rhs() + ") ESCAPE '!'", true
}, orm.LookupValue(orm.LikePattern("%", "%")))
```

The new lookup then works in any `orm.Q` key:

<!-- go:fragment -->
```go
cafes := orm.Objects[Post]().Filter(orm.Q{"title__unaccent_icontains": "cafe"})
```

`orm.LookupValue` maps the value before it is bound (`orm.LikePattern`
escapes a literal into a `LIKE` pattern). An unknown lookup fails with a
suggestion: `unknown lookup "icontain" — did you mean "icontains"?`.

## Go Functions on SQLite {#go-functions-on-sqlite}

`orm/sqlite`'s `Func` registers a Go function as an SQL function on every
SQLite connection opened after it — register in an `init`, before
connecting:

```go
var words = orm.OrderedFunc[int]("words", orm.Arity(1))

func init() {
	sqlite.MustFunc("words", func(s string) int { return len(strings.Fields(s)) })
	sqlite.MustFunc("now_ms", func() int64 { return time.Now().UnixMilli() }, sqlite.Volatile())
}

func longPosts(ctx context.Context) ([]Post, error) {
	return orm.Objects[Post]().Filter(words(PostColumns.Body.Expr()).Ge(5)).All(ctx)
}
```

Parameters and results are strings, `[]byte`, bools, numbers, `time.Time`,
`any` or pointers to them (NULL in and out), an `error` result after the
value. They are deterministic unless `sqlite.Volatile()`, and run unindexed;
scalar functions only. `sqlite.Func` returns the error that `MustFunc`
panics with.

## SQL Functions in a Migration {#sql-functions-in-a-migration}

Postgres and MySQL run functions written in SQL. Create them in a
[migration](/orm/migrations) with `migration.CreateFunction` — unapplying
drops them; SQLite gets nothing, so use a Go function there:

<!-- go:context
import m "github.com/vuka-lang/orm/v2/migration"

var _ = m.F
-->

```go
var initials = m.CreateFunction{
	Name:    "initials",
	Params:  m.Dialects{Postgres: "s text", MySQL: "s VARCHAR(255)"},
	Returns: m.Dialects{Postgres: "text", MySQL: "VARCHAR(255)"},
	Body:    m.Dialects{Postgres: "SELECT upper(left(s, 1))", MySQL: "RETURN UPPER(LEFT(s, 1))"},
}
```

It is `IMMUTABLE` / `DETERMINISTIC` unless `Volatile`; `Language` picks a
Postgres language (`plpgsql`). Extension functions (`levenshtein`,
`unaccent`) come with `m.CreateExtension{Name: "fuzzystrmatch"}`.


## Portable Dates

Date expressions compose in Go and compile for the connection's dialect.
Generated timestamp lookups are `orm.TimeField` values; the `orm.DateField`
model attribute still describes a DATE column.

| Expression | MySQL | Postgres | SQLite |
|---|---|---|---|
| `orm.Today()` | `CURDATE()` | `CURRENT_DATE` | `date('now')` |
| `orm.Now()` | `NOW()` | `now()` | `datetime('now')` |
| `orm.Today().AddDays(-3)` | `DATE_SUB(CURDATE(), INTERVAL 3 DAY)` | `CURRENT_DATE + -3` | `date(date('now'), '-3 day')` |
| `orm.Today().AddMonths(2)` | `DATE_ADD(CURDATE(), INTERVAL 2 MONTH)` | `CURRENT_DATE + interval '2 month'` | `date(date('now'), '+2 month')` |
| `orm.Now().Add(time.Second)` | `DATE_ADD(NOW(), INTERVAL 1 SECOND)` | `now() + interval '1 second'` | `datetime(datetime('now'), '+1 second')` |
| `orm.Date(f)` | `DATE(f)` | `(f)::date` | `DATE(f)` |
| `f.Between(a, b)` | `f BETWEEN a AND b` | same | same |

`AddDays`, `AddMonths`, and `Add(time.Duration)` work on generated timestamp
fields and on `Date(f)`. Durations retain fractional seconds.
`TodayIn(loc)` computes the wall date using `orm.Clock` in Go and binds its
`YYYY-MM-DD` value, independently of the connection's timezone. A nil
location uses UTC. `Date.Of`, `Date.Def`, and the date lookup transform
remain available.
