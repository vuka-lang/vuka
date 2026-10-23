# Crons

[[toc]]

## Introduction

Sessions to expire every ten minutes, a report at seven on weekday mornings,
a cache to warm every thirty seconds: you register the function with
`app.Cron(name, spec, fn)`. It takes a `context.Context` and its
dependencies, like a handler, and the app runs it on schedule.

<!-- go:context
type DB struct{}

func OpenDB() *DB { return &DB{} }

type Owners struct{}

func (o *Owners) ForgetUnconfirmed(before time.Time) int { return 0 }
-->

## Registering a Cron

In `main.go`, register a function and a service's method, each with its
schedule:

```go
// forgetUnconfirmed drops the sign-ups nobody confirmed within a day.
func forgetUnconfirmed(ctx context.Context, owners *Owners) {
	if n := owners.ForgetUnconfirmed(time.Now().Add(-24 * time.Hour)); n > 0 {
		slog.InfoContext(ctx, "forgot unconfirmed sign-ups", "n", n)
	}
}

type Reports struct {
	db     *DB
	mailer *mail.Mailer
}

func NewReports(db *DB, mailer *mail.Mailer) *Reports { return &Reports{db: db, mailer: mailer} }

// Morning is a service's method.
func (r *Reports) Morning(ctx context.Context) error { return nil }

func main() {
	app := web.New()
	app.Service(OpenDB)
	app.Service(NewReports)
	app.Cron("forgetUnconfirmed", "@every 10m", forgetUnconfirmed)
	app.Cron("Reports.Morning", "CRON_TZ=Africa/Dar_es_Salaam 0 7 * * 1-5", (*Reports).Morning)
	log.Fatal(app.Run(""))
}
```

A cron returns nothing or an `error`. Its parameters are a
`context.Context` and [provided types](/web/dependency-injection); on a
method, the receiver is provided too. Its name, `app.Cron`'s first
argument, names it in logs, the toolbar and `RunCron`.

### Function Literals

A function literal works too:

<!-- go:fragment -->
```go
app := web.New()
app.Cron("digest", "0 8 * * *", func(ctx context.Context, m *mail.Mailer) error {
	return nil
})
```

## Writing Schedules {#schedules}

A spec is a five-field cron expression, a shorthand, or an interval:

| spec | runs |
|---|---|
| `*/15 * * * *` | every fifteen minutes |
| `0 7 * * 1-5` | at 07:00, Monday to Friday |
| `30 2 1,15 * *` | at 02:30 on the 1st and the 15th |
| `0 0 * JAN,JUL SUN` | at midnight on the Sundays of January and July |
| `@hourly`, `@daily` (`@midnight`), `@weekly`, `@monthly`, `@yearly` (`@annually`) | at the start of each |
| `@every 30s`, `@every 1h30m` | every duration |
| `CRON_TZ=Europe/Paris 0 9 * * *` | at 09:00 in Paris |

### The Five Fields

Five fields: minute (0–59), hour (0–23), day of the month (1–31), month
(1–12 or `JAN`–`DEC`) and day of the week (0–7 or `SUN`–`SAT`; 0 and 7 are
Sunday). Each is `*`, a value, a range `a-b`, a list `a,b,c`, and any of
these with a step: `*/5`, `10-30/10`. When both day fields are restricted,
a day matching either runs: `0 0 13 * FRI` is every 13th and every Friday.

### Intervals and Time Zones

`@every d` runs at the multiples of `d`, counted from a fixed origin:
`@every 30s` runs at :00 and :30 of each minute, on every replica and after
every restart. Without `CRON_TZ=` (or `TZ=`) the fields are read in the
process's local time; set it on servers whose zone isn't yours. A zone
needs the system's time zone data, or `import _ "time/tzdata"` in the
program.

### Daylight Saving

A time the clock skips — 02:30 on the night clocks go
forward — runs at the jump, once, however many runs fell in the gap. A time
the clock passes twice — 01:30 on the night they go back — runs once, at the
first passing. With the hour field `*` the job runs by the clock instead:
an hourly job runs every hour that happens, twice in the repeated one,
never in the skipped one.

### Parsing Specs in Go

The parser is its own package, `github.com/vuka-lang/web/v2/cron`, with no
dependencies: `cron.Parse(spec)` returns a `Schedule` whose `Next(t)` is the
first run after `t`.

## How Crons Run {#running}

