# Configuration and .env

Configuration comes from environment variables. `web.New()` first reads
`.env` files into the process environment, so every constructor, `Start`
method and option sees their values through `os.Getenv` or `web.Env`.

```vuka
func OpenDB() (*sql.DB, error) {        // a constructor: func(deps…) T or (T, error)
	return sql.Open("pgx", web.MustEnv[string]("DATABASE_URL"))
}

func main() {
	app := web.New(web.Provide(OpenDB), web.Require("DATABASE_URL", "SECRET_KEY"))
	app.Run(":" + web.Env("PORT", "8080"))
}
```

## Files and precedence

By `VUKA_ENV`, the first file listed winning:

| `VUKA_ENV` | Files |
|---|---|
| `development`, `staging`, … | `.env.<env>.local`, `.env.local`, `.env.<env>`, `.env` |
| `test` (also when unset under `go test`) | `.env.test`, `.env` |
| unset | `.env.local`, `.env` |

- The **real environment wins** over every file: a key is set only when the
  process doesn't have it yet.
- `.env.local` files hold one machine's settings and are skipped in tests, so
  test runs don't depend on the developer's box.
- `VUKA_ENV` picks the files, so it comes from the real environment; set in a
  file, it still turns on dev behaviour.
- Files are read from the working directory or, when it has none of them,
  from the nearest parent up to the module root (the directory with
  `go.mod`), so `go test ./pkg/...` finds the project's `.env`. Missing files
  are fine.
- With `VUKA_ENV=development` the app logs which files it read.

| Option | |
|---|---|
| `web.DotEnv(files…)` | replaces the list (first wins) |
| `web.NoDotEnv()` | reads none |
| `web.LoadEnv(files…)` | loads them now, before `web.New` — for options built from the environment; New reading the same files afterwards changes nothing |
| `web.Require(keys…)` | fails Build when any is unset |

## Syntax

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

`$VAR` and `${VAR}` expand in unquoted and double-quoted values, from the real
environment, then the keys defined above (in this file or a lower-precedence
one); an undefined variable is empty. A malformed line is a Build error with
the file and line, and nothing of that file is set.

## Reading

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
`encoding.TextUnmarshaler`, or a slice of these. A value that doesn't convert
gives the default, and the next `app.Build()` (or `app.Start`, for
constructors) fails naming the variable.

## Required variables

`web.Require` fails Build listing every missing variable, the files looked
for, and the keys of `.env.example` that are unset too:

```text
web: missing environment variables: SECRET_KEY (read .env; looked for .env.local, .env); also unset, listed in .env.example: SMTP_HOST
```

Errors and logs name variables and files, never values. Keep `.env` and
`.env*.local` out of git, and commit an `.env.example` with the keys and
harmless values instead; in production, set real environment variables.
