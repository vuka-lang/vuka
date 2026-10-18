# Versions and compatibility

An app has one vuka version to think about. This page says what that version
means, when releases happen, and what may change between them.

## One version for the toolchain

vuka is four modules:

| Module | What it is |
|---|---|
| `github.com/vuka-lang/vuka` | the language runtime your generated code imports |
| `github.com/vuka-lang/ui` | components, JSX nodes, live sessions |
| `github.com/vuka-lang/web` | the web framework: routes, pages, islands, auth |
| `github.com/vuka-lang/vuka/cmd/vuka` | the `vuka` command |

From v0.50.0 they are released together under one version, a **set**: vuka
v0.50.0 is the runtime, ui, web and the command, all tagged v0.50.0 and
tested together. An app requires the three libraries at the same version and
uses the command of that version.

```text
$ vuka version
vuka v0.50.0 (vuka, ui, web v0.50.0)

example.com/shop:
  github.com/vuka-lang/vuka  v0.50.0
  github.com/vuka-lang/ui    v0.50.0
  github.com/vuka-lang/web   v0.50.0
```

`vuka version` warns when an app's go.mod mixes sets, for example after a
`go get` of one module alone:

```text
warning: example.com/shop is not one vuka set (vuka v0.50.0, ui v0.50.0, web v0.51.0); vuka upgrade moves it to one
```

`vuka new` writes a go.mod pinned to its own set. Other modules an app uses,
such as the ORM, keep versions of their own.

## Upgrading

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
vuka upgrade            # the latest set
vuka upgrade v0.51.0    # or a chosen one
vuka upgrade -n         # say what it would do, change nothing
```

`vuka upgrade` moves the app's go.mod to the set (`go get` of vuka, ui and
web; it never runs `go mod tidy`, which would drop requirements only `.vuka`
files use), runs the `vuka fix` rewrites registered for the releases in
between, then `vuka vet ./...` and `vuka test ./...`, and prints a summary.
The go.mod and go.sum it started from are kept in `.vuka/upgrade/`, so an
upgrade you don't want is undone with:

```sh
mv .vuka/upgrade/go.mod go.mod; mv .vuka/upgrade/go.sum go.sum
```

New tags are fetched straight from their repositories (`GOPROXY=direct`), so
a release can be used the hour it is tagged.

## Release cadence

- **Fixes land on main continuously.** They are released together, as a
  patch release (v0.50.1, v0.50.2, …), once a week. `vuka release` refuses a
  second patch within seven days of the last.
- **Security fixes ship at once**, as a patch release outside the weekly
  train.
- **Minor releases** (v0.51.0) carry features. Each is announced with
  upgrade notes: what changed, what to do, and what `vuka fix` does for you.

Every release has a section in [Releases](/releases), written for app
authors.

## The compatibility promise

### What it covers

- the `.vuka` language: syntax, built-in attributes and decorators;
- the exported Go API of vuka, ui and web;
- the browser API pages and islands use: `live.*`, `useVuka()`,
  `VukaLink`, `window.vuka`;
- the `vuka` command's subcommands and flags, and the environment
  variables it reads;
- diagnostic codes (a code keeps its meaning; its wording may improve).

Not covered: the Go code vuka generates (read it, don't depend on its
shape), `internal` packages, the wire protocol between a page and its server
(both come from the same set), and the development toolbar.

### Patch releases

A patch release never breaks a working app. It fixes bugs, closes security
holes and improves messages. If a fix has to change behaviour an app could
rely on, the release note says so and shows how to keep the old behaviour.

### Minor releases before 1.0

A minor release may change or remove what the promise covers, under three
rules:

1. **Deprecate first.** The old form keeps working for at least one minor
   release. It is marked `@deprecated("use X")` (`// Deprecated: use X` in
   Go), and `vuka vet` warns where it is used, naming the replacement.
2. **Ship the rewrite.** Every breaking change comes with a `vuka fix`
   fixer that makes the edit, and `vuka upgrade` runs it when it crosses
   that release.
3. **Show it.** The release note has a before/after example.

For example, a field attribute replacing a struct tag:

```vuka
// before (v0.51)
Q string `url:"q"`

// after (v0.52) — vuka fix url-tags rewrites it
Q string @ui.URL("q")
```

v0.51 adds `@ui.URL` and makes `url:""` a vet warning; v0.52 may drop the tag.
An app upgrading from v0.50 to v0.52 in one step gets the rewrite applied by
`vuka upgrade`.

### After 1.0

From v1.0.0, Go's own rule: a program that builds and passes its tests on
v1.x keeps doing so on every later v1. Minor releases add; they don't remove
or change what the promise covers. Deprecated names keep working for the
whole major version, with the vet warning. Exceptions are the ones Go makes:
security problems, and bugs whose behaviour no correct program relies on;
both are called out in the release note.

A v2, if it ever comes, is a new module path (`…/v2`), with `vuka fix`
rewriting the imports and every renamed or removed API.
