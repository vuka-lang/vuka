# vukavet

[[toc]]

## Introduction

`vukavet` is `go vet` with vuka's checks. A vuka app written in plain Go
builds with `go build` and no `vuka` command; what the app would refuse at
boot, or fail at render, `vukavet` reports from the source, with the same
codes as the boot, the compiler and `vuka vet` ([`vuka explain
E0401`](/tools/cli#explain), [the catalogue](/errors/)).

## Running vukavet {#running}

Add it as a tool of your module and hand it to `go vet`:

```sh
go get -tool github.com/vuka-lang/vuka/cmd/vukavet/v2
go vet -vettool=$(go tool -n vukavet) ./...
```

It reports each problem with its code and a help line:

```
users.go:66:2: E0324: Profile.Bio is deferred, and the group Profile has no loader
	help: mark its loader @ui.Loads("profile")
main.go:17:29: E0405: GET /api/users (ShowUser): parameter #2 int binds the path segment its position gives, and the path has none left for it
	help: take the query in a struct, func(q Query), or add the segment to the path
main.go:18:6: E0401: GET /api/users is declared twice: by ListUsers at main.go:16 and by ListUsers at main.go:18
	help: give one of them another path or method
```

### Running the Checks Alone

`-vettool` replaces vet's analyzers, so `vukavet` runs them too: it is
`go vet` plus the checks below. Each check is a flag of its own
(`-routes=false`), and `go tool vukavet ./...` runs it without `go vet`.

## Checks

The checks are:

| Check | Reports |
|---|---|
| `routes` | generated page routes and API handlers: duplicate paths, invalid signatures, unfillable typed parameters, unsupported results and invalid service constructors; folder layouts reading the request, or the page's title in their `Load` ([W0703](/errors/W0703)) |
| `live` | decorated hooks, bound fields, deferred groups, typed handler parameters and tracked server state |
| `templates` | shape-file fields and markup, ambiguous names, missing keys in live loops and invalid hole values |
| `schema` | an orm model's `Schema` method: `s.Field` given something other than the address of the receiver's field ([E0501](/errors/E0501)), a nil attribute ([E0502](/errors/E0502)), a field behind a pointer ([E0503](/errors/E0503)) |

A check reports only what the source decides: a path built at run time, a
value of an interface type in a template hole or a route a package
registers for an app it never sees is left to the boot.

## Checking a .vuka App {#a-vuka-app}

`vuka vet` runs the same checks on the Go a `.vuka` app becomes, with its
positions:

```sh
vuka vet ./...
```

Its reports point into the `.vuka` source:

```
decorated/users.vuka:36: E0401: GET /api/users is declared twice: by ListUsers at users.vuka:32 and by ShowUser at users.vuka:36
```

### Which vukavet Runs

It hands go vet a vukavet as `-vettool`, the first of:

1. `VUKA_VUKAVET`, a path (`off` runs go vet's own analyzers only);
2. the app's own, `go tool -n vukavet`, when its `go.mod` declares the
   tool (`go get -tool github.com/vuka-lang/vuka/cmd/vukavet/v2`) — the
   way to pin it with the app;
3. a `vukavet` beside the `vuka` command, then one on `PATH`, built at
   this vuka's version;
4. one `vuka vet` builds at its own version (`go install …@version`, once,
   into the user's cache directory).

When none can be found or built, `vuka vet` says so and why on stderr and
runs go vet's own analyzers. `-vettool=…` replaces the choice:
`vuka vet -vettool=$(go tool -n vukavet) ./...` still works.

## In Editors and Linters {#editors}

gopls runs only its own analyzers, so `vukavet`'s reports come from
`go vet`: run it on save or in CI. In `.vuka` files [the editor](/tools/editors)
reports the island checks itself, as `vuka vet` does.

For a linter that takes `go/analysis` analyzers, they are
`check.Analyzers` of `github.com/vuka-lang/vuka/cmd/vukavet/v2/check`.
