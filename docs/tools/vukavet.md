# vukavet

`vukavet` is `go vet` with vuka's checks. A vuka app written in plain Go
builds with `go build` and no `vuka` command; what the app would refuse at
boot, or fail at render, `vukavet` reports from the source, with the same
codes as the boot, the compiler and `vuka vet` ([`vuka explain
E0401`](/tools/cli#explain), [the catalogue](/errors/)).

```sh
go get -tool github.com/vuka-lang/vuka/cmd/vukavet
go vet -vettool=$(go tool -n vukavet) ./...
```

```
users.go:66:2: E0324: Profile.Bio is deferred, and the group Profile has no loader
	help: mark its loader @ui.Loads("profile")
main.go:17:29: E0405: GET /api/users (ShowUser): parameter #2 int binds the path segment its position gives, and the path has none left for it
	help: take the query in a struct, func(q Query), or add the segment to the path
main.go:18:6: E0401: GET /api/users is declared twice: by ListUsers at main.go:16 and by ListUsers at main.go:18
	help: give one of them another path or method
```

`-vettool` replaces vet's analyzers, so `vukavet` runs them too: it is
`go vet` plus the checks below. Each check is a flag of its own
(`-routes=false`), and `go tool vukavet ./...` runs it without `go vet`.

## Checks

| Check | Reports |
|---|---|
| `routes` | generated page routes and API handlers: duplicate paths, invalid signatures, unfillable typed parameters, unsupported results and invalid service constructors |
| `live` | decorated hooks, bound fields, deferred groups, typed handler parameters and tracked server state |
| `templates` | shape-file fields and markup, ambiguous names, missing keys in live loops and invalid hole values |
| `schema` | an orm model's `Schema` method: `s.Field` given something other than the address of the receiver's field ([E0501](/errors/E0501)), a nil attribute ([E0502](/errors/E0502)), a field behind a pointer ([E0503](/errors/E0503)) |

A check reports only what the source decides: a path built at run time, a
value of an interface type in a template hole or a route a package
registers for an app it never sees is left to the boot.

## A `.vuka` app

The same checks run on the Go a `.vuka` app becomes, with its positions:

```sh
vuka vet -vettool=$(go tool -n vukavet) ./...
```

```
decorated/users.vuka:36: E0401: GET /api/users is declared twice: by ListUsers at users.vuka:32 and by ShowUser at users.vuka:36
```

## Editors

gopls runs only its own analyzers, so `vukavet`'s reports come from
`go vet`: run it on save or in CI. In `.vuka` files [the editor](/tools/editors)
reports the island checks itself, as `vuka vet` does.

For a linter that takes `go/analysis` analyzers, they are
`check.Analyzers` of `github.com/vuka-lang/vuka/cmd/vukavet/check`.
