# The vuka command

::: go
A Go app doesn't need the `vuka` command: `go build`, `go run .` and `go test`
build it, and [`vukavet`](/tools/vukavet) runs the framework's checks under
`go vet`. The command is still useful beside them — `vuka new` and `vuka
add` scaffold, `vuka run -watch` rebuilds on save, `vuka upgrade` moves the
libraries — and `vuka eject` turns `.vuka` files into Go.
:::

```text
vuka new <dir> [module path] [--ui=… --db=… --mail=… --auth=… --frontend=… --name=… --yes --force]
vuka add module|service|component|island|page|entity … [--dry-run] [--force]
vuka build|run|test|vet|install [go flags] [packages]
vuka run -watch [-debug] [go flags] [package] [args]
vuka mod tidy|why|vendor|graph|… [args]
vuka build-islands [-watch]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka eject [-rm] [file.vuka… | dir | dir/...]
vuka explain [-full] file.vuka
vuka explain E0201
vuka fix [-n] [fixer…]
vuka fmt [-l] [-w] [-d] [paths…]
vuka lsp [-gopls path] [-log file] [-shared=false]
vuka version
vuka upgrade [-n] [-no-test] [version]
vuka release [-yes] [-no-test] [-security] vX.Y.Z   (maintainers)
```

## new

Starts an app you can grow: pages, sign-in, a database with migrations,
mail, styles and tests, wired together and working before you write a
line. [Getting started](/web/getting-started) walks through what it makes.

```sh
vuka new shop                       # asks five questions
vuka new shop example.com/shop      # a module path other than the directory's name
vuka new shop --db=postgres --frontend=react --yes
```

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

