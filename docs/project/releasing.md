# Releasing (maintainers)

The set — vuka, ui, web, cmd/vuka and cmd/vukavet — is released by one
command, run in a vuka checkout with the ui and web checkouts beside it:

```text
work/
  vuka/       github.com/vuka-lang/vuka (and cmd/vuka, cmd/vukavet)
  vuka-ui/    github.com/vuka-lang/ui
  vuka-web/   github.com/vuka-lang/web
```

```sh
cd work/vuka
vuka release v1.1.0          # checks, then prints the plan; changes nothing
vuka release -yes v1.1.0     # does it
```

`-ui` and `-web` point at checkouts elsewhere.

## Before you run it

Write the release's section in `docs/releases.md`, headed with the version
and date, for app authors: what changed, what to do, what `vuka fix` does.

```md
## v1.1.0 — 2026-11-02
```

A section marked `unreleased` passes a dry run with a warning and stops
`-yes`.

A deprecation or (in a major) a breaking change also needs its `vuka fix` fixer (`cmd/vuka/fix.go`) and an
`upgradeSteps` entry naming it (`cmd/vuka/upgrade.go`), so `vuka upgrade`
runs it for apps crossing the release:

<!-- go:context
type upgradeStep struct {
	version string
	fixers  []string
	note    string
}
-->

```go
var upgradeSteps = []upgradeStep{
	{"v1.2.0", []string{"url-tags"}, "url:\"q\" tags become @ui.URL(\"q\")"},
}
```

## Checks

Every run, dry or not, checks:

- each repository is on `main`, has no uncommitted changes, and is level
  with `origin/main` (after a fetch);
- each module is the one expected, and `cmd/vuka/toolchain.go` has its four
  version constants;
- the version is `vX.Y.Z`, at least v0.50.0, after every tag of every module
  of the set, and needs no `/vN` module path;
- `docs/releases.md` has its section;
- the patch train: a patch release within seven days of the set's last one is
  refused unless `-security` says it is a security fix;
- `go vet ./...` and `go test ./...` pass in each module, run against the
  checkouts being released: for the run, a module's requirements of vuka,
  ui and web are replaced by their checkouts, as CI does, and its go.mod and
  go.sum are put back after (`-no-test` skips them, for a quick look at the
  plan).

A failed check stops the release before anything changes.

## What it does

In dependency order, so each module requires tags that exist:

1. tag vuka `vX.Y.Z` (annotated) and push it;
2. ui: `go get github.com/vuka-lang/vuka@vX.Y.Z`, `go build ./...`, commit,
   tag, push;
3. web: `go get` vuka and ui at `vX.Y.Z`, build, commit, tag, push;
4. cmd/vuka: write `vX.Y.Z` into `toolchain.go` (the command's version and the
   set `vuka new` pins), `go get` vuka and the other libraries of the set its
   go.mod requires, build, commit, tag `cmd/vuka/vX.Y.Z`, push;
5. cmd/vukavet: tag `cmd/vukavet/vX.Y.Z` on the same commit and push it
   (it requires none of the set);
6. wait for proxy.golang.org to serve the five modules (`-proxy-wait`, 10m by
   default; 0 skips it; a slow proxy is a warning, not a failure);
7. install `github.com/vuka-lang/vuka/cmd/vuka@vX.Y.Z` and
   `github.com/vuka-lang/vuka/cmd/vukavet@vX.Y.Z` with `GOPROXY=direct` into
   a temporary directory and check that its `vuka version` reports the set.

The go commands run with `GOPROXY=direct GOSUMDB=off GOWORK=off`, so fresh
tags resolve at once and a go.work doesn't leak local checkouts into a
release. It never runs `go mod tidy`.

## The ORM

`github.com/vuka-lang/orm` has versions of its own and isn't part of
`vuka release`. It requires vuka and web, so a release of it that should
require a new set comes after that set: in an orm checkout on main,

```sh
GOPROXY=direct GOSUMDB=off GOWORK=off go get github.com/vuka-lang/vuka@vX.Y.Z github.com/vuka-lang/web@vX.Y.Z
go build ./... && go vet ./... && go test ./...
git commit -am "orm vA.B.C: require vuka, web vX.Y.Z"
git tag -a vA.B.C -m "orm vA.B.C"
git push origin HEAD:refs/heads/main refs/tags/vA.B.C
```

`ormVersion` in `cmd/vuka/new.go` is the orm `vuka new` pins and `vuka
upgrade` moves apps to; a release of the set whose scaffolds need a new orm
sets it in the release's pull request and releases the orm right after the
set.

## When a step fails

Tags are never moved or deleted. Fix the cause and run the same command
again: a module whose tag is already on its branch is skipped, and the
release carries on with the next. A step that failed after its commit but
before its tag leaves the repository ahead of `origin/main`; push that commit
(or reset it) so the checks pass, then rerun.

## The VS Code extension

The extension in `editors/vscode` has its own version and is published by the
`Extension` workflow when a `vscode-vX.Y.Z` tag is pushed (or by hand from the
Actions tab). The tag must match `version` in its `package.json`.

```sh
git tag vscode-v0.5.0 && git push origin vscode-v0.5.0
```

The run tests the grammars, packages `vuka.vsix` (kept as an artifact), then
publishes it:

- **Open VSX** (VSCodium and other VS Code–based editors) with the `OVSX_PAT`
  secret: a token from open-vsx.org for an account that owns the `vuka-lang`
  namespace (`npx ovsx create-namespace vuka-lang -p <token>`, once).
- **VS Code Marketplace** with the `VSCE_PAT` secret: an Azure DevOps token
  with the Marketplace "Manage" scope for the `vuka-lang` publisher. Skipped
  while the secret isn't set.
