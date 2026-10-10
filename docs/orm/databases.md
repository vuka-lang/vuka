# Databases

## Support

| | SQLite | Postgres | MySQL / MariaDB |
|---|---|---|---|
| Import | `orm/sqlite` (pure Go, `glebarez/go-sqlite`) | `orm/postgres` (pgx) | `orm/mysql` (go-sql-driver) |
| URL | `sqlite:app.db`, `sqlite::memory:` | `postgres://…` | `mysql://user:pass@tcp(host)/db` |
| Queries, relations, aggregates, subqueries | ✓ | ✓ | ✓ |
| Text search | FTS5 | `tsvector` | `FULLTEXT` |
| Trigram similarity | in Go, unindexed | `pg_trgm` | `LIKE` |
| Vectors | distances in Go, unindexed | pgvector, HNSW/IVFFlat | stored, not searched |
| Migration lock | — | advisory lock | `GET_LOCK` |
| SQL functions | Go functions (`sqlite.Func`) | `CreateFunction` | `CreateFunction` |

SQLite is the development and test database: the same code runs there, with
searches unindexed. A file-backed SQLite gets a small read pool.

### MySQL notes

- **Collation.** `Connect` adds `charset=utf8mb4` to a DSN naming neither a
  charset nor a collation: the driver would otherwise greet the server in
  `utf8mb4_general_ci` while MySQL 8 and 9 make tables in
  `utf8mb4_0900_ai_ci`, and comparing the two fails with an illegal mix of
  collations. A DSN's own `charset` or `collation` is kept. `orm.MySQLDSN(dsn)`
  is the DSN `Connect` opens, for a `*sql.DB` opened elsewhere.
- **Vectors are storage only**: a `VECTOR(n)` column where the server has the
  type (MySQL 9, MariaDB 11.7), else `VARBINARY(4n)`; a distance query fails
  when it is written, naming the field.
- **Version detection.** The ORM reads `VERSION()` once when it makes tables
  or runs migrations, to pick the column types; `orm sqlmigrate --dialect
  mysql/9.3.0` writes a given server's.

## Connecting

| | |
|---|---|
| `orm.ConnectURL(url)` | the dialect from the URL |
| `orm.Connect(dialect, dsn)` | a dialect and a driver DSN |
| `orm.Open(sqlDB, dialect)` | a `*sql.DB` you opened yourself |
| `orm.AddDatabase(name, db, orm.AsDefault())` | registers it in `orm.Default`, the program's set |

A database added this way is used as it is: the program opened it, and
closes it. To have the ORM open, supervise and close them — with replicas, a
pool, health checks and a circuit breaker — declare them:

```vuka
dbs := orm.OpenDatabases(ctx,
	orm.Declare("main", os.Getenv("DATABASE_URL"), orm.AsDefault(),
		orm.Replicas(os.Getenv("REPLICA_URL")),
		orm.Pool{MaxOpen: 20, MinIdle: 2, Acquire: 2 * time.Second}),
	orm.Declare("analytics", "postgres://…/analytics?pool_max=5&breaker_failures=3", orm.ReadOnly()),
	orm.Routers(tenants{}))?
defer dbs.Close(ctx)
ctx = orm.WithDatabases(ctx, dbs)
```

| `orm.Declare(name, url, …)` option | |
|---|---|
| `orm.AsDefault()` | the default database |
| `orm.ReadOnly()` | writes refused |
| `orm.Replicas(urls…)` | read replicas |
| `orm.Pool{…}` | the [connection pool](#connection-pool) |
| `orm.Credentials(fn)` | user and password asked for every new connection (a rotating secret) |
| `orm.Session{"statement_timeout": "5s"}` | settings run on each new connection |
| `orm.Timeout(d)` | bounds every statement |
| `orm.SlowQuery(d)` | logs statements slower than `d` |

`orm.OpenDatabases(ctx, opts…)` is `orm.NewDatabases(opts…)` then
`Start(ctx)`; with nothing declared it reads `DATABASE_URL` (as `default`)
and `DATABASE_URL_<NAME>`, as the `orm` command does. `Start` first runs
`set.Check()`: a model, a route or a connection handle naming a database the
set doesn't declare fails it, and so does a relation between models on two
databases that isn't `CrossDB`. Other options of the set: `orm.Routers(…)`,
`orm.ReadYourWrites(bool)`, `orm.Logger(l)`, and `orm.OnOpen(fn)` — work
once the databases are open (migrations, a seed).

`orm.WithDatabases(ctx, set)` puts a set on a context; a process serving
several apps gives each its own. In a web app, [`ormweb`](#web-integration)
does all of this.

## Multiple databases

A model's database comes from, first to last:

1. `orm.Using(ctx, name)` — every query with that context (`orm.WithDB(ctx,
   db)` still wins, for tests and tools);
2. the set's routers (below);
3. the model's `Meta`: `DB` (reads and writes), `Read` and `Write` (over
   `DB`), and `Mirror` (writes repeated there);
4. the table's route: `set.Route("categories", orm.Route{DB: "legacy",
   Mirror: "main"})` — configuration for tables whose models say nothing;
5. the default.

```vuka
type Event struct {
	orm.Base @orm.Meta{DB: "analytics"}
	ID   int64
	Kind string @orm.Char{Max: 50}
}