::: go
`vuka new` writes the app in `.vuka` files. For a Go app, run
`vuka eject -rm ./...` in it straight after: the same app, as a plain Go
module that `go run .` and `go test ./...` take (see [eject](#eject)).
:::

## add

`vuka add module <name>` grows an app `vuka new` made, a module at a time:

```sh
vuka add module invoices
```

writes `modules/invoices/` — an `Invoice` model, an `Invoices` service, a
page at `/invoices` linked in the navigation (or `/api/invoices` routes in
an API app), a test of the service — and `migrations/000N_invoices.go`
making its table, after the latest migration. It adds the module's import
to `main.vuka`, which is all a module needs to be part of the app: its
routes, pages and services declare themselves. The app applies the
migration when it next starts. The name is a plural noun in lower case;
an existing module, or an app without a database, is refused.

It is `add`, not a form of `new`: `new` makes a project, `add` grows the
one you are in, and a project directory named `module` stays possible.

The other kinds add one piece each:

| | |
|---|---|
| `vuka add service Mailer [--module notes]` | a service in `services/` (or `modules/notes/`; `notes/Mailer` is the same) with its constructor `NewMailer` and a test, registered in `main.vuka`'s `services` with `app.Service(notes.NewMailer)`: pages, routes and hooks take a `*notes.Mailer` parameter |
| `vuka add component StatCard [--module m]` | a stateless component, in `components/` or the module: the struct of its props in `stat_card.vuka`, its markup in `stat_card.vuka.html` |
| `vuka add island Chart [--module m] [--vue\|--react\|--svelte] [--dir d]` | `web/islands/Chart.vue` (`.tsx`, `.svelte`; `web/islands/<m>/` with `--module`), its props typed for an import, and `components/chart.vuka` with `chart.vuka.html` (or in `modules/<m>/`) — a `Chart` component placing it, so pages write `<components.Chart title="Sales" count={3} />`; the framework's packages and Vite plugin are added when missing and installed, showing it is under way (`--no-install` prints the command instead) |
| `vuka add page /reports [--module m] [--stateless]` | a page in `pages/`, `reports_page.vuka` and its markup `reports_page.vuka.html`: a live `@web.Live` type with an event (and an `@ui.Mount` hook), or with `--stateless` `@web.Get` on a handler; a path with `{parameters}` (`/reports/{id}`) makes the `@web.Get` handler, its parameters the path's, unless `--live` |
| `vuka add page /board --island Board [--module m] [--stateless] [--react] [--layout expr]` | a page backed by a component: an `@ui.Island` struct (bare: the component is where it looks) embedding `ui.Live` and routed with `@web.Live`, or with `--stateless` neither `ui.Live` nor `@web.Live` but `@web.Get`; its layout `@ui.Island(ui.Layout(expr))`, by default `layouts.Page("Board")` when the app has `layouts/`, none with `--layout none` — a prop, a field marked `@ui.Bind @ui.URL("q")` and an `@ui.OnChange` hook (a stateless page's `bind:"q"` `url:"q"`, its hooks in a `LiveSchema` method: `@ui.Mount` and `@ui.OnChange` are a live page's) — and the component reading and setting them with `useLive` (Svelte: `live()`): `v-model="live.q.value"`. `--module m` puts the component in `web/islands/m/` |
| `vuka add entity Comment --module posts body:text post:ref email:string:unique` | an orm model in `entities/` (or the module; `posts/Comment` is the same); `--unmanaged --table t` maps an existing table — no timestamps, test or migration, and no `migrations/` needed. Otherwise it comes with `ID`, `CreatedAt` and `UpdatedAt`, a test, and its migration written by `go tool orm makemigrations` |

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

Every kind refuses a name that isn't a Go identifier of the right case, a
declaration or file that exists (`--force` overwrites it), and a module that
doesn't; `--dry-run` lists what it would write and changes nothing. Files
are formatted as `vuka fmt` formats them, and a package nothing imports yet
is imported in `main.vuka`.

::: go
`vuka add` writes `.vuka` files too; `vuka eject -rm` on what it wrote
(`vuka eject -rm ./modules/invoices/...`) keeps them as Go.
:::

## build, run, test, vet, install

::: go
A Go app builds, runs and tests with the go command itself: `go build`,
`go run .`, `go test ./...`. What these commands add for `.vuka` files it
doesn't need; the rest has a Go route: `go vet -vettool=$(go tool -n vukavet) ./...`
for the [framework's checks](/tools/vukavet), `VUKA_DEV=1 go run .` with
`web.Dev()` among `web.New`'s options for Tailwind and Vite while you work,
[`go tool vuka build-islands`](#build-islands) for islands, and
[`go tool vuka gen -inplace ./...`](#gen) for the files embedding the
Tailwind CSS and the pages' styles and scripts in a production build.
:::

The go command's, with every `.vuka` file in the module transpiled into an
overlay (`go … -overlay`): nothing is written among your sources. `vuka build`
also refreshes [`build/`](/guide/how-it-works#the-build-module). Flags and
packages are passed to go as they are.

**Only what changed is transpiled again.** The Go each package became is
kept in the user's cache directory (`vuka/transpile`), keyed by the
package's sources, the API of the module's packages it imports, the go
command, the build environment, `go.mod`/`go.sum`/`go.work` and this vuka:
a build after a build transpiles the packages whose inputs changed, and a
change to a function's body leaves its importers alone. The island build is
skipped the same way when nothing in the frontend root (its sources,
config, lockfile, installed packages, `@source` paths) changed.
`VUKA_CACHE=off` turns both off. Under `vuka run` the files `web.Frontend`
serves are left out of the binary (empty in the overlay; it reads them from
disk in development), so they cost the linker nothing; `-embed` (with
`-watch`) or `VUKA_EMBED_STUB=off` builds them in.

`vuka vet` also reports what the go command can't see: a page backed by
a component that embeds `ui.Live` but is routed with `@web.Get` (a warning,
[W0301](/errors/W0301): it works; `@web.Live` says what it is), an island's
component file missing or ambiguous, and what the app would refuse at boot
of a page backed by a component when the source shows it already: a
deferred field whose group has no loader ([E0324](/errors/E0324)), a
loader of the wrong shape, a bad `bind:` or `defer:` tag, two handles of one
name. Those are errors: vet fails. The checks of
[vukavet](/tools/vukavet) — routes, handlers, live pages, templates, orm
schemas — run on the Go the app becomes with
`vuka vet -vettool=$(go tool -n vukavet) ./...`.

**Declarer packages are linked automatically.** Every package of the module
with a [declarer](/features/decorators#declaration-decorators), a type
decorator or a composed decorator on a declaration — routes, crons, live
pages, commands — or a struct type embedding a type marked with
`vukaDeclares()` — [ORM models](/orm/models) embedding `orm.Base` — is blank-imported into each main package through a
generated `vuka_link_gen.go` in the overlay, so its registrations exist in
the binary without a hand-written import, and its compile errors fail the
build like anyone else's. `vuka build` and `run` say what they link
(`● linking 2 declarer packages (…)`); with `run -watch`, a declarer package
created mid-session is linked by the next rebuild and a deleted one
unlinked. Every declarer package goes into every main — a blank import of a
package main already reaches is harmless, and a declarer package's init only
registers — except that a package under `examples/` is linked only into that
example's own main; test files, `testdata` and nested modules never count.
Opt out with a `//vuka:nolink` line above a package clause: on a declarer
package it is left for you to import (or not), and on a main package the
whole program curates its own imports.

In a module on [web](/web/) with views, the app's
[Tailwind CSS](/web/styling) comes along:

- `vuka run` starts `tailwindcss --watch` beside the program, writing
  `.vuka/tailwind/app.css` (a directory that ignores itself in git), and names
  that file to the app in `VUKA_TAILWIND_CSS`; open pages take each rebuild
  without reloading. A signal stops it with the program. Without the Tailwind
  CLI, it says how to install it and the pages go unstyled.
- `vuka build` and `vuka install` build the stylesheet once, minified, and
  embed it with a generated `vuka_tailwind_gen.go` in each main package that
  depends on web, through the overlay like the rest. Without the Tailwind CLI
  they fail, saying how to install it.

A page's own [styles and scripts](/web/assets) (`web.Style`, `web.Script`,
`web.CSS`, `web.JS`) come along too:

- `vuka run` scopes each component's stylesheets into
  `.vuka/tailwind/scoped/`, which the Tailwind build imports, and bundles its
  scripts (TypeScript or JavaScript, with esbuild, linked into the `vuka`
  command: no Node) into `.vuka/assets/`, which it names to the app in
  `VUKA_ASSETS`. It looks at the files every 200ms: a stylesheet saved
  restyles open pages without a reload, a script saved is what the
  component's next mount loads.
- `vuka build` and `vuka install` compile them with the stylesheet, the
  modules minified, and embed the modules with a generated
  `vuka_assets_gen.go` in each main package. A missing file, a CSS or
  TypeScript syntax error, a method two scripts of one component export and
  a `ctx.push` naming no method of its component (or not as a string
  literal) stop the build at their file and line; the names scripts push are
  the only methods the app lets the browser call by name; a selector matching nothing in its
  component's markup is a warning.

`VUKA_TAILWIND=off` skips Tailwind, as `web.NoTailwind()` in the source does;
the pages' own stylesheets are then served as they are, scoped.

A [frontend](/web/frontend) in `frontend/` (of the working directory, else of
the module root) with a `package.json` is a Vite project, and comes along
too:

- `vuka run` starts its dev server (`node_modules/.bin/vite`) beside the
  program, its output prefixed `vite:`; the app's pages load their modules
  from it. A signal stops it after the program.
- `vuka build` and `vuka install` run `vite build` before compiling, so the
  binary embeds the current build.
- Both first install the dependencies when Vite isn't installed, with the
  package manager of the lockfile, and copy web's Vite plugin to
  `frontend/vuka/vite.js` when `vite.config` imports it.

**Long steps say they are going.** A package install, the Vite island
build, a migration `vuka add entity` writes and `vuka new`'s module fetch
show one line on a terminal — a spinner and the seconds so far — that ends
in `✓ npm packages installed in web/` or `✗`. Off a terminal (CI, a pipe,
`vuka run`'s prefixed log) it is a line at the start, a `still installing`
line every ten seconds and a line at the end. The tool's own output is kept
rather than shown; when the step fails, its last 30 lines follow the error.

`VUKA_FRONTEND=dir` names another directory; `VUKA_FRONTEND=off` skips it.

### run -watch

`vuka run -watch` (or `--watch`; <span class="go-only">`go tool vuka run -watch .` in a Go app, where the command is a tool of the module</span>) keeps the program current while you edit:
save a file and, a moment later, the new version is the one answering. You
stay on one command for the whole session instead of stopping and restarting
it after each change.

```sh
vuka run -watch .
PORT=8094 vuka run -watch ./cmd/app -v   # flags after the package are the program's
vuka run -watch -debug .                 # keep debug information, for delve
```

- **The app keeps serving while the next build compiles.** vuka builds the new
  version to a binary first; only once it compiles does the running one get
  SIGTERM (SIGKILL after 2s) and the new one start, with the same arguments
  and environment. `● rebuilt in 1.2s` counts from the save being seen to the
  new process starting; the app is down only for the swap itself.
- **A rebuild does only what the change needs.** A save is seen as it
  happens: vuka listens to the system's file events (kqueue on macOS and the
  BSDs, inotify on Linux), watches new directories as they appear, and starts
  the build once the files have been calm for 30ms, so an editor's save
  through a temporary file, or a formatter's rewrite, is one build. Only the
  packages whose sources changed are transpiled again, plus those importing a
  package whose API changed; the module's own packages are type-checked in
  memory rather than compiled for their export data. A save while a build
  runs stops that build, stale already, and starts the next one.
- **Builds link without debug information** (`-ldflags=-w`), which makes the
  link faster and the binary smaller to start. `-debug` keeps it, for delve or
  a debugger attaching to the app; other `-ldflags` you pass are kept, with
  `-w` added in front.
- **A build that fails changes nothing.** The errors print as `vuka build`
  prints them, with the `.vuka` file and line, and the last good build keeps
  running until the code compiles again.
- **A change that leaves the program as it was restarts nothing**
  (`● unchanged`): a comment, a blank line at the end, a file the program
  doesn't build from. The app keeps its in-memory state.
- **What starts a build:** a change to a `.vuka`, `.templ` or `.go` file (not
  `_test` files), `go.mod`, `go.sum`, or a file the app embeds, such as an
  `.html` view. Hidden files and directories, `build/`, `.vuka/`,
  `node_modules`, `testdata`, `vendor` and nested modules are left alone.
  Saves that come together, like a formatter rewriting several files, make one
  build. Where the system has no file events (or with `VUKA_WATCH=poll`, for a
  file system that doesn't deliver them), vuka looks at the sources every
  200ms instead.
- **Tailwind keeps running** across the restarts, and restyles open pages as it
  does under `vuka run`; so do the pages' [styles and scripts](/web/assets),
  their declarations read again with each build.
- **Open [live pages](/web/live) reconnect** to the new build. A development
  app (`web.Dev()` or `VUKA_ENV=development`) rejoins them as the new build
  renders them; otherwise each page reloads.
- **The [development toolbar](/web/toolbar) follows the build.** On every
  page of a web app, its handle says `building… 1.2s` while the next build
  compiles and `build failed` when it didn't, with the compiler's output in
  its drawer: you see that the page in front of you is the last good build
  without looking at the terminal. vuka passes the app its session
  directory in `VUKA_DEV_DIR` (under plain `vuka run` too, which turns the
  toolbar on); the build's state is `build.json` there. A failed build of
  the styles, scripts or islands shows as `island build failed` with the
  build's error (Vite's, for a Vite frontend), while the last good bundle
  keeps serving; an island it doesn't have shows a box saying why.
- **Vite keeps running** across the restarts: a frontend's page keeps its
  state through a rebuild of the app.
- Ctrl-C or SIGTERM stops the app, the watcher, Tailwind and Vite, waiting for the
  app to finish; a second one kills an app that doesn't.

Without `-watch`, `vuka run` starts the program once, as `go run` does.
`vuka run -help` sums this up.

## mod

<span class="go-only">A Go app needs plain `go mod tidy`.</span> `go mod`, seeing the imports of `.vuka` files too. Plain `go mod tidy` reads
only `.go` files and would drop requirements only Vuka code uses; for the
command's duration, each package gets a small file of blank imports standing
for its `.vuka` files' imports (and the runtime, when the code uses it), removed
when it ends. It reads imports without transpiling, so it works while a
dependency is still missing.

## build-islands

Builds the module's [islands](/web/islands) from its source alone, for an
app built with plain `go build`: the types `ui.RegisterIsland[T](…)`, a
route's `ui.Island(…)` option or `@ui.Island` registers, read without
running the app. The bundle and its stylesheets go into `.vuka/` of the
frontend's build — the root `web.Frontend` serves — where `//go:embed`
picks them up and the app serves them from. One line in the main package:

```go
//go:generate go tool vuka build-islands
```

beside the frontend's `//go:embed all:web/dist` of `var dist embed.FS`,
which the app serves:

<!-- go:context
var dist embed.FS
-->

```go
func main() {
	app := web.New(web.Frontend(dist, "web/dist"), web.Dev())
	log.Fatal(app.Run(""))
}
```

Run it after the frontend's own build (`vite build` empties `dist`), then
`go build`. A build of islands alone, with no `index.html`, is served too.
It writes `islands.d.ts` and `vuka-live.d.ts`, the components' types,
in the frontend's folder. A component it can't find is
[E0310](/errors/E0310); an app with no `web.Frontend` is
[E0609](/errors/E0609).

`-watch` keeps the islands built in `.vuka/assets` until its input closes:
[`web.Dev()`](/web/frontend) runs it under `VUKA_DEV=1 go run .` when the
app registers islands and the command is a tool of the module
(`go get -tool github.com/vuka-lang/vuka/cmd/vuka`) or on `PATH`.

## gen

Writes `build/`, a plain Go module (`-o` picks another directory). With
`-inplace`, writes each generated file beside its `.vuka` file instead — for
[libraries](/guide/go-interop#publishing-a-library). `-check` writes nothing and
fails when the output is out of date. Both include the files embedding the
app's [Tailwind CSS](/web/styling) and its pages'
[styles and scripts](/web/assets), and each main package's `vuka_link_gen.go`
of [auto-linked declarer packages](#build-run-test-vet-install), so plain
`go build` serves them too. The link files need the whole module's packages,
so `-inplace` writes them only for a module-wide gen (the default `./...`),
and once the last declarer package is gone the file is written empty rather
than left with stale imports.

The generated Go reads as hand-written: gofmt'd, one import block, Vuka's own
names spelled like a person's (`boardType`, not `__Board_type`), doc comments
on the declarations they document, and a `//line main.vuka:12` directive only
where the code stops following the `.vuka` file line by line, so compiler
errors, stack traces and debuggers point at the `.vuka` source. Files
written to disk name it relative to themselves, never by absolute path.

## eject

```sh
vuka eject                 # the module: gen -inplace
vuka eject pages/orders.vuka
vuka eject -rm ./...       # the Go becomes the source
```

Writes the Go generated for `.vuka` files beside them, as `gen -inplace`
does: for the files named, or the packages (`dir`, `dir/...`; the module by
default). With `-rm` the Go becomes the source: each `.vuka` file is deleted
and its Go kept as `orders.go` (`orders_vuka.go` when `orders.go` is taken),
without the generated-code header or line directives, its earlier forms
rewritten first as the `attributes`, `queries` and `db-schema` [fixers](#fix)
do (a model's `orm:` tags become its `Schema` method's attributes). What's
left is a plain Go module: `go build`, `go vet` and `go test` take it with no `vuka` command,
and it behaves as the `.vuka` version did. Eject the whole module (`./...`)
so the [link files](#build-run-test-vet-install) and the files embedding
CSS and assets are written too; they stay generated files.

::: vuka
```vuka
// pets.vuka
@web.Controller("/admin/pets")
type PetAdmin struct{ store *PetStore }

@web.Get("/pets/{id}")
func ShowPet(id int, store *PetStore) Result[Pet] { return store.Find(id) }
```
:::

<!-- go:context
type DB struct{ *orm.DB }
type Pet struct{ ID int }

type PetStore struct{ db *DB }

func (s *PetStore) Find(id int) vuka.Result[Pet] { return vuka.Ok(Pet{ID: id}) }
-->

::: go
```go
// pets.go, after vuka eject -rm
type PetAdmin struct{ store *PetStore }

func ShowPet(id int, store *PetStore) vuka.Result[Pet] { return store.Find(id) }

func NewPetAdmin(store *PetStore) *PetAdmin {
	return &PetAdmin{store: store}
}

func init() {
	web.Routes.Controller("/admin/pets", NewPetAdmin)
	vuka.RegisterParams(ShowPet, "id", "store")
	web.Routes.Get("/pets/{id}", ShowPet)
}
```
:::

Decorators become the calls they stand for. A route, page or controller
decorator is a call of its package's [registry](/features/decorators#registries)
in an `init` — `@web.Get("/pets/{id}")` on `ShowPet` is
`web.Routes.Get("/pets/{id}", ShowPet)`, after `vuka.RegisterParams`
records the parameters' names, the calls a Go app makes on its `App` — a
type's constructor is Go's `NewX` (taking its pointer and interface fields), a
type decorator is called with `vuka.TypeFor[X](NewX)` and a declarer with
`vuka.DeclOf(fn)`.

## explain

Shows what Vuka makes of a file: each rewritten line beside the Go it becomes,
then the code added after the source (decorator wrappers, statics), each
labelled with the line it comes from. `-full` prints the whole generated file.

Given a diagnostic's code, it prints that code's page of the
[catalogue](/errors/): what is wrong, why, and an example before and after
the fix.

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

Every diagnostic of the compiler, `vuka vet`, the editor and the libraries'
checks at boot has a code: `E` for an error, `W` for a warning. On a terminal
the frame is coloured (`NO_COLOR` turns it off). `vuka build`, `vet`, `test`,
`run` and `install` take `-diag=json` to print the diagnostics as a JSON
array instead, each with its code, severity, message, span (file relative to
the current directory, line, column), related spans, help, suggestion and the
URL of its page.

## fix

Repairs what tooling can. `-n` only reports; name fixers to run only those.

| Fixer | |
|---|---|
| `runtime` | upgrade the module's Vuka runtime when generated code needs a newer one |
| `static-names` | write statics as `User.Table` in `.vuka` files, not by their Go names |
| `orphans` | remove files `vuka gen -inplace` wrote for `.vuka` files that are gone |
| `attr-of` | rename `vuka.Attr[T](c)` to `vuka.AttrOf[T](c)`, in `.vuka` and `.go` files (v0.5.0) |
| `ui` | move UI code to [`github.com/vuka-lang/ui`](/ui/) (v0.10.0): `vuka.Node` → `ui.Node` and the other UI names, `vuka/live`, `vuka/term`, `vuka/templx` → `ui/…`, `vuka.TemplComponents` → `templx.Components`; import ui in `.vuka` files with JSX; `go get` ui |
| `attributes` | write a live page's field tags (`bind:`, `url:`, `defer:`, `validate:`) and a model's `orm:` tags as attributes: `orm:"elements:tag_ids"` → `@orm.Elements{Column: "tag_ids"}`, `orm:"column:x;unique"` → `@orm.Column("x") @orm.Unique`; what has no attribute stays in the tag |
| `db-schema` | rename `orm.Schema` values to `orm.DBSchema` (`orm.Of[T](orm.Schema{…})` → `orm.Of[T](orm.DBSchema{…})`), in `.vuka` and `.go` files, once the module's orm names it so; `*orm.Schema`, a schema method's, stays |
| `queries` | write queries made for `orm.Query`, before `Post.Objects` was the model's manager, as the manager's: `vuka.Or(…)` of a model's fields → `orm.Or(…)`, `fn.Lower(Post.Title)` → `orm.Lower(Post.Title)`, `fn.In(a, q, c)` → `a.InQuery(q.Values[any](c.Name()))`, `x := q.First(ctx)?` read as an `Option` → `orm.Optional(q.First(ctx))?`, `Post.Objects.Manager()` → `Post.Objects` |
| `ui-structs` | write the [ui kit](/ui/components#kit)'s components as struct literals, once the module's ui declares them so: `ui.Card(ui.CardProps{…})` → `ui.Card{…}`, `ui.Table(ui.TableProps[T]{…})` → `ui.Table[T]{…}`, in `.vuka` and `.go` files; `ui.Button(p)` compiles as it is (a conversion), and a generic one given a value, `ui.Table(t)`, is left for its type argument |

## fmt

Formats `.vuka` files, as `gofmt` formats Go: it prints the formatted file,
or with `-w` writes it back, `-l` lists the files whose formatting differs, and
`-d` shows the diff. Paths are files or directories (all `.vuka` files below,
skipping hidden directories, `node_modules`, `vendor` and `build/` modules vuka
made); no path means the current directory. A file that doesn't parse is
reported and left alone.

- **Go** comes out exactly as gofmt would print it, alignment included. Vuka's
  own syntax is spaced the same way: `@attr(…)` arguments and static fields'
  types and values are gofmt-formatted, a static's `static name` lines up like a
  field name, `match` and its cases are indented like a `switch`, and an
  attribute always starts its own line, with the declaration below it.
- **JSX** is laid out the way Prettier lays out markup. An element that fits
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

::: vuka
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
:::

::: go
Go files are `gofmt`'s; `vuka fmt` formats `.vuka` files only, and
`.vuka.html` templates keep the layout they are written with.
:::

## lsp

The language server. See [Editors](/tools/editors).

## version

Prints the command's version and the set of vuka, ui and web it expects
(from v0.50.0 one version for all four; see
[Versions and compatibility](/project/versions)). Inside an app it also lists
the versions the app's go.mod requires, with any `replace`, and warns when
they aren't one set or aren't this command's set.

## upgrade

Moves an app to a set: the latest when no version is named (fetched with
`GOPROXY=direct`, so a fresh tag counts). It `go get`s vuka, ui and web at that
version — and, to this command's own set, the orm at the version `vuka new`
pins, when the app requires an older one — (never `go mod tidy`), runs the `vuka fix` rewrites registered for the
releases between the app's version and the target, then `vuka vet ./...` and
`vuka test ./...` (`-no-test` skips the tests), and prints a summary. `-n`
(or `-dry-run`) prints the plan and changes nothing. go.mod and go.sum as they
were are kept in `.vuka/upgrade/`; a failed `go get` puts them back.

## release

For maintainers: releases vuka, ui, web, cmd/vuka and cmd/vukavet under one
version. A dry
run unless `-yes`. See [Releasing](/project/releasing).
