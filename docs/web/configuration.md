# Configuration

[[toc]]

## Introduction

Configuration comes from environment variables. `web.New()` first reads
`.env` files into the process environment, so every constructor, `Start`
method and option sees their values through `os.Getenv` or `web.Env`.

You keep one machine's settings in `.env` files during development, and set
real environment variables in production.

## Reading Configuration at Startup

A constructor reads its settings, and `web.Require` names the variables the
app can't run without:


```go
func OpenDB() (*sql.DB, error) {        // a constructor: func(deps…) T or (T, error)
	return sql.Open("pgx", web.MustEnv[string]("DATABASE_URL"))
}

func main() {
	app := web.New(web.Require("DATABASE_URL", "SECRET_KEY"))
	app.Service(OpenDB)
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
```

## Env Files and Precedence {#files-and-precedence}

Which files are read depends on `VUKA_ENV`, and the first file listed wins:

| `VUKA_ENV` | Files |
|---|---|
| `development`, `staging`, … | `.env.<env>.local`, `.env.local`, `.env.<env>`, `.env` |
| `test` (also when unset under `go test`) | `.env.test`, `.env` |
| unset | `.env.local`, `.env` |

The **real environment wins** over every file: a key is set only when the
process doesn't have it yet. `.env.local` files hold one machine's settings
and are skipped in tests, so test runs don't depend on the developer's box.

### Variables Files Can't Set

Keys starting with `VUKA_` (`VUKA_ENV`, `VUKA_DEV_DIR`, `VUKA_DEV_LIVE_KEY`)
come from the real environment only. An env file setting one is ignored,
with a line in the log: a `.env` left next to a deployed binary never
switches it into development. `web.IsDev()` says whether the process runs in
development.

### Where Files Are Found

Files are read from the working directory or, when it has none of them,
from the nearest parent up to the module root (the directory with `go.mod`),
so `go test ./pkg/...` finds the project's `.env`. Without a `go.mod` in the
working directory or above it (a deployed binary), only the working
directory is read. Missing files are fine.

With `VUKA_ENV=development` the app logs which files it read.

### Choosing the Files

These options change which files are read:

| Option | |
|---|---|
| `web.DotEnv(files…)` | replaces the list (first wins) |
| `web.NoDotEnv()` | reads none |
| `web.LoadEnv(files…)` | loads them now, before `web.New` — for options built from the environment; New reading the same files afterwards changes nothing |
| `web.Require(keys…)` | fails Build when any is unset |

## Env File Syntax {#syntax}

An env file holds one `KEY=value` per line:

```sh
# a comment
PORT=8080
export HOST=localhost              # export is allowed; " #" starts a comment
NAME=two words                     # unquoted values are trimmed
RAW='no $expansion, no \n escapes'  # single quotes (and `backticks`) are literal
KEY="-----BEGIN KEY-----
multi-line, with \n \t \" \\ and \$ escapes"
DATABASE_URL=postgres://${HOST}:5432/app
REPLICA_URL="${REPLICA_HOST:-$HOST}"   # :- gives a default for unset or empty
app.name=pets                      # keys: [A-Za-z_][A-Za-z0-9_.]*
```

`$VAR` and `${VAR}` expand in unquoted and double-quoted values: from the
real environment, then from the keys defined above (in this file or a
lower-precedence one). An undefined variable is empty.

A malformed line is a Build error with the file and line, and nothing of
that file is set.

## Reading Values {#reading}

`web.Env` converts a value to the type of its default, or to the type you
name:

<!-- go:fragment -->
```go
port := web.Env("PORT", 8080)                   // int; the default when unset or empty
debug := web.Env[bool]("DEBUG")                 // true/false, 1/0, yes/no, on/off
ttl := web.Env("CACHE_TTL", 5*time.Minute)      // time.Duration
hosts := web.Env[[]string]("ALLOWED_HOSTS")     // comma-separated (any slice of these)
db := web.Env[*url.URL]("DATABASE_URL")         // absolute URLs; also encoding.TextUnmarshaler types
key := web.MustEnv[string]("SECRET_KEY")        // panics when unset or invalid
```

| | |
|---|---|
| `web.Env[T](key, def…)` | the value as a `T`; the default when unset, empty or invalid |
| `web.MustEnv[T](key)` | the value; panics when unset or invalid |

`T` is a string, a number, a bool, a `time.Duration`, a `*url.URL`, an
`encoding.TextUnmarshaler`, or a slice of these. A value that doesn't
convert gives the default, and the next `app.Build()` (or `app.Start`, for
constructors) fails naming the variable.

## Required Variables

`web.Require` fails Build listing every missing variable, the files looked
for, and the keys of `.env.example` that are unset too:

```text
web: missing environment variables: SECRET_KEY (read .env; looked for .env.local, .env); also unset, listed in .env.example: SMTP_HOST
```

Errors and logs name variables and files, never values.

> [!WARNING]
> Keep `.env` and `.env*.local` out of git, and commit an `.env.example`
> with the keys and harmless values instead. In production, set real
> environment variables.
