# Expressions and functions

Package `github.com/vuka-lang/orm/fn` is SQL's expressions over field
references, typed. Each gives an `orm.Term[T, V]` — computed by the database,
for model `T`, of Go type `V` — whose predicates go in `Filter` beside the
references', whose `Asc`/`Desc` go in `OrderBy`, and whose `As("name")` names
it for `Annotate`, `Having` and `Values`.

```vuka
Post.Objects.Filter(fn.Lower(Post.Title).Eq("go tooling"))
Post.Objects.Filter(fn.Num(Post.Likes).GtF(fn.Num(Post.Views).Div(2)))     // likes > views / 2
Post.Objects.Filter(fn.Year(Post.Published).Eq(2025))
Post.Objects.OrderBy(fn.Length(Post.Title).Desc())
Post.Objects.Update(ctx, orm.AssignF(Post.Views, fn.Num(Post.Views).Add(1)))?
```

**Go:** a computed condition is a string of SQL, `"likes > views / 2"`,
checked by nobody until the database reads it.

**Vuka adds** terms typed by their result: `fn.Length(Post.Title)` is an
`OrderedTerm[Post, int64]`, so `.Eq("x")` doesn't compile, and `Contains` is
there only on text.

## Terms

| Term | Of | Predicates and operators |
|---|---|---|
| `orm.Term[T, V]` | any | `Eq`, `Ne`, `In`, `IsNull`, `NotNull`, `EqF`, `NeF`, `Asc`, `Desc`, `As` |
| `orm.OrderedTerm[T, V]` | numbers, strings | those, and `Lt`, `Le`, `Gt`, `Ge`, `Range(lo, hi)`, `LtF`…`GeF`, `Add`, `Sub`, `Mul`, `Div` |
| `orm.StringTerm[T, V]` | text | those, and `Contains`, `IContains`, `StartsWith`, `IStartsWith`, `EndsWith`, `IEndsWith`, `IExact` |
| `orm.CompareTerm[T, V]` | times | `Eq`…, `Lt`, `Le`, `Gt`, `Ge` |

The `…F` forms compare with another column or term: `fn.Num(Post.Likes).GtF(Post.Views)`.

