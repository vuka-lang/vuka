# The vuka Command

[[toc]]

## Introduction

The `vuka` command creates apps, grows them a piece at a time, and builds,
runs, tests and vets them through the go command. It also formats `.vuka`
files, explains what the compiler makes of them, and serves your editor.

## Command Overview

These are the subcommands and their flags:

```text
vuka new <dir> [module path] [--ui=… --db=… --mail=… --auth=… --frontend=… --name=… --yes --force]
vuka add module|service|component|island|page|entity … [--dry-run] [--force]
vuka build|run|test|vet|install [go flags] [packages]
vuka run -watch [-debug] [-fresh] [go flags] [package] [args]
vuka mod tidy|why|vendor|graph|… [args]
vuka build-islands [-watch]
vuka graphql schema [-check file] [-o file] [package]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka eject [-rm] [file.vuka… | dir | dir/...]
vuka explain [-full] file.vuka
vuka explain E0201
vuka fmt [-l] [-w] [-d] [-check] [-rewrite] [-imports] [paths…]
vuka lsp [-gopls path] [-log file] [-shared=false]
vuka version
vuka upgrade [-n] [-no-test] [version]
vuka release [-yes] [-no-test] [-security] vX.Y.Z   (maintainers)
```

## Creating an App {#new}

`vuka new` starts an app you can grow: pages, sign-in, a database with migrations,
mail, styles and tests, wired together and working before you write a
line. [Getting started](/web/getting-started) walks through what it makes.

You can name the module path and answer questions with flags:

```sh
vuka new shop                       # asks five questions
vuka new shop example.com/shop      # a module path other than the directory's name
vuka new shop --db=postgres --frontend=react --yes
```

### Answering the Questions

