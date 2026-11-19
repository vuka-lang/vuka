# Config Server

[[toc]]

## Introduction

With several apps, or one app in several environments, keeping each
machine's environment variables by hand gets error-prone: a shared value is
copied into every app, a change has no history, and an app only sees it after
a restart. A config server keeps every app's configuration in one place — a
git repository or a directory — and serves each app its values when it
starts, and again whenever they change.

The server and its client are [github.com/vuka-lang/config](https://github.com/vuka-lang/config),
released with the rest of Vuka. An app needs one import and a few
environment variables; it reads its values with `web.Env` as before
([Configuration](/web/configuration)).

## The Layout

A repository (or directory) holds env files, in the
[syntax](/web/configuration#syntax) of a project's own. The root holds what
every app shares; each app has a folder:

```text
config/
  .env                     every app, every environment
  .env.production          every app, in production
  orders/
    .env                   the orders app, every environment
    .env.production        the orders app, in production
```

For the app `orders` running with `VUKA_ENV=production`, the four files
merge in this order, a later one winning: `.env`, `.env.production`,
`orders/.env`, `orders/.env.production`. Without `VUKA_ENV` only the two
`.env` files apply. A missing file is skipped.

App names are letters, digits, `-` and `_`; an environment name may also
hold `.`. Neither starts with `-` or `.`.

Kept in git, every change has an author, a diff and a revert, and an app can
be pinned to a branch, a tag or a commit.

## Running the Server

Install the command:

```sh
go install github.com/vuka-lang/config/v2/cmd/vuka-config@latest
```

Make the server's signing key, and a token for each app:

```sh
vuka-config keygen --out signing.key    # prints the public key: the apps' VUKA_CONFIG_KEY
vuka-config token orders                # prints the app's VUKA_CONFIG_TOKEN, and a line for the tokens file
```

The tokens file holds one `app=hash` line per app; the server keeps only the
hash. Then serve a repository or a directory:

```sh
vuka-config serve --git https://example.com/acme/config.git --tokens tokens --key signing.key
vuka-config serve --dir /etc/acme-config --tokens tokens --key signing.key
```

| Setting | |
|---|---|
| `--addr` | where it listens; default `:8888` |
| `--tokens` / `VUKA_CONFIG_TOKENS` | the apps' token hashes |
| `--key` / `VUKA_CONFIG_SIGNING_KEY` | the signing key |
| `--poll` | how often a repository is fetched; default 30 s |
| `VUKA_CONFIG_HOOK_SECRET` | turns on `POST /_hooks/git`, which fetches at once (point the repository's push webhook at it) |
| `VUKA_CONFIG_GIT_TOKEN` / `--ssh-key` | access to a private repository |
| `--tls-cert`, `--tls-key` | serve HTTPS |

Run it over HTTPS (with `--tls-cert`/`--tls-key`, or behind a proxy that
ends TLS): an app's token travels with every request.

A token reads its own app's folder and the shared root files, nothing else.
Every answer is signed, and an app checks the signature with the public key,
so nothing between the server and the app can change a value.

## Connecting an App

Import the client for its effect, in the app's `main` package:

```go
import _ "github.com/vuka-lang/config/v2/client"
```

and give the app these variables:

| Variable | |
|---|---|
| `VUKA_CONFIG_URI` | the server's URL; unset, the client does nothing |
| `VUKA_CONFIG_TOKEN` | the app's token |
| `VUKA_CONFIG_KEY` | the server's public key |
| `VUKA_APP` | the app's name, its folder in the repository |
| `VUKA_ENV` | its environment (`production`): the files it reads |
| `VUKA_CONFIG_LABEL` | a branch, tag or commit; default the repository's default branch |
| `VUKA_CONFIG_CACHE` | where the last snapshot is kept; default the user cache directory |
| `VUKA_CONFIG_FAIL_FAST` | `1`: don't start without a fresh snapshot |
| `VUKA_CONFIG_WATCH` | `0`: fetch at start only, don't follow changes |
| `VUKA_CONFIG_RESTART` | `1`: restart when a value the app reads only at start changes |

```sh
VUKA_CONFIG_URI=https://config.internal VUKA_APP=orders VUKA_ENV=production \
VUKA_CONFIG_TOKEN=… VUKA_CONFIG_KEY=… ./orders
```

The values arrive before `main` runs, so every option and constructor sees
them. Precedence, highest first:

1. the process's real environment;
2. the config server;
3. env files;
4. the default in code (`web.Env`'s second argument).

A variable set in one machine's real environment still overrides the
server, which pins a value on that machine.

### When the Server Can't Be Reached

The client retries briefly, then starts with the last snapshot it received,
kept on disk sealed with the app's token and checked against the signature.
With no snapshot it starts with env files and defaults, logs an error naming
the server, and keeps trying: the first snapshot it gets is applied.
`VUKA_CONFIG_FAIL_FAST=1` makes the start fail instead.

## Changes While the App Runs

The client follows the server: a change pushed to the repository, or saved
in the directory, reaches every running instance within seconds. How a
change reaches the code depends on how the code reads the value:

<!-- go:fragment -->
```go
// read where it's used: always current
if web.Env[bool]("MAINTENANCE") {
	return errors.New("down for maintenance")
}

// a handle that stays current
limit := web.Hot[int]("RATE_LIMIT", 100)
_ = limit.Get()

// a value kept elsewhere: be told when it changes
stop := web.OnEnvChange("LOG_LEVEL", func(old, new string) {
	log.Printf("log level %s → %s", old, new)
})
defer stop()
```

| | |
|---|---|
| `web.Env[T](key, def…)` | reads the current value on every call |
| `web.Hot[T](key, def…)` | a handle whose `Get` is always current, parsed once per change |
| `web.OnEnvChange(key, fn)` | runs `fn(old, new)` after the key changes; returns a function that unregisters it |

Applied changes are logged by key, never by value:
`config: applied 2 changes (version 3f2a1c9e0b1d): LOG_LEVEL, RATE_LIMIT`. A
key removed from the repository goes back to its env-file value. A snapshot
that doesn't verify, or has a secret the app can't decrypt, is not applied at
all: the app keeps the values it has and logs an error naming the variable.

### Values Read Only at Start

Some values can't change in a running app: a listen address, or a database
URL read once to open a pool. The compiler finds the keys the program reads
while it starts — in package variables, `init`, `main`, `Start` methods and
the constructors passed to the app — and records them. A change to one is
logged as needing a restart:

```text
config: DATABASE_URL changed and takes effect on restart
```

With `VUKA_CONFIG_RESTART=1` the app also stops gracefully and exits with
status 75, for its supervisor to start it again with the new value. With
several instances, they restart one at a time only if the supervisor
staggers them.

A key also read through `web.Hot` or `web.OnEnvChange` isn't counted: the
app handles its changes. A key the compiler can't read — not a constant — is
counted by `vuka vet` ([W0405](/errors/W0405)).

## Secrets

A secret is stored encrypted to one app's key, so neither the repository nor
the server can read it. Make the app's key pair:

```sh
vuka-config keygen-app orders
# secret key (VUKA_CONFIG_SECRET_KEY): …
# app key line (--app-keys file):      orders=…
```

Give the server the app key lines (`--app-keys FILE` or
`VUKA_CONFIG_APP_KEYS`). Encrypt a value from standard input, so it never
lands in shell history, and put the line in the app's folder, such as
`orders/.env.production`:

```sh
printf %s "$PASSWORD" | vuka-config encrypt --app orders --var DB_PASSWORD \
    --server https://config.internal --key "$VUKA_CONFIG_KEY"
# DB_PASSWORD={cipher}v1:…
```

`--server` fetches the app's public key from the server and checks its
signature; `--pub` gives the key directly. An encrypted value is bound to its
app and its variable: copied to another app or another name, it won't
decrypt.

The app's private key is the one secret a deploy still delivers: set
`VUKA_CONFIG_SECRET_KEY` next to `VUKA_CONFIG_TOKEN`. Several keys,
comma-separated, are accepted while one is being replaced. A secret the app
can't decrypt stops it from starting, naming the variable. Secrets can't go
in the shared root files, which every app reads.

For clients that aren't Vuka apps and read the `.env` form
(`GET /orders/production.env`), `serve --decrypt` decrypts values encrypted
to the server's own key (`keygen-app --server`, `encrypt --server-key`).
The server can then read those secrets; it is off by default.

## Reference

| Endpoint | |
|---|---|
| `GET /{app}/{env}[/{label}]` | the app's merged values, signed (JSON) |
| `GET /{app}/{env}[/{label}].env` | the same as an env file |
| `GET /{app}/{env}/watch[?label=…]` | a stream of the snapshot's version |
| `GET /_keys/{app}` | the app's public key, signed |
| `POST /_hooks/git` | fetch the repository now |

`-` stands for no environment: `GET /orders/-`.
