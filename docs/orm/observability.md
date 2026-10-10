# Observability

## Observers

Every statement passes through observers — the hook for logging, tracing,
metrics, a debug toolbar, or counting queries in tests:

```vuka
stop := orm.Observe(func(ctx context.Context, q orm.QueryInfo) {
	slog.DebugContext(ctx, "sql", "sql", q.SQL, "db", q.DB, "dur", q.Duration, "rows", q.Rows, "err", q.Err)
})
defer stop()

ctx = orm.WithObserver(ctx, logQuery)   // one context's statements only
```

| `orm.QueryInfo` field | |
|---|---|
| `SQL`, `Args` | the statement and its arguments |
| `DB`, `Replica` | the database's name, and whether a replica of it served the read |
| `Start`, `Duration` | when, how long |
| `Rows` | the rows an exec affected; -1 for a query |
| `Err` | the error |
| `Refused`, `Model` | a query the ORM refused before sending it (an argument it can't write, a join across databases): `Err` says why; it never reached the database |

## N+1 queries

`orm.WatchRepeats(ctx, n, warn)` calls `warn` once a context runs the same
statement `n` times — the N+1 that
[`SelectRelated` or `PrefetchRelated`](/orm/queries#relations) would read in
one query:

```vuka
ctx = orm.WatchRepeats(ctx, 5, func(ctx context.Context, sql string, times int) {
	slog.WarnContext(ctx, "repeated query", "sql", sql, "times", times)
})

posts := Post.Objects.All(ctx)?
for _, p := range posts {
	p.Author.Get(ctx)?          // an N+1: SelectRelated(Post.Author) reads it in one
}
```

Put it on each request's context in development, and on tests' contexts with
a `warn` that fails the test.

## Slow queries

`orm.SlowQuery(d)` on a [declared database](/orm/databases#connecting) logs
each statement slower than `d`, through `orm.Logger(l)` (else
`slog.Default()`):

```vuka
orm.Declare("analytics", os.Getenv("ANALYTICS_URL"), orm.SlowQuery(200 * time.Millisecond))
```

Slow queries and state changes are all a supervised database logs.

## Pool state

`orm.ObserveState(fn)` is told every state change of a declared database or
replica — `Connecting`, `Up`, `Degraded`, `Down` — for alerts and metrics:

```vuka
stop := orm.ObserveState(func(e orm.StateEvent) {
	slog.Warn("database", "db", e.DB, "replica", e.Replica, "from", e.From, "to", e.To, "err", e.Err)
})
```

## Stats

| | |
|---|---|
| `orm.Stats(name)` | one database of `orm.Default`: an `orm.DBStats` |
| `set.StatsOf(name)`, `set.Stats()` | one or every database of a set |

`orm.DBStats` holds `database/sql`'s pool statistics (`Pool`), the state, the
breaker (`closed`, `open`, `half-open`) and its failures, the last error,
and each replica's health and pool. It marshals to JSON — it is what
[ormweb's `/healthz`](/orm/databases#web-integration) answers.
