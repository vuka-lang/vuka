# Transactions

**Go:** `orm.Atomic(ctx, func(ctx context.Context) error { … })` — a closure
around the work, and the context inside it is the transaction's.

**Vuka adds** a [decorator](/features/decorators): the function is the
transaction.

```vuka
@orm.Transaction
func publish(ctx context.Context, slug string) error {
	p := Post.Objects.Get(ctx, Post.Slug.Eq(slug))?
	p.Published = Some(time.Now())
	return p.Save(ctx)
}
```

`@orm.Transaction` runs the function in a transaction: committed when it
succeeds, rolled back when it returns an error or an `Err` result, or
panics. Its `context.Context` argument is the transaction's, so the queries
it makes with it take part. A function without a `context.Context` parameter
is an error when it is called. Nested — a transaction calling another —
the inner one is a savepoint: its failure rolls back its own part.

If the commit itself fails, the function's error result (or its `Result`)
carries that error.

## Which database

A transaction spans one database. It begins on the database of the first
write inside it — reads before the first write run outside it, on the
models' write databases — and a write to another database fails. To name the
database from the start, put `@orm.TxOn` beside it:

```vuka
@orm.Transaction
@orm.TxOn("analytics")
@orm.Retry(3)
func record(ctx context.Context, e Event) error { return e.Save(ctx) }
```

See [Multiple databases](/orm/databases#multiple-databases).

## Retries

`@orm.Retry(n)` runs the transaction again — up to `n` times, with a jittered
backoff — on a deadlock or serialization failure (Postgres 40001/40P01,
MySQL 1213/1205, SQLite busy). The function must be safe to run twice, and
only the outermost transaction retries.

## After the commit

`orm.OnCommit(ctx, fn)` runs `fn` once the transaction commits — never for a
rolled back one, nor for a rolled back savepoint's part; outside a
transaction, at once. The place to send mail or enqueue work only for
writes that stay:

```vuka
@orm.Transaction
func signUp(ctx context.Context, u User) error {
	u.Save(ctx)?
	return orm.OnCommit(ctx, func(ctx context.Context) { sendWelcome(u.Email) })
}
```

## In Go

| | |
|---|---|
| `orm.Atomic(ctx, fn, opts…)` | `fn` in a transaction; nested, a savepoint |
| `orm.AtomicOn(ctx, db, fn, opts…)` | on a given `*orm.DB` |
| `orm.TxOn("db")`, `orm.Retry(n)` | the options, as values |

```go
err := orm.Atomic(ctx, func(ctx context.Context) error {
	_, err := Users.Filter(orm.Q{"age__lt": 18}).Delete(ctx)
	return err
}, orm.Retry(3))
```
