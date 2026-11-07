# Versions

[[toc]]

## Introduction

An app has one vuka version to think about. This page says what that version
means, when releases happen, and what may change between them.

## One Version for the Toolchain {#one-version-for-the-toolchain}

The set is five modules:

| Module | What it is |
|---|---|
| `github.com/vuka-lang/vuka/v2` | the language runtime your generated code imports |
| `github.com/vuka-lang/ui/v2` | components, templates, live sessions |
| `github.com/vuka-lang/web/v2` | the web framework: routes, pages, islands, auth |
| `github.com/vuka-lang/vuka/cmd/vuka/v2` | the `vuka` command |
| `github.com/vuka-lang/vuka/cmd/vukavet/v2` | `go vet` with vuka's checks, for apps in plain Go |

From v0.50.0 they are released together under one version, a **set**: vuka
v1.0.0 is the runtime, ui, web, the command and vukavet, all tagged v1.0.0
and tested together. An app requires the three libraries at the same version
and uses the command of that version. `vuka version` shows the set an app
uses:

```text
$ vuka version
vuka v2.0.0 (vuka, ui, web v2.0.0)

example.com/shop:
  github.com/vuka-lang/vuka/v2  v2.0.0
  github.com/vuka-lang/ui/v2    v2.0.0
  github.com/vuka-lang/web/v2   v2.0.0
```

`vuka version` warns when an app's go.mod mixes sets, for example after a
`go get` of one module alone:

```text
warning: example.com/shop is not one vuka set (vuka v2.0.0, ui v2.0.0, web v2.1.0); vuka upgrade moves it to one
```

### The ORM's Versions

`vuka new` writes a go.mod pinned to its own set. The ORM,
`github.com/vuka-lang/orm/v2`, is released with the set under the same version,
tagged right after it, since it requires vuka and web. Versions up to v2.15.0
had their own numbering, so an app on an older one moves to the set's version
on its next `vuka upgrade`.

## Upgrading

Install the latest command, then move the app to a set:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
vuka upgrade            # the latest set
vuka upgrade v2.1.0     # or a chosen one
vuka upgrade -n         # say what it would do, change nothing
vuka upgrade main       # main's current commits, to try unreleased work
vuka upgrade -self main # main's command, compiling with main's vuka
```

In a Go app with the command as a tool of the module, it is
`go tool vuka upgrade`; or `go get` vuka, ui and web at one version
(`go get github.com/vuka-lang/vuka/v2@v2.1.0 github.com/vuka-lang/ui/v2@v2.1.0 github.com/vuka-lang/web/v2@v2.1.0`)
and run `go vet -vettool=$(go tool -n vukavet) ./...` and `go test ./...`.

### What vuka upgrade Does

`vuka upgrade` moves the app's go.mod to the set (`go get` of vuka, ui and
web, and — to the command's own set — of the orm, at the version its new
apps use, when the app requires an older one; it never runs `go mod tidy`,
which would drop requirements only `.vuka` files use), then runs `vuka vet ./...` and
`vuka test ./...`, and prints a summary.
The go.mod and go.sum it started from are kept in `.vuka/upgrade/`, so an
upgrade you don't want is undone with:

```sh
mv .vuka/upgrade/go.mod go.mod; mv .vuka/upgrade/go.sum go.sum
```

New tags are fetched straight from their repositories (`GOPROXY=direct`), so
a release can be used the hour it is tagged.

`vuka upgrade main` moves the app to the main branches instead: vuka, ui, web
and, when the app requires them, the orm and the `cmd/vuka` and `cmd/vukavet`
tools, each at its current main commit's pseudo-version. It runs the rewrites
of every release after the app's, including those main has for a release not
yet tagged, then the same checks, and the summary names each module's commit.
The commits are not a set tested together, so this is for trying unreleased
work, not for shipping. The rewrites are the command's, so a command installed
from a release runs main's to do it, with the same flags, built against vuka's
main (see below). `vuka version` reports the app as on main, with the commits. A pseudo-version (`v2.17.1-0.…`) sorts below the
next release, so `vuka upgrade` once it is tagged moves the app back to tags.

### The Command's Compiler {#command-compiler}

The compiler is part of the vuka module, and the command links it at the
version its own go.mod pins. A compiler fix merged to main reaches the
command only once that pin moves, which is a later commit: so
`go install github.com/vuka-lang/vuka/cmd/vuka/v2@main` can install a command
whose compiler is older than main's. `vuka upgrade -self main` installs main's
command built against vuka's main instead, in a temporary module that
requires both at main; `vuka upgrade main` runs main's command built the same
way.

```sh
vuka upgrade -self          # the newest release's command
vuka upgrade -self v2.17.0  # a release's
vuka upgrade -self main     # main's, compiling with main's vuka
vuka upgrade -self -n main  # say what it would run
```

It installs into `GOBIN` (or `GOPATH/bin`), prints the command's version and
the vuka it compiles with, and changes no app. `vuka version` shows that vuka
too, and a command that compiles an app (build, run, test, vet, lsp, …)
warns once when it is older than the one the app's go.mod requires:

```text
warning: this vuka command compiles with vuka 9156bdf7ad26, older than the b616b7547f57 example.com/shop requires;
         its fixes since aren't applied: vuka upgrade -self main (or go tool vuka, which builds against the app's go.mod)
```

`go tool vuka`, with the command a tool of the app's module, always builds
against the app's go.mod and never needs this.

## Release Cadence {#release-cadence}

- **Fixes land on main continuously.** They are released together, as a
  patch release (v1.0.1, v1.0.2, …), once a week. `vuka release` refuses a
  second patch within seven days of the last.
- **Security fixes ship at once**, as a patch release outside the weekly
  train.
- **Minor releases** (v1.1.0) carry features. Each is announced with
  upgrade notes: what changed, what to do, and the required source changes.

Every release has a section in [Releases](/releases), written for app
authors.

## The Compatibility Promise {#the-compatibility-promise}

From v1.0.0 an app that builds and passes its tests on v1.x keeps doing so
on every later v1: minor releases add, patch releases fix, and neither
removes or changes what the promise covers — the Go API of vuka, ui, web
and orm, the `.vuka` and `.vuka.html` languages, the browser API, the
command's subcommands and flags, diagnostic codes and the generated-code
contract. A deprecated name keeps working, with a `vuka vet` warning naming
its replacement; a breaking change waits for a major version, with a
documented source change and a before/after example.

v2.16.0 is an exception: its [release notes](/releases) list each breaking
change and what to do about it.

[The Compatibility Promise](/project/compatibility) says exactly what is
covered, what a release may still change, and what is not covered.

### Before 1.0

From v0.50.0 to v1.0.0, a minor release could change what the promise
covers, after a deprecation of one minor release, with a documented source change
that `vuka upgrade` ran and a before/after example in the release note.
v1.0.0 made the last such changes; [Releases](/releases) lists them
and what `vuka upgrade` does about each.
