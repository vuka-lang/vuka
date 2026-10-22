# Expressions and functions

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
SQL's expressions are the engine's: a field's `Expr()` is its column
(`orm.F("title")`), and functions, `Case`, `Switch`, `Cast`, `Value` and
`SQL` take expressions, values or other calls — and typed fields as they
are, `orm.Lower(PostFields.Title)`. A typed call compares by its
result's type, so `orm.Length(…).Eq("x")` doesn't compile:



<!-- go:fragment -->
```go
f, posts := PostFields, orm.Objects[Post]()
title := f.Title.Expr()
named := posts.Filter(orm.Lower(title).Eq("go tooling"))
liked := posts.Filter(orm.Q{"likes__gt": f.Views.Div(2)}) // likes > views / 2
of2025 := posts.Filter(orm.Year(f.Published.Expr()).Eq(2025))
byLength := posts.Order(orm.Length(title).Desc())
if _, err := posts.Filter(f.Slug.Eq(slug)).Update(ctx, orm.Set{"views": f.Views.Add(1)}); err != nil {
	return err
}
```

A number field (an `orm.NumberField`, what `orm generate` writes for an
integer or float column) does arithmetic: `Add`, `Sub`, `Mul` and `Div`
take a value of the field's type and give an `orm.NumExpr`, an
`OrderedExpr` that goes on (`f.Views.Add(1).Mul(2)`); `orm.Num[int](e)`
makes one of any expression. `Div` divides as the database does: whole
numbers for integers on SQLite and Postgres.

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
| `SQL(template, args…)` | your own SQL: `orm.SQL("{0} * {1}", PostFields.Likes.Expr(), PostFields.Views.Expr())` |
| `Where(e, lookup, v)` | a condition on any expression: `orm.Where(orm.SQL("{0} - {1}", views, likes), "gte", 49)` |

A typed call is a `TypedExpr[R]` (`Eq`, `Ne`, `In`, `IsNull`, `NotNull`,
`Asc`, `Desc`, `As`), an `OrderedExpr[R]` (and `Lt`, `Le`, `Gt`, `Ge`,
`Between`) or a `TextExpr` (and `Contains`, `IContains`, `StartsWith`,
`EndsWith`, `Like`). `Asc` and `Desc` go in `Order`; `orm.Asc(e)` and
`orm.Desc(e)` make any expression a term.

## Aggregates

`orm.Count(field)`, `orm.Sum`, `orm.Avg`, `orm.Min`, `orm.Max`, each with
`.Filter(cond)`, `.Distinct()` and `.As(name)`:


```go
type Stats struct {
	Author string `orm:"author__name"`
	Posts  int64
	Liked  int64
}

func likedStats(ctx context.Context) ([]Stats, error) {
	return orm.Objects[Post]().Annotate("posts", orm.Count("id")).
		Annotate("liked", orm.Count("id").Filter(PostFields.Likes.Gte(5))).
		Values[Stats]("author__name", "posts", "liked").All(ctx)
}
```

See [Values and aggregates](/orm/queries#values-and-aggregates).

## Case and Switch


```go
type Heat struct {
	Title string
	Heat  string
}
```

<!-- go:fragment -->
```go
f, posts := PostFields, orm.Objects[Post]()
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


```go
type Top struct {
	Name string
	Top  string
}
```

<!-- go:fragment -->
```go
posts, users := orm.Objects[Post](), orm.Objects[User]()
// each user's most-viewed post
latest := posts.Filter(orm.Q{"author": orm.OuterRef("id")}).OrderBy(PostFields.Views.Desc()).Limit(1).Values[string]("title")

tops, err := users.Annotate("top", orm.Subquery(latest)).Values[Top]("name", "top").All(ctx)
if err != nil {
	return err
}

liked := users.Filter(orm.Exists(posts.Filter(orm.Q{"author": orm.OuterRef("id")}, PostFields.Likes.Gte(20))))
viewed := users.Filter(UserFields.ID.InQuery(posts.Filter(PostFields.Views.Gte(50)).Values[int64]("author")))
```

## Custom functions

A database function is defined once, with its SQL per dialect, and called
like a Go function. The constructor picks the result type's comparisons:

| Constructor | Call's type | Comparisons |
|---|---|---|
| `orm.Func[R](name, opts…)` | `TypedExpr[R]` | `Eq`, `Ne`, `In`, `IsNull`, `NotNull`, `Asc`, `Desc`, `As`, `Over` |
| `orm.OrderedFunc[R](name, opts…)` | `OrderedExpr[R]` | those, and `Lt`, `Le`, `Gt`, `Ge`, `Between` |
| `orm.TextFunc(name, opts…)` | `TextExpr` | those, and `Contains`, `IContains`, `StartsWith`, `EndsWith`, `Like` |
| `orm.Aggregate[R](name, opts…)` | an aggregate's | as `Func` |

| Option | |
|---|---|
| `orm.Template(dialect, sql)` | its SQL on a dialect: `{0}`, `{1}` the arguments, `{*}` all; `""` every dialect not named |
| `orm.Arity(n)` | checks the number of arguments |
| `orm.IsAggregate()` | an aggregate: a condition on it is `HAVING`'s |
| `.Transform()` | (on a one-argument function) a key step before any lookup: `orm.Q{"name__unaccent__icontains": "jose"}` |


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
dist := EditDist(PostFields.Title.Expr(), "go")
close, err := orm.Objects[Post]().Filter(dist.Le(1), PostFields.Views.Gt(50)).Order(dist.Asc()).All(ctx)
```

`.Over(orm.Window{PartitionBy: []string{"author_id"}, OrderBy: []string{"-views"}})`
makes a call a window function's (`row_number`, `rank`, or an aggregate over
a window).

## Custom lookups

`orm.RegisterLookup` adds a lookup to `orm.Q` keys and `Where`, Django's
`register_lookup`. Its function writes the condition per dialect from the
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


<!-- go:fragment -->
```go
cafes := orm.Objects[Post]().Filter(orm.Q{"title__unaccent_icontains": "cafe"})
```

`orm.LookupValue` maps the value before it is bound (`orm.LikePattern`
escapes a literal into a `LIKE` pattern). An unknown lookup fails with a
suggestion: `unknown lookup "icontain" — did you mean "icontains"?`.

## Go functions on SQLite

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
	return orm.Objects[Post]().Filter(words(PostFields.Body.Expr()).Ge(5)).All(ctx)
}
```

Parameters and results are strings, `[]byte`, bools, numbers, `time.Time`,
`any` or pointers to them (NULL in and out), an `error` result after the
value. They are deterministic unless `sqlite.Volatile()`, and run unindexed;
scalar functions only. `sqlite.Func` returns the error that `MustFunc`
panics with.

## SQL functions in a migration

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
