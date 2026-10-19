# Transactions

<!-- go:context
type Post struct {
	orm.Model[Post]
	ID        int64
	Slug      string
	Published vuka.Option[time.Time]
}

type Event struct {
	orm.Model[Event]
	ID   int64
	Kind string
}

type User struct {
	orm.Model[User]
	ID    int64
	Email string
}

func sendWelcome(email string) {}
-->

`orm.Transaction(ctx, func(ctx context.Context) error { … })` runs a closure
in a transaction, and the context inside it is the transaction's.<span class="vuka-only">
Vuka adds a [decorator](/features/decorators): the function is the
transaction.</span>

::: vuka
```vuka
@orm.Transaction
func publish(ctx context.Context, slug string) error {
	p := Post.Objects.Get(ctx, Post.Slug.Eq(slug))?
	p.Published = Some(time.Now())
	return p.Save(ctx)
}
```
:::

::: go
```go
func publish(ctx context.Context, slug string) error {
	return orm.Transaction(ctx, func(ctx context.Context) error {
		p, err := orm.Objects[Post]().Get(ctx, PostFields.Slug.Eq(slug))
		if err != nil {
			return err
		}
		p.Published = vuka.Some(time.Now())
		return p.Save(ctx)
	})
}
```
:::

<span class="vuka-only">`@orm.Transaction` runs the function</span><span class="go-only">`orm.Transaction` runs `fn`</span> in a transaction: committed when it
succeeds, rolled back when it returns an error<span class="vuka-only"> or an `Err` result</span>, or
panics. <span class="vuka-only">Its `context.Context` argument is the transaction's, so the queries
it makes with it take part. A function without a `context.Context` parameter
is an error when it is called.</span><span class="go-only">The `ctx` it gives `fn` is the transaction's, so the queries
made with it take part.</span> Nested — a transaction calling another —
the inner one is a savepoint: its failure rolls back its own part.

If the commit itself fails, <span class="vuka-only">the function's error result (or its `Result`)
carries</span><span class="go-only">`orm.Transaction` returns</span> that error.

## Which database

A transaction spans one database. It begins on the database of the first
write inside it — reads before the first write run outside it, on the
models' write databases — and a write to another database fails. To name the
database from the start, <span class="vuka-only">put `@orm.TxOn` beside it</span><span class="go-only">pass `orm.TxOn`</span>:

::: vuka
```vuka
@orm.Transaction
@orm.TxOn("analytics")
@orm.Retry(3)
func record(ctx context.Context, e Event) error { return e.Save(ctx) }
```
:::

::: go
```go
func record(ctx context.Context, e Event) error {
	return orm.Transaction(ctx, func(ctx context.Context) error { return e.Save(ctx) },
		orm.TxOn("analytics"), orm.Retry(3))
}
```
:::

See [Multiple databases](/orm/databases#multiple-databases).

## Retries

<span class="vuka-only">`@orm.Retry(n)`</span><span class="go-only">`orm.Retry(n)`</span> runs the transaction again — up to `n` times, with a jittered
backoff — on a deadlock or serialization failure (Postgres 40001/40P01,
MySQL 1213/1205, SQLite busy). The function must be safe to run twice, and
only the outermost transaction retries.

## After the commit

`orm.OnCommit(ctx, fn)` runs `fn` once the transaction commits — never for a
rolled back one, nor for a rolled back savepoint's part; outside a
transaction, at once. The place to send mail or enqueue work only for
writes that stay:

::: vuka
```vuka
@orm.Transaction
func signUp(ctx context.Context, u User) error {
	u.Save(ctx)?
	return orm.OnCommit(ctx, func(ctx context.Context) { sendWelcome(u.Email) })
}
```
:::

::: go
```go
func signUp(ctx context.Context, u User) error {
	return orm.Transaction(ctx, func(ctx context.Context) error {
		if err := u.Save(ctx); err != nil {
			return err
		}
		return orm.OnCommit(ctx, func(ctx context.Context) { sendWelcome(u.Email) })
	})
}
```
:::

## The calls

| | |
|---|---|
| `orm.Transaction(ctx, fn, opts…)` | `fn` in a transaction; nested, a savepoint (`orm.Atomic` is the same) |
| `orm.AtomicOn(ctx, db, fn, opts…)` | on a given `*orm.DB` |
| `orm.TxOn("db")`, `orm.Retry(n)` | the options, as values |

<!-- go:fragment -->
```go
err := orm.Transaction(ctx, func(ctx context.Context) error {
	_, err := orm.Objects[User]().Filter(orm.Q{"age__lt": 18}).Delete(ctx)
	return err
}, orm.Retry(3))
```