It asks, with the arrow keys (or the item's number) and Enter:

| Question | Choices (first is the default) | Flag |
|---|---|---|
| UI | Live pages + Tailwind · templ views + Tailwind · API only | `--ui=live\|templ\|api` |
| Database | SQLite · PostgreSQL · MySQL · none | `--db=sqlite\|postgres\|mysql\|none` |
| Mail | logged in development, SMTP in production · none | `--mail=yes\|no` |
| Authentication | yes, with a login page · no | `--auth=yes\|no` |
| Frontend app | none · React · Vue · Svelte | `--frontend=none\|react\|vue\|svelte` |

A flag answers its question; `--yes` (`-y`) takes the default for every
question no flag answers. Without a terminal — a pipe, CI — nothing is
asked: the flags and the defaults are used, and the command never waits
for input. `--name` is the app's name as people read it (`APP_NAME`; the
directory's name, capitalised, by default).

### What It Sets Up

- **Sign-in needs a database.** It keeps its users, sessions and tokens
  there, so sign-ins survive restarts and replicas share them. With
  `--db=none` the question isn't asked and the app has none;
  `--db=none --auth=yes` is refused, saying why.
- **The first admin** is created when the app first starts, from
  `ADMIN_EMAIL` and `ADMIN_PASSWORD` in `.env`. `vuka new` fills in a
  strong random password and prints it once. `.env` is in `.gitignore`;
  `.env.example`, committed, has placeholders. The app never asks for a
  password at start-up.
- **SQLite** needs no setup: the file is made under `data/` on the first
  run, pure Go, no CGO. PostgreSQL and MySQL get a `DATABASE_URL` to fill in
  and their driver's import; the generated tests run on SQLite either way.
- **A frontend** is a Vite project in `frontend/`, served at `/app`
  (`web.FrontendAt`), with `APP_NAME` passed to it (`web.FrontendEnv`).
  `vuka run` starts its dev server, `vuka build` builds it into the binary.
- **A directory that isn't empty** is refused; `--force` writes the app
  beside what is there, keeping every file it finds (and saying which).

After writing the files it runs `vuka mod tidy`, then prints the next
steps. `vuka test ./...` passes in the new app as it is.

## Adding to an App {#add}

### Adding a Module

`vuka add module <name>` grows an app `vuka new` made, a module at a time:

```sh
vuka add module invoices
```

This writes `modules/invoices/` — an `Invoice` model, an `Invoices` service, a
page at `/invoices` linked in the navigation (or `/api/invoices` routes in
an API app), a test of the service — and `migrations/000N_invoices.go`
making its table, after the latest migration. It adds the module's import
to `main.vuka`, which is all a module needs to be part of the app: its
routes, pages and services declare themselves.

The app applies the migration when it next starts. The name is a plural noun in lower case;
an existing module, or an app without a database, is refused.

It is `add`, not a form of `new`: `new` makes a project, `add` grows the
one you are in, and a project directory named `module` stays possible.

### Adding Services, Components, Islands, Pages and Entities

The other kinds add one piece each:

| | |
|---|---|
| `vuka add service Mailer [--module notes]` | a service in `services/` (or `modules/notes/`; `notes/Mailer` is the same) with its constructor `NewMailer` and a test, registered in `main.vuka`'s `services` with `app.Service(notes.NewMailer)`: pages, routes and hooks take a `*notes.Mailer` parameter |
| `vuka add component StatCard [--module m]` | a stateless component, in `components/` or the module: the struct of its props in `stat_card.vuka`, its markup in `stat_card.vuka.html` |
| `vuka add island Chart [--module m] [--vue\|--react\|--svelte] [--dir d]` | `web/islands/Chart.vue` (`.tsx`, `.svelte`; `web/islands/<m>/` with `--module`), its props typed for an import, and `components/chart.vuka` with `chart.vuka.html` (or in `modules/<m>/`) — a `Chart` component placing it, so pages write `<components.Chart title="Sales" count={3} />`; the framework's packages and Vite plugin are added when missing and installed, showing it is under way (`--no-install` prints the command instead) |
| `vuka add page /reports` | a one-struct `.vuka.html` page under `pages/`; use `[id]` names for parameters and `_layout.vuka.html` for shared layout. When the app's `main` doesn't pass `pages.Routes()` yet (an app with templ views, an API, or none of `pages/` so far), it adds the import and the option to `web.New` — see [serving the pages](#add-page-routes) |
| `vuka add page /board --island Board` | a page shape under `pages/` that imports its frontend component; layouts use `_layout.vuka.html`, hooks use decorators |
| `vuka add entity Comment --module posts body:text post:ref email:string:unique` | an orm model in `entities/` (or the module; `posts/Comment` is the same); `--unmanaged --table t` maps an existing table — no timestamps, test or migration, and no `migrations/` needed. Otherwise it comes with `ID`, `CreatedAt` and `UpdatedAt`, a test, and its migration written by `go tool orm makemigrations` |

### Entity Field Types

An entity's fields are `name:type`, `?` after the type for an optional one
(an `Option`), and `:unique` or `:index` after it:

| type | Go | column |
|---|---|---|
| `string` | `string` | `VARCHAR(255)` (`@orm.Char{Max: 255}`) |
| `text` | `string` | unbounded text (`@orm.LongText`) |
| `int`, `bigint` | `int64` | integer |
| `float` | `float64` | floating point |
| `decimal` | `float64` | `NUMERIC(12,2)` |
| `bool` | `bool` | boolean, default false |
| `time`, `date` | `time.Time` | a timestamp, a date |
| `json` | `orm.JSON[map[string]any]` | JSON |
| `ref` | `orm.FK[T]` | `<name>_id`, a foreign key to the model `T` the name names (`user:ref` or `user_id:ref` → `User`, in the module or another one); optional: `ON DELETE SET NULL` |

`--table` names the table (the plural of the name in snake_case otherwise).

### Serving the Pages {#add-page-routes}

A page under `pages/` is served once the app's `main` passes
`pages.Routes()` to `web.New`. An app made with live pages does from the
start; for any other, the first `vuka add page` (or `vuka add module`
writing a page) adds it:

```diff
 import (
 	"github.com/vuka-lang/web/v2"
+
+	"example.com/shop/pages"
 )
 …
-	web.New(web.Dev()).Run("")
+	web.New(pages.Routes(), web.Dev()).Run("")
```

It edits the main that owns `pages/` — the module root's, else the
module's only package main — in `main.go` or `main.vuka`, keeping its
comments and layout. When `web.New` spreads a list of options
(`web.New(options()...)`), `pages.Routes()` goes at the end of that list.
A main that passes it already is left as it is. When the module has
several mains and none passes it, or `web.New` can't be found, nothing is
edited: the command prints the line to add and the file it goes in.
`vuka fix routes` makes the same change in an existing app, the fix for
[W0701](/errors/W0701).

### Refusals and Dry Runs

Every kind refuses a name that isn't a Go identifier of the right case, a
declaration or file that exists (`--force` overwrites it), and a module that
doesn't; `--dry-run` lists what it would write and changes nothing. Files
are formatted as `vuka fmt` formats them, and a package nothing imports yet
is imported in `main.vuka`.

## Building, Running and Testing {#build-run-test-vet-install}

`vuka build`, `run`, `test`, `vet` and `install` are the go command's, with every `.vuka` file in the module transpiled into an
overlay (`go … -overlay`): nothing is written among your sources. `vuka build`
also refreshes [`build/`](/guide/how-it-works#the-build-module). Flags and
packages are passed to go as they are. `vuka build` precompresses what the
binary serves as it is: the stylesheet, the components' scripts and the
island chunks are embedded with their brotli and gzip, and `vite build` and
`vuka build-islands` write a `.br` and a `.gz` beside the files of a
frontend's build ([Compression and Caching](/web/deployment#compression)).

### When a Build Fails {#build-errors}

A failed build shows the errors of the packages that fail themselves,
each at the line and column of the `.vuka`, `.vuka.html` or `.go` file you
wrote, with the line and a caret under the code. Files that fail only
because they import one of those packages are counted in one line, since
fixing the package fixes them; `-v` lists them.

```text
$ vuka build
error[E0002]: not enough arguments in call to a.HomePath
  --> layouts/main.vuka.html:16:14
   |
16 |     <a href={a.HomePath()}>Home</a>
   |              ^^^^^^^^^^^^ HomePath takes (ctx context.Context)
   |
   = help: pass the request's context: a.HomePath(ctx)

error[E0002]: undefined: entities.UserColumns
  --> modules/accounts/service.vuka:14:6
   |
14 |     _ = entities.UserColumns
   |         ^^^^^^^^^^^^^^^^^^^
   |
   = note: UserColumns is generated by the orm for entities.User and wasn't generated for this build

2 errors in 2 packages · 3 pages couldn't build because of them
```

What a package can't do with the types of one that doesn't compile — a
field of an embedded type it can't find, a GraphQL result of an invalid
type, a variable whose only use is in such code — isn't its own error: its
files are counted with the others in the last line. A file that doesn't
parse has one error a broken declaration, and an `import` without a path
one error at the `import`, naming the imports the file's code needs.

The packages lower in the import graph come first: fixing them often
clears the rest. Every error of a package is shown, up to ten, and then
how many more there are; `vuka vet` shows them all. A Go error whose cause
is certain gets a hint:

- a call given too few or too many arguments shows what the function
  takes, and when the missing one is a `context.Context` in markup (or in
  Go with a `ctx` in scope), the call with `ctx` passed;
- an attribute (`@graphql.Query`) or a package (`strings.ToUpper`) used
  without its import names the import: one the package's other files have,
  the standard library's, or the toolchain's (`github.com/vuka-lang/web/v2/graphql`)
  when `go.mod` requires its module;
- an unused import says to delete it;
- a model's typed fields (`UserColumns`) that weren't generated say what
  generates them, and a type that isn't a model says so;
- a `urls` value the folder routes don't have says that the folders
  under `pages/` make them.

`vuka run -watch` reports a failed rebuild the same way. On a terminal each
build's errors replace the last one's on the screen, and the last line
says when it failed (`● build failed at 14:02:07; the last good build keeps
running`); written to a file or a pipe, the output is plain, every build's
errors one after the other. The [toolbar](/web/toolbar#while-vuka-run-watch-rebuilds)'s
build panel shows the same text, and the [editor](#lsp) shows each error at
its source, the import of a failing package naming that package's error.

### Running While You Edit {#run-watch}

`vuka run -watch` builds the app, runs it, and rebuilds it when a source
changes: `.vuka`, `.vuka.html`, `.templ` and `.go` files (not tests),
`go.mod`, `go.sum` and the files the app embeds. The running version keeps
serving while the next one compiles, and is swapped out only once the new
build is green; a build that fails prints its errors and leaves it running,
and one that comes out the same restarts nothing. `-debug` keeps the debug
information watch builds leave out, for delve.

**A start with nothing changed runs the last build at once.** Each good
build's binary is kept in vuka's cache directory (`vuka/run`, or
`VUKA_CACHE`), under a key hashing everything the build reads: the module's
sources (Go, `.vuka`, `.templ`, the island components `.vuka` files import;
and those of modules a `replace` or `go.work` points at),
`go.mod`, `go.sum` and `go.work`, the embedded files (by name only for the
`web.Frontend` files the build stubs), the build flags, the go command's
environment (`GOOS`, `GOARCH`, `GOFLAGS`, `CGO_*`, its `go env -w`
settings), the module's Go and this vuka. When the next `vuka run -watch`
finds a build under the key its sources have now, it starts that build
before transpiling anything and prints

```text
● reusing the last build (no changes)
```

It then builds as usual, in the background. A build that comes out the same
leaves the app running; one that comes out different, from an input the key
doesn't cover, is swapped in with a warning saying whether the transpiler's
or go build's inputs were missed. The last three builds of a module are
kept (at most 2 GB together), so switching branches back and forth reuses
too, and modules not run for two weeks are dropped. `-fresh` builds before
the app starts as before; `VUKA_REUSE=off` turns reuse off. `vuka build`
always builds.

### Incremental Transpiling

**Only what changed is transpiled again.** The Go each package became is
kept in the user's cache directory (`vuka/transpile`), keyed by the
package's sources, the API of the module's packages it imports, the go
command, the build environment, `go.mod`/`go.sum`/`go.work` and this vuka:
a build after a build transpiles the packages whose inputs changed, and a
change to a function's body leaves its importers alone.

The island build is
skipped the same way when nothing in the frontend root (its sources,
config, lockfile, installed packages, `@source` paths) changed.
`VUKA_CACHE=off` turns both off; `VUKA_CACHE=<dir>` keeps the caches
(transpiled packages, export data, island builds) in that directory instead
of the user's cache directory.

A build reads each directory of the module once, however many of its steps
look at it, and asks the go command for the export data of everything the
module imports with one `go list`, not one per import. Under `vuka run
-watch` a step of a rebuild that takes more than three seconds is named
while it runs (`● transpiling 35 packages… (3s so far)`) and timed when it
ends.

Under `vuka run` the files `web.Frontend`
serves are left out of the binary (empty in the overlay; it reads them from
disk in development), so they cost the linker nothing; `-embed` (with
`-watch`) or `VUKA_EMBED_STUB=off` builds them in.

### Vetting

`vuka vet` checks shape files, decorated hooks, field bindings, deferred
loaders and typed handler arguments as well as the generated Go. Missing
loaders, unfillable parameters and invalid markup are reported at source locations,
and so is a shape's script naming a ref, state or event the shape doesn't
have ([W0308](/errors/W0308)).
It runs [vukavet](/tools/vukavet)'s checks too, as go vet's `-vettool`: the
app's `go tool vukavet` when its `go.mod` declares it, else a `vukavet`
beside `vuka` or on `PATH`, else one it builds at its own version
([which](/tools/vukavet#a-vuka-app)); it says so when it can't.
`-vettool=…` overrides it, and `VUKA_VUKAVET=off` runs go vet's own
analyzers only.

**`vuka vet` reports what it was asked about.** Given directories
(`vuka vet ./entities/`, `./modules/...`), it transpiles the whole module, as
a build does, but reports only the warnings of those packages, and the
errors of those packages and of the module's packages they import, through
Go packages and Vuka packages alike — the errors that stop them building.
Another package's errors don't fail it; one line counts them (`(1 error in
packages not asked about, nor imported by them: vuka vet ./... lists it)`).
`vuka vet ./...`, no packages, or packages named by import path report the
whole module.

## Ejecting to Go {#eject}

`vuka eject` turns `.vuka` files into Go:

```sh
vuka eject                 # the module: gen -inplace
vuka eject pages/orders.vuka
vuka eject -rm ./...       # the Go becomes the source
```

It writes the Go generated for `.vuka` files beside them, as `gen -inplace`
does: for the files named, or the packages (`dir`, `dir/...`; the module by
default). With `-rm` the Go becomes the source: each `.vuka` file is deleted
and its Go kept as `orders.go` (`orders_vuka.go` when `orders.go` is taken),
without the generated-code header or line directives.

What's left is a plain Go module: `go build`, `go vet` and `go test` take it with no `vuka` command,
and it behaves as the `.vuka` version did. Eject the whole module (`./...`)
so the [link files](#build-run-test-vet-install) and the files embedding
CSS and assets are written too; they stay generated files.

Rendering remains authored in shape files. Page registrations are generated
from `pages/`, while services are explicitly registered on the app. Use
`vuka explain` to inspect lowering rather than copying generated rendering
or registration calls into application source.

## Printing the GraphQL Schema {#graphql}

`vuka graphql schema` prints the SDL of the schema the app serves with
[`web.GraphQL`](/web/graphql): it builds the main package (the current
directory's, or the one named) and runs it until the schema is built,
serving nothing.

```sh
vuka graphql schema                        # print it
vuka graphql schema -o schema.graphql      # write it
vuka graphql schema -check schema.graphql  # compare; exit 1 on a breaking change
```

See [The Schema Command](/web/graphql#schema-command).

## Explaining Code and Diagnostics {#explain}

`vuka explain` shows what Vuka makes of a file: each rewritten line beside the Go it becomes,
then the code added after the source (decorator wrappers, statics), each
labelled with the line it comes from. `-full` prints the whole generated file.
It transpiles the file's package and what that imports of the module (Go
packages too), as the build does, so an error elsewhere in the module
doesn't stop it.

Given a diagnostic's code, it prints that code's page of the
[catalogue](/errors/): what is wrong, why, and an example before and after
the fix. A build error names the code to look up:

```text
$ vuka build
error[E0203]: an attribute must start its own line
 --> pages/users.vuka:12:18
   |
12 | func helper() {} @doc("Lists the users")
   |                  ^^^^
   |
   = help: put the attribute on its own line, above the declaration

For more information about this error, try `vuka explain E0203`.
$ vuka explain E0203
```

### Diagnostic Codes

Every diagnostic of the compiler, `vuka vet`, the editor and the libraries'
checks at boot has a code: `E` for an error, `W` for a warning. On a terminal
the frame is coloured (`NO_COLOR` turns it off).

`vuka build`, `vet`, `test`,
`run` and `install` take `-diag=json` to print the diagnostics as a JSON
array instead, each with its code, severity, message, span (file relative to
the current directory, line, column), related spans, help, suggestion and the
URL of its page.

## Formatting Files {#fmt}

`vuka fmt` formats `.vuka` and `.vuka.html` files, as `gofmt` formats Go: it
prints the formatted file, or with `-w` writes it back, `-l` lists the files
whose formatting differs, and `-d` shows the diff. `-check` lists them and
fails when there are any, writing nothing — the flag for CI. `-rewrite`
writes a shape in the Go-header form in the `<script lang="go">` form before
formatting it.

`-imports` organizes each file's imports before formatting it, as
`goimports` does Go's — what [the editor does on save](/tools/editors#organize-imports):
imports nothing in the file uses go, a package the file uses without an
import comes in when only one package can be meant, and the imports are
grouped. `vuka fmt -l -imports` lists the files it would change, `vuka fmt
-w -imports ./...` changes them, and `vuka fmt -check -imports` fails CI on
any.

Paths are files or directories (all `.vuka` and `.vuka.html` files below,
skipping hidden directories, `node_modules`, `vendor` and `build/` modules vuka
made); no path means the current directory. A file that doesn't parse is
reported and left alone.

### Formatting Rules

- **Go** comes out exactly as gofmt would print it, alignment included. Vuka's
  own syntax is spaced the same way: `@attr(…)` arguments and static fields'
  types and values are gofmt-formatted, a static's `static name` lines up like a
  field name, `match` and its cases are indented like a `switch`, and an
  attribute always starts its own line, with the declaration below it.
- **Shape markup** is laid out the way Prettier lays out markup. An element that fits
  in 80 columns (a tab counts 4) stays on one line, unless its children were
  written across lines and hold an element; otherwise its children go one per
  line, a tab deeper than the line the element starts on. Attributes that
  don't fit go one per line, with `>` or `/>` on its own line under the `<`.
  `<br/>` becomes `<br />`; `<p></p>` stays as it is. `{for}`, `{if}` and
  `{match}` blocks follow the same rule, their bodies a tab deeper and `}}`
  under the `{`; a match always takes several lines, its cases under the `{`.
  Go expressions in `{…}` are gofmt-formatted (`{a+b}` becomes `{a + b}`);
  one holding a comment is left as written.
- **Text never changes what it renders.** Spaces that render (`<b>a</b> <i>b</i>`,
  `<p>  x  </p>`) are kept, so elements they join stay on one line; lines of
  text keep their breaks and are re-indented; entities are left as written.
  Each formatted file is checked against the original, and an internal error
  is reported rather than a page that would render differently.

This shows a file before and after formatting:

```vuka
// before
return <section className="pets" id="main" data-count={len(pets)} title="All our pets">
<h1>Pets ({len(pets)})</h1>
	<table>{for _,p:=range pets {<PetRow pet={p}/>}}</table>
		{if admin { <button disabled={len(pets)==0} >Add</button> } else { <i>read only</i> }}
</section>

// after
return <section
	className="pets"
	id="main"
	data-count={len(pets)}
	title="All our pets"
>
	<h1>Pets ({len(pets)})</h1>
	<table>{for _, p := range pets { <PetRow pet={p} /> }}</table>
	{if admin {
		<button disabled={len(pets) == 0}>Add</button>
	} else {
		<i>read only</i>
	}}
</section>
```

### Formatting Shape Files {#fmt-shapes}

A `.vuka.html` file is formatted whole, in the form it is in:

- **The Go** in its `<script lang="go">` (or its Go header) is formatted as a
  `.vuka` file, unindented.
- **The markup** is indented two spaces a level, by its elements and by the
  blocks of its `if`, `for` and `switch` statements. Statements and the Go
  in holes are gofmt-formatted (`{a+b}` becomes `{a + b}`). An opening tag
  that doesn't fit in the width goes one attribute a line, with `>` or `/>`
  on its own line under the `<`. Void elements and childless components
  close themselves: `<br>` becomes `<br />`, `<ui.Badge></ui.Badge>`
  becomes `<ui.Badge />`. Runs of blank lines become one.
- **Text stays on its lines.** Only whitespace that doesn't render changes:
  line breaks are re-indented, and what `<pre>`, `<textarea>`, a
  `<script>` and an inline `<style>` hold is left as written. Each file is
  checked against the original, and an internal error is reported rather
  than a page that would render differently.
- **A component's `<style>`**, after the markup, has its CSS formatted: one
  selector and one declaration a line, blocks indented two spaces.
- **Tailwind class order**, when turned on, sorts each static `class="…"`
  list the way Tailwind orders its rules: classes it doesn't know first, as
  written, then utilities by the property they set, then those under a
  variant (`hover:`, `md:`). A `class={…}` hole is never touched.

The project's `vuka.toml`, next to its `go.mod`, sets the width and the
Tailwind order in its `[fmt]` table. Both are optional:

```toml
[fmt]
width = 100      # the column a tag's attributes wrap at
tailwind = true  # sort class lists in Tailwind's order (off by default)
```

The editor's format-on-save and range formatting use the same rules and
the same file.

> [!NOTE]
> Tailwind ordering is off by default: it reorders every class list of a
> project the first time it runs, which is a change worth making on purpose.

### Converting Shapes {#shape-script}

`vuka fix shape-script` rewrites every `.vuka.html` file of the module in
the Go-header form in the `<script lang="go">` form, leaving its Go and
markup as written; a file it can't read as a shape is left alone and
reported by the compiler. `vuka fmt -rewrite -w` does the same and formats
the files.

### Processor Variables {#ctx-vars}

`vuka fix ctx-vars` rewrites, in the module's `.vuka` and `.vuka.html`
files, the reads `vuka vet` reports as [W0402](/errors/W0402):
`auth.Can(ctx, "x")` and `auth.Current(ctx).Can("x")` become
`perms.Has("x")`, `auth.Current(ctx)` becomes `user`, and
`web.Request(ctx).URL.Path`, `.URL.Query()` and `.PathValue("id")` become
`request.Path`, `request.Query` and `request.Params["id"]`. Imports of
`web` and `auth` left unused go. A read whose variable `main` doesn't
list, or whose name a local takes, is left alone.

## The Language Server {#lsp}

`vuka lsp` is the language server. See [Editors](/tools/editors).

## Checking Versions {#version}

`vuka version` prints the command's version and the set of vuka, ui and web it expects
(from v0.50.0 one version for all four; see
[Versions and compatibility](/project/versions)), and the Go that built it. Inside a
module it prints the Go that builds the module, warning when that is newer than vuka's
(reinstall vuka with it: `GOTOOLCHAIN=<that go> go install …`). Inside an app it also lists
the versions the app's go.mod requires, with any `replace`, and warns when
they aren't one set or aren't this command's set.

## Upgrading an App {#upgrade}

`vuka upgrade` moves an app to a set: the latest when no version is named (fetched with
`GOPROXY=direct`, so a fresh tag counts). It `go get`s vuka, ui and web at that
version — and, to this command's own set, the orm at the version `vuka new`
pins, when the app requires an older one — (never `go mod tidy`), then runs `vuka vet ./...` and
`vuka test ./...` (`-no-test` skips the tests), and prints a summary.

`-n`
(or `-dry-run`) prints the plan and changes nothing. go.mod and go.sum as they
were are kept in `.vuka/upgrade/`; a failed `go get` puts them back.

## Releasing {#release}

For maintainers: `vuka release` releases vuka, ui, web, cmd/vuka and cmd/vukavet under one
version. A dry
run unless `-yes`. See [Releasing](/project/releasing).
