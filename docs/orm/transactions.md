# Transactions

[[toc]]

## Introduction

When several writes must succeed or fail together, you run them in a
transaction. `orm.Transaction(ctx, func(ctx context.Context) error { … })` runs
a closure in a transaction, and the context inside it is the transaction's.

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

## Running a Transaction {#running}

This function reads and publishes a post inside one transaction:

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

`orm.Transaction` runs `fn` in a transaction: committed when it succeeds,
rolled back when it returns an error or panics. The `ctx` it gives `fn` is the
transaction's, so the queries made with it take part.

If the commit itself fails, `orm.Transaction` returns that error.

### Nested Transactions

Nested — a transaction calling another — the inner one is a savepoint: its
failure rolls back its own part.

## Choosing the Database {#which-database}

A transaction spans one database. It begins on the database of the first
write inside it — reads before the first write run outside it, on the
models' write databases — and a write to another database fails. To name the
database from the start, pass `orm.TxOn`:

```go
func record(ctx context.Context, e Event) error {
	return orm.Transaction(ctx, func(ctx context.Context) error { return e.Save(ctx) },
		orm.TxOn("analytics"), orm.Retry(3))
}
```

See [Multiple Databases](/orm/databases#multiple-databases).

## Retrying on Conflicts {#retries}

`orm.Retry(n)` runs the transaction again — up to `n` times, with a jittered
backoff — on a deadlock or serialization failure (Postgres 40001/40P01,
MySQL 1213/1205, SQLite busy). Only the outermost transaction retries.

> [!WARNING]
> The function must be safe to run twice.

## Running Code After the Commit {#after-the-commit}

`orm.OnCommit(ctx, fn)` runs `fn` once the transaction commits — never for a
rolled back one, nor for a rolled back savepoint's part; outside a
transaction, at once. It is the place to send mail or enqueue work only for
writes that stay:

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

## Reference {#the-calls}

The calls and options are:

| | |
|---|---|
| `orm.Transaction(ctx, fn, opts…)` | `fn` in a transaction; nested, a savepoint (`orm.Atomic` is the same) |
| `orm.AtomicOn(ctx, db, fn, opts…)` | on a given `*orm.DB` |
| `orm.TxOn("db")`, `orm.Retry(n)` | the options, as values |

Options follow the function:

<!-- go:fragment -->
```go
err := orm.Transaction(ctx, func(ctx context.Context) error {
	_, err := orm.Objects[User]().Filter(orm.Q{"age__lt": 18}).Delete(ctx)
	return err
}, orm.Retry(3))
```
