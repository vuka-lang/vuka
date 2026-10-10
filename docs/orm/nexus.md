# Coming from nexus

The ORM was extracted from the [nexus](https://github.com/paulmanoni/nexus)
framework's ORM, `github.com/paulmanoni/nexus/orm`. The engine is the same —
models embedding `orm.Model[T]`, `orm.For[T]()` managers, `Q` lookups,
QuerySets, migrations — with no dependency on nexus.

## What changes

| nexus | vuka-lang/orm |
|---|---|
| `github.com/paulmanoni/nexus/orm` | `github.com/vuka-lang/orm` |
| `nexus makemigrations`, `nexus migrate`, `nexus showmigrations`, `nexus sqlmigrate` | `orm makemigrations`, `orm migrate`, `orm showmigrations`, `orm sqlmigrate` (`go run github.com/vuka-lang/orm/cmd/orm …`) |
| `nexus generate models` | `orm generate` |
| databases from `nexus.toml` `[databases.*]`, bound at boot | `orm.AddDatabase`, or `orm.Declare` + `orm.OpenDatabases` (URLs, `DATABASE_URL`) |
| table routes from `nexus.toml` | `set.Route(table, orm.Route{…})` |
| the `nexus_migrations` table | `orm_migrations`; set `orm.MigrationsTable = "nexus_migrations"` to keep a database's history |
| nexus's error codes | `orm.Error` kinds (below) |
| the debug toolbar's SQL panel | an [`orm.Observe`](/orm/observability) hook |

What stayed in nexus is the integration: binding managers as `nexus.Option`s
at boot, reading databases and routes from `nexus.toml`, the dashboard's model
listing, the toolbar's trace spans, and `Manager.GraphRelation` (a batched
GraphQL field over `nexus.LoadField`). Each is an adapter over the ORM's
seams: `orm.Observe`, `orm.SetFieldOptions`, `orm.Pred` (predicates as data)
and `orm.Ordering`.

## Errors

A framework maps the kinds onto its own model; nexus's mapping:

| orm Kind | nexus code | HTTP |
|---|---|---|
| `NotFound` | `NotFound` | 404 |
| `MultipleFound` | `Conflict` | 409 |
| `Conflict` | `Conflict` (+ `Fields`) | 409 |
| `Invalid` | `InvalidInput` (+ `Fields`) | 422 |
| `Unavailable` | `Unavailable` | 503 |
| none | `Internal` | 500 |

## From the engine to Vuka

Go code keeps working as it is. A Vuka program gets a layer of its own over
the same engine; each engine form and its Vuka one:

| Engine (Go) | Vuka |
|---|---|
| `Filter(Q{"author__name": x})`, `Exclude`, `Get(ctx, Q{…})` | `Filter(Post.Author.Name.Eq(x))`, `Exclude(…)`, `Get(ctx, …)` → `Result` |
| `And`, `Or`, `Not`, raw `Q` | `vuka.And/Or/Not`; `orm.P[T](q)`, `Where(q)` |
| `OrderBy("-views")`, `Limit`, `Offset`, `Distinct` | `OrderBy(Post.Views.Desc())`, the same |
| `First` | `First` → `Result[Option[T]]` |
| `Count`, `Exists`, `Iter`, `All` | the same, as `Result`s (`Iter` as is) |
| `Paginate(ctx, qs, r, Sortable("views"))` | `Paginate(ctx, r, orm.SortableBy(Post.Views))` |
| `Values[R]("author__name", "n")`, `Aggregate(Count("id"))` | `Values[R](ctx, orm.Into(R.Author, Post.Author.Name), …)`, `Aggregate[R](ctx, …)` |
| `Annotate("n", Count("tags"))`, `GroupBy`, `Having` | `Annotate(fn.Count(Post.Tags).As("n"))`, `GroupBy(Post.Author)`, `Having(n.Gt(1))` |
| `F("views")`, `SQL("{0} + 1", F("views"))` | `fn.Num(Post.Views).Add(1)`, `fn.SQL[int](…, Post.Views)` |
| `Case(When(c, v)).Else(v)`, `Switch(e)` | `fn.Case(fn.When(pred, v)).Else(v)`, `fn.Switch(ref)` |
| `Subquery`, `Exists`, `OuterRef` | `fn.Subquery[T](q, col)`, `fn.Exists[T](q)`, `fn.OuterEq(inner, outer)` |
| `SelectRelated("author")`, `PrefetchRelated("tags", Prefetch(…))` | `SelectRelated(Post.Author)`, `PrefetchRelated(Post.Tags, orm.PrefetchBy(Post.Tags, q))` |
| `Create`, `GetOrCreate(Q, Defaults)`, `Update(Set{…})` | `Create`, `GetOrCreate(ctx, row, Tag.Name)`, `Update(ctx, orm.Assign(Post.Status, "x"))` |
| `Save`, `SaveFields("title")`, `Load` | row methods: `SaveFields(ctx, Post.Title)`, `Load(ctx, Post.Tags)` |
| `u.Add(ctx, "tags", t)` | `p.Tags.Add(ctx, t)` |
| `SearchVector`, `Match`, `SearchRank`, `Similarity` | `orm.Search(Post.Title, Post.Body).Matches(q)`, `.Rank(q)`, `fn.Similarity(Post.Title, s)` |
| `CosineDistance("embedding", v)`, `Nearest` | `fn.CosineDistance(Post.Embedding, v)`, `Nearest(Post.Embedding, v, orm.Cosine)` |
| `Atomic`, `TxOn`, `Retry` | `@orm.Transaction`, `@orm.TxOn("db")`, `@orm.Retry(n)` |
| `Meta{DB, Read, Write, Mirror}`, `crossdb`, `ConnFor` | `@orm.Meta{DB: "analytics"}`, `@orm.Rel{CrossDB: true}`, `@orm.Database("analytics")` |
| struct tags, `Indexes()`, `Generated()` | [field attributes](/orm/models#field-attributes), `@orm.SearchOf` |

Not a Vuka form: a field reference has no methods of the ORM's, so a vector
distance is `fn.CosineDistance(Post.Embedding, v)` rather than
`Post.Embedding.CosineDistance(v)`; and reverse relations have no field type
yet — filter through them with `orm.Q{"posts__views__gt": 60}`.