`app.Start` (and `app.Run`) builds what the crons need, then schedules
them; `app.Stop` stops scheduling first, waits for the runs in progress
until its context is done, then cancels their context — so a long job
should watch `ctx.Done()`. Four rules hold for every run:

- **No overlap.** A tick that finds the previous run still going is skipped
  and logged (`cron skipped: the previous run is still running`).
- **No catching up.** Ticks missed while the machine slept aren't run late;
  the next one is.
- **Panics** are recovered and logged with their stack; the scheduler keeps
  going.
- **Errors** go to `slog` (`cron failed cron=digest err=…`); a run that
  succeeds logs at debug level.

### Mistakes Are Build Errors

Mistakes are [Build errors](/web/dependency-injection), before anything is
served: an invalid spec (naming the function and where it is declared), a
parameter nothing provides, two crons of one name.

```text
web: @web.Cron("61 * * * *") on main.cleanUp at jobs.vuka:12: cron: "61 * * * *": minute: 61 is out of range 0-59
```

### Running a Cron Now

These methods and options run crons on demand or change how they are
scheduled:

| | |
|---|---|
| `app.RunCron(ctx, name)` | runs one now and returns its error; `web.ErrCronRunning` when it is already running |
| `web.NoCrons()` | serve without scheduling them: a replica that only answers requests |
| `web.CronClock(c)` | schedule by another clock (`crontest.Clock` in tests) |

## Running Several Replicas {#several-replicas}

Every process schedules its own crons, so with three replicas each tick
runs three times. Provide a `web.CronLock` and only the replica that claims
a tick runs it:

```go
type CronLock interface {
	Claim(ctx context.Context, name string, tick time.Time) (bool, error)
}
```

Every replica asks for the same name and tick, so the lock is a unique key.
With a database, insert the pair into a table with a unique index:


```go
type DBLock struct{ db *sql.DB }

func (l *DBLock) Claim(ctx context.Context, name string, tick time.Time) (bool, error) {
	res, err := l.db.ExecContext(ctx,
		`INSERT INTO cron_ticks (name, tick) VALUES ($1, $2) ON CONFLICT DO NOTHING`, name, tick)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func newApp() *web.App {
	app := web.New()
	app.Service(func(db *sql.DB) web.CronLock { return &DBLock{db} })
	return app
}
```

With Redis, `SET cron:<name>:<tick> 1 NX EX 3600` claims it when it answers
`OK`. A lock that fails skips the tick and logs why. Without one the app
uses `web.NewMemoryCronLock()`, which claims each tick once in its own
process. `RunCron` runs without asking.

## In the Toolbar

In development each run is a record of its own in the
[toolbar](/web/toolbar), like a request's: its statements, timeline, logs
and error, listed under *Cron runs*. Every page's drawer has a **Crons**
panel: each cron's schedule, function, next run and last run, and a
**Run now** button that runs it and opens its record.

## Testing Crons {#testing}

`crontest.Run(app, name)` runs a cron with its dependencies and returns its
error:


```go
func TestForgetUnconfirmed(t *testing.T) {
	app := web.New()
	app.Service(OpenDB)
	app.Service(func() *mail.Mailer { return mailtest.New() })
	owners, err := web.Resolve[*Owners](app)
	if err != nil {
		t.Fatal(err)
	}
	_ = owners // add an old sign-up
	if err := crontest.Run(app, "forgetUnconfirmed"); err != nil {
		t.Fatal(err)
	}
	// check it is gone
}
```

For the schedule itself, `crontest.NewClock(t)` is a virtual clock: nothing
fires until the test moves it.


```go
func TestMorningReport(t *testing.T) {
	clock := crontest.NewClock(time.Date(2026, 1, 5, 6, 59, 0, 0, time.UTC)) // a Monday
	app := web.New(web.CronClock(clock))
	app.Service(OpenDB)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer app.Stop(context.Background())

	clock.WaitTimers(1)        // the scheduler waits for its tick
	clock.Advance(time.Minute) // 07:00: the report runs
	crontest.Wait(app)         // and has finished
}
```

| | |
|---|---|
| `clock.Advance(d)`, `clock.Set(t)` | move the clock, firing the timers due |
| `clock.WaitTimers(n)` | wait until the scheduler waits on `n` timers — one per cron — before moving it again |
| `clock.Next()` | when the next timer fires |
| `crontest.Wait(app)` | wait for the runs in progress; a tick that finds one still going is skipped |
