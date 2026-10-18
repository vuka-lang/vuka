# Releasing (maintainers)

The set — vuka, ui, web and cmd/vuka — is released by one command, run in a
vuka checkout with the ui and web checkouts beside it:

```text
work/
  vuka/       github.com/vuka-lang/vuka (and cmd/vuka)
  vuka-ui/    github.com/vuka-lang/ui
  vuka-web/   github.com/vuka-lang/web
```

```sh
cd work/vuka
vuka release v0.51.0         # checks, then prints the plan; changes nothing
vuka release -yes v0.51.0    # does it
```

`-ui` and `-web` point at checkouts elsewhere.

## Before you run it

Write the release's section in `docs/releases.md`, headed with the version
and date, for app authors: what changed, what to do, what `vuka fix` does.

```md
## v0.51.0 — 2026-11-02
```

A section marked `unreleased` passes a dry run with a warning and stops
`-yes`.

A breaking change also needs its `vuka fix` fixer (`cmd/vuka/fix.go`) and an
`upgradeSteps` entry naming it (`cmd/vuka/upgrade.go`), so `vuka upgrade`
runs it for apps crossing the release:

```go
var upgradeSteps = []upgradeStep{
	{"v0.52.0", []string{"url-tags"}, "url:\"q\" tags become @ui.URL(\"q\")"},
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
- `go vet ./...` and `go test ./...` pass in each module (`-no-test` skips
  them, for a quick look at the plan).

A failed check stops the release before anything changes.

## What it does

In dependency order, so each module requires tags that exist:

1. tag vuka `vX.Y.Z` (annotated) and push it;
2. ui: `go get github.com/vuka-lang/vuka@vX.Y.Z`, `go build ./...`, commit,
   tag, push;
3. web: `go get` vuka and ui at `vX.Y.Z`, build, commit, tag, push;
4. cmd/vuka: write `vX.Y.Z` into `toolchain.go` (the command's version and the
   set `vuka new` pins), `go get` vuka, build, commit, tag
   `cmd/vuka/vX.Y.Z`, push;
5. wait for proxy.golang.org to serve the four modules (`-proxy-wait`, 10m by
   default; 0 skips it; a slow proxy is a warning, not a failure);
6. install `github.com/vuka-lang/vuka/cmd/vuka@vX.Y.Z` with `GOPROXY=direct`
   into a temporary directory and check that its `vuka version` reports the
   set.

The go commands run with `GOPROXY=direct GOSUMDB=off GOWORK=off`, so fresh
tags resolve at once and a go.work doesn't leak local checkouts into a
release. It never runs `go mod tidy`.

## When a step fails

Tags are never moved or deleted. Fix the cause and run the same command
again: a module whose tag is already on its branch is skipped, and the
release carries on with the next. A step that failed after its commit but
before its tag leaves the repository ahead of `origin/main`; push that commit
(or reset it) so the checks pass, then rerun.