| `fn.` | |
|---|---|
| `Num(ref)`, `Str(ref)`, `Time(ref)`, `Val(ref)` | a reference as a term, for its operators: `fn.Str(Post.Body).IContains("go")` |
| `Lit[T](v)` | a value |
| `Lower`, `Upper`, `Trim` | text |
| `Length` | `int64` |
| `Abs` | numbers |
| `Year`, `Month`, `Day` | `int64` |
| `Date` | the date as `YYYY-MM-DD` text |
| `Coalesce(ref, rest…)` | the first that isn't NULL |
| `Cast[R](ref, orm.AsText)` | a cast |
| `SQL[R](template, ref, rest…)` | your own SQL: `fn.SQL[int]("{0} * 2", Post.Likes)` |
| `Call[R](f, ref, rest…)` | a [custom function](#custom-functions) as a term |

## Aggregates

`fn.Count(ref)`, `fn.Sum`, `fn.Avg` (a `float64`), `fn.Min`, `fn.Max`, each
with `.Filter(pred)` and `.Distinct()`:

```vuka
type Stats struct {
	Author string
	Posts  int64
	Liked  int64
}

rows := Post.Objects.Values[Stats](ctx,
	orm.Into(Stats.Author, Post.Author.Name),
	orm.Into(Stats.Posts, fn.Count(Post.ID)),
	orm.Into(Stats.Liked, fn.Count(Post.ID).Filter(Post.Likes.Ge(5))))?
```

See [Values and aggregates](/orm/queries#values-and-aggregates).

## Case and Switch

```vuka
heat := fn.Case(fn.When(Post.Views.Ge(100), "hot"), fn.When(Post.Views.Ge(20), "warm")).Else("cold").As("heat")
score := fn.Switch(Post.Author.Name).Case("ada", 1).Case("bob", 2).Else(0)

type Heat struct {
	Title string
	Heat  string
}

hs := Post.Objects.Annotate(heat).Values[Heat](ctx, orm.Into(Heat.Title, Post.Title), orm.Into(Heat.Heat, heat))?
twos := Post.Objects.Filter(fn.Num(score).Eq(2)).All(ctx)?
```

`fn.When(pred, value)` takes a predicate of the model; every branch's value
has the type of the first.

## Subqueries

| | |
|---|---|
| `fn.Subquery[T](query, col)` | a column of another query's first row |
| `fn.Exists[T](query)` | whether another query has rows |
| `fn.In(ref, query, col)` | `ref IN (SELECT col …)` |
| `fn.OuterEq(inner, outer)` | inside a subquery: a column equal to the outer row's |
| `fn.FilteredRelation(rel, pred).As(name)` | a relation joined with a condition: `.Count()`, `.Field(ref)` |

```vuka
// each user's most-viewed post
latest := Post.Objects.Filter(fn.OuterEq(Post.Author, User.ID)).OrderBy(Post.Views.Desc()).Limit(1)

type Top struct {
	Name string
	Top  string
}

tops := User.Objects.Values[Top](ctx, orm.Into(Top.Name, User.Name), orm.Into(Top.Top, fn.Subquery[User](latest, Post.Title)))?

liked := User.Objects.Filter(fn.Exists[User](Post.Objects.Filter(fn.OuterEq(Post.Author, User.ID), Post.Likes.Ge(20))))
viewed := User.Objects.Filter(fn.In(User.ID, Post.Objects.Filter(Post.Views.Ge(50)), Post.Author))
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

```vuka
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
`function editdist has no SQL on mysql`. Arguments are field references,
other expressions, or plain values, which are always bound as parameters,
never written into the SQL.

Called with field references, a function's comparisons are the engine's
conditions — they go in `Where`, `Order` and `AnnotateAs`. As a term of the
model they go everywhere a reference's do:

```vuka
close := Post.Objects.Where(EditDist(Post.Title, "go").Le(1)).Order(EditDist(Post.Title, "go").Asc())

dist := orm.OrderedTermOf[Post, int](EditDist(Post.Title, "go"))   // or fn.Call[int](EditDist, Post.Title, "go")
near := Post.Objects.Filter(dist.Le(1), Post.Views.Gt(50)).OrderBy(dist.Asc())
```

`.Over(orm.Window{PartitionBy: []string{"author_id"}, OrderBy: []string{"-views"}})`
makes a call a window function's (`row_number`, `rank`, or an aggregate over
a window).

## Custom lookups

`orm.RegisterLookup` adds a lookup to `orm.Q` keys and `Where`, Django's
`register_lookup`. Its function writes the condition per dialect from the
left side's SQL and the value (`rhs()` binds it); `false` means no SQL on
that dialect:

```go
orm.RegisterLookup("unaccent_icontains", func(lhs string, rhs func() string, d orm.Dialect) (string, bool) {
	if d.Name() != "postgres" {
		return "", false
	}
	return "unaccent(" + lhs + ") ILIKE unaccent(" + rhs() + ") ESCAPE '!'", true
}, orm.LookupValue(orm.LikePattern("%", "%")))
```

```vuka
Post.Objects.Where(orm.Q{"title__unaccent_icontains": "cafe"})
```

`orm.LookupValue` maps the value before it is bound (`orm.LikePattern`
escapes a literal into a `LIKE` pattern). An unknown lookup fails with a
suggestion: `unknown lookup "icontain" — did you mean "icontains"?`.

## Go functions on SQLite

`orm/sqlite`'s `Func` registers a Go function as an SQL function on every
SQLite connection opened after it — register in an `init`, before
connecting:

```vuka
var words = orm.OrderedFunc[int]("words", orm.Arity(1))

func init() {
	sqlite.MustFunc("words", func(s string) int { return len(strings.Fields(s)) })
	sqlite.MustFunc("now_ms", func() int64 { return time.Now().UnixMilli() }, sqlite.Volatile())
}

long := Post.Objects.Where(words(Post.Body).Ge(5)).All(ctx)?
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

```go
m.CreateFunction{
	Name:    "initials",
	Params:  m.Dialects{Postgres: "s text", MySQL: "s VARCHAR(255)"},
	Returns: m.Dialects{Postgres: "text", MySQL: "VARCHAR(255)"},
	Body:    m.Dialects{Postgres: "SELECT upper(left(s, 1))", MySQL: "RETURN UPPER(LEFT(s, 1))"},
}
```

It is `IMMUTABLE` / `DETERMINISTIC` unless `Volatile`; `Language` picks a
Postgres language (`plpgsql`). Extension functions (`levenshtein`,
`unaccent`) come with `m.CreateExtension{Name: "fuzzystrmatch"}`.