type Note struct {
	orm.Base @orm.Meta{Read: "replica", Write: "main"}
	ID   int64
	Text string
}

eu := Event.Objects.Count(orm.Using(ctx, "analytics_eu"))?   // another database, for this query
```

### Routers

An `orm.Router` decides per query; embed `orm.BaseRouter` for the methods you
don't decide. The first router naming a database wins; `""` abstains:

```vuka
type tenants struct{ orm.BaseRouter }

func (tenants) ReadDB(ctx context.Context, m orm.ModelInfo) string  { return tenantOf(ctx) }
func (tenants) WriteDB(ctx context.Context, m orm.ModelInfo) string { return tenantOf(ctx) }
```

| `orm.Router` method | |
|---|---|
| `ReadDB(ctx, m) string`, `WriteDB(ctx, m) string` | the database for a model's reads, writes |
| `AllowRelation(a, b) bool` | whether two models may relate |
| `AllowMigrate(db, m) bool` | whether a model's table is migrated on `db` |

Register with `orm.Routers(…)` or `set.AddRouter(r)`. `AllowRelation` and
`AllowMigrate` return bools, so they can't abstain: every router must allow
(`BaseRouter` allows everything).

### Read-your-writes

Reads go to the read side — the `Read` database, or a replica — except
inside a transaction, after a write in the same `orm.Scope(ctx)`, or under
`orm.Primary(ctx)`: then a model's reads go where it writes. Middleware
starts a scope per request (ormweb does), so once a request writes `Note`,
its reads of `Note` see the write. A scope that wrote a database also sends
raw reads of it to the primary. `orm.ReadYourWrites(false)` on the set turns
it off.

### Cross-database relations

A relation to a model on another database is declared `CrossDB`:

```vuka
type Sale struct {
	orm.Base @orm.Meta{DB: "analytics"}
	ID      int64
	Product orm.FK[Product] @orm.Rel{CrossDB: true, Related: "sales"}
	Amount  int
}
```

Its key is a plain indexed column with no foreign key constraint.
`PrefetchRelated`, `Load` and `FK.Get` read the related rows from their own
database, in batches by key, and filtering by the key reads the column. A
query that would join it — a filter, an ordering or `SelectRelated` through
it (`Sale.Product.Name.Eq(…)`) — is refused before it is sent, naming the
relation. Without `CrossDB`, a relation between models on two databases
fails `Check`, naming both. (The Go spelling is the tag `orm:"crossdb"`.)

### Transactions

A transaction spans one database: the one of its first write, or the one
`@orm.TxOn("db")` names. See [Transactions](/orm/transactions#which-database).

### Mirrors

`Meta.Mirror` (or a route's `Mirror`) repeats a model's writes on a second
database — the way to move a table: write both while reading one, then
switch.

## Raw SQL

```vuka
type Sale struct {
	Day   time.Time
	Total int64
	Note  Option[string]  // NULL → None; a pointer works too
}

sales := orm.Raw[Sale](ctx, "SELECT day, SUM(total) AS total, NULL AS note FROM sales WHERE day > ? GROUP BY day", since, orm.On("analytics"))?
n := orm.RawRow[int64](ctx, "SELECT COUNT(*) FROM sales", orm.On("analytics"))?   // NotFound for no row
orm.Exec(ctx, "DELETE FROM sales WHERE day < ?", cutoff, orm.On("analytics"))?
posts := Post.Objects.Raw(ctx, "SELECT * FROM posts WHERE views > ?", 10)?       // the model's read database
```

| | |
|---|---|
| `orm.Raw[R](ctx, sql, args…)` | rows into structs (or scalars) |
| `orm.RawRow[R](ctx, sql, args…)` | one row; `orm.NotFound` when there is none |
| `orm.Exec(ctx, sql, args…)` | a statement; the rows affected |
| `Post.Objects.Raw(ctx, sql, args…)` | a model's rows |

Columns fill struct fields by `db` tag, column or name; a column the struct
has no field for is an error unless `orm.Loose()` is among the arguments.
`?` marks arguments everywhere (written `$1`… on Postgres; `??` is a literal
`?`), and values are always bound. Without `orm.On(name)` a raw read goes to
the default database's read side, `Exec` to its primary. `orm.Timeout(d)`
among the arguments bounds the statement. `Using`, routers, transactions and
observers apply as to any query.

### Connection handles

A struct embedding `orm.Conn`, named for a database, is a handle on it —
injected by type in a web app:

```vuka
@orm.Database("analytics")
type Analytics struct{ orm.Conn }

