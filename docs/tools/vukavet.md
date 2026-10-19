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
	help: give the group a loader in the page's LiveSchema:
		l.Loads("profile", p.loadProfile)
main.go:17:29: E0405: GET /api/users/{id} (ShowUser): parameter #1 int binds by name, and it has none
	help: in plain Go, name it with web.Params
main.go:18:6: E0401: GET /api/users is declared twice: by ListUsers at main.go:16 and by ListUsers at main.go:18
	help: give one of them another path or method
```

`-vettool` replaces vet's analyzers, so `vukavet` runs them too: it is
`go vet` plus the checks below. Each check is a flag of its own
(`-routes=false`), and `go tool vukavet ./...` runs it without `go vet`.

## Checks

| Check | Reports |
|---|---|
| `routes` | on `web.Routes.X(…)` and `app.X(…)`: a method and path declared twice, in the package or by a package it imports ([E0401](/errors/E0401)); a handler that isn't a function ([E0403](/errors/E0403)); a scalar parameter `web.Params` doesn't name ([E0405](/errors/E0405)); results the app can't write ([E0406](/errors/E0406)); `web.Params` with more names than parameters, or a name twice ([E0419](/errors/E0419)); a service's or controller's constructor of the wrong shape ([E0418](/errors/E0418)); a page backed by its island under another method than GET ([E0331](/errors/E0331)) |
| `live` | a live page's constructor or struct ([E0329](/errors/E0329), [E0330](/errors/E0330)), its `LiveSchema` method: `l.Field` given something other than the address of the receiver's field, or a nil attribute ([E0335](/errors/E0335)), a hook given nil, a group that isn't a name or loaded twice ([E0336](/errors/E0336)); and what `ui.CheckIsland` refuses of a page backed by its island: `ui.Bind` or `bind:` ([E0322](/errors/E0322)), `ui.Defer` or `defer:` ([E0323](/errors/E0323)), a deferred group without a loader or with a wrong one ([E0324](/errors/E0324), [E0325](/errors/E0325)), two handles of one name ([E0326](/errors/E0326)), `Changed` ([E0327](/errors/E0327)), `validate:` ([E0328](/errors/E0328)), `Mount` ([E0333](/errors/E0333)) |
| `templates` | a `ui.HTML` call given more or fewer values than its labels ([E0127](/errors/E0127)), a value not written as its label ([E0132](/errors/E0132)), or a value whose type its hole's place can't take ([E0128](/errors/E0128)); in a `.vuka.html`, a bare name that is both a field and a package-level name ([W0101](/errors/W0101)); a live list without keys ([W0102](/errors/W0102)) and a stateful component in a loop without one ([W0103](/errors/W0103)) |
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