@web.Get("/report")
func Report(ctx context.Context, a *Analytics) Result[[]Sale] {
	return vuka.Of(orm.Raw[Sale](a.With(ctx), "SELECT day, SUM(total) AS total FROM sales GROUP BY day"))
}
```

A `Conn` is a `context.Context` too (background, carrying its database), so
`orm.Raw[R](a, …)` works; `a.With(ctx)` keeps the request's cancellation,
transaction and scope. A handle among Raw's arguments routes it as `orm.On`
does. `Exec`, `Ping`, `Stats`, `DB` and `Name` are its methods. In Go,
`orm.ConnFor[Analytics]("analytics")` declares it and returns its
constructor; `orm.HandleOf[T](set)` makes one.

## Connection pool

Every declared database has a supervisor. Its pool is set with `orm.Pool` or
with URL parameters (taken off before the driver sees the URL):

| `orm.Pool` field | URL parameter | Default | |
|---|---|---|---|
| `MaxOpen` | `pool_max` | unlimited | connections at most |
| `MaxIdle` | `pool_max_idle` | 2 | idle connections kept |
| `MinIdle` | `pool_min_idle` | 0 | opened at start and kept open while up |
| `MaxLifetime` | `pool_max_lifetime` | none | less up to 20% at random, so connections don't all reconnect at once |
| `MaxIdleTime` | `pool_max_idle_time` | none | |
| `Acquire` | `pool_acquire` | wait | with `MaxOpen` in use, fail `Unavailable` after this |
| `Health` | `pool_health` | 10s | ping interval, primary and replicas |
| `Breaker{Failures, Cooldown}` | `breaker_failures`, `breaker_cooldown` | 5, 5s | the circuit breaker |
| `StartupWait` | `startup_wait` | 5s | how long `Start` waits for the database; negative doesn't wait |

- **States**: `Connecting`, `Up`, `Degraded` (a replica down, or the breaker
  probing), `Down`. A database that doesn't answer within `StartupWait`
  starts `Down` and the program starts anyway; the supervisor reconnects in
  the background with exponential backoff (100ms to 30s). State changes are
  logged and reach [`orm.ObserveState(fn)`](/orm/observability#pool-state).
- **Circuit breaker**: only connection errors count — refused, reset, timed
  out, dropped — never a query's own. After `Failures` in a row, statements
  fail at once with `orm.Unavailable`; after `Cooldown` one statement probes,
  and its success closes it.
- **Retries**: a read that fails on a dropped connection is retried once, on
  a fresh connection; writes never are.
- **Replicas**: reads go to the healthy replica least in use, the primary
  when none is; a replica failing a read or a ping is ejected until a ping
  succeeds again.
- **Timeouts**: `orm.Timeout(d)` on `Declare` bounds every statement;
  `orm.WithTimeout(ctx, d)` a context's.
- **Shutdown**: `set.Close(ctx)` (or `Stop`) refuses new statements, waits
  for running ones until ctx's deadline, then closes the pools.

## Web integration

`github.com/vuka-lang/orm/ormweb` serves a set of databases in a
[web](/web/) app. `ormweb.Databases(opts…)` is one `web.Option`:

```vuka
func main() {
	app := web.New(
		ormweb.Databases(
			orm.Declare("main", os.Getenv("DATABASE_URL"), orm.AsDefault(), orm.Replicas(os.Getenv("REPLICA_URL"))),
			orm.Declare("analytics", os.Getenv("ANALYTICS_URL")),
		).MigrateOnStart(true),
		web.Use(ormweb.UsingFrom(func(r *http.Request) string { return r.Header.Get("X-Tenant") })),
	)
	log.Fatal(app.Run(""))
}
```

It:

- provides `*orm.Databases` as a ready value, so the app's
  [start](/web/dependency-injection#lifecycle) opens it (waiting
  `StartupWait`) before anything built from constructors starts, and its
  stop closes it gracefully after they stop;
- provides every [connection handle](#connection-handles) type, so handlers
  take `*Analytics`;
- puts the set and an `orm.Scope` on each request's context, so handlers'
  queries need nothing more and [read their writes](#read-your-writes);
- serves `GET /healthz`: JSON of each database's state, pool, breaker and
  replicas, 503 when the default database is down;
- adds the set's `Check` to the app's `Build`: a model, route or handle
  naming a database the app doesn't declare is a Build error, reported with
  web's own.

| | |
|---|---|
| `.MigrateOnStart(b)` | runs pending migrations as the databases open; by default when `VUKA_ENV=development`, read then, so a `.env` counts |
| `.Health(path)` | moves the health route; `""` drops it |
| `ormweb.Set(s, opts…)` | serves a set made elsewhere |
| `ormweb.UsingFrom(fn)` | middleware sending a request's queries to the database `fn` names (a tenant's); `""` leaves them |
| `ormweb.Middleware(s)`, `ormweb.HealthHandler(s)` | the parts, for another router |

[`examples/multidb`](https://github.com/vuka-lang/orm/tree/main/examples/multidb)
is a whole app: products on main read from a replica, sales on analytics
holding their product by a CrossDB key, a raw report through an injected
`*Analytics`, a tenant's database per request, and `/healthz`.
