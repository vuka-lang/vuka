# The vuka command


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
| `vuka add page /reports` | a one-struct `.vuka.html` page under `pages/`; use `[id]` names for parameters and `_layout.vuka.html` for shared layout |
| `vuka add page /board --island Board` | a page shape under `pages/` that imports its frontend component; layouts use `_layout.vuka.html`, hooks use decorators |
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


## build, run, test, vet, install


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

`vuka vet` checks shape files, decorated hooks, field bindings, deferred
loaders and typed handler arguments as well as the generated Go. Missing
loaders, unfillable parameters and invalid markup are reported at source locations.
It runs [vukavet](/tools/vukavet)'s checks too, as go vet's `-vettool`: the
app's `go tool vukavet` when its `go.mod` declares it, else a `vukavet`
beside `vuka` or on `PATH`, else one it builds at its own version
([which](/tools/vukavet#a-vuka-app)); it says so when it can't.
`-vettool=…` overrides it, and `VUKA_VUKAVET=off` runs go vet's own
analyzers only.

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
without the generated-code header or line directives. What's
left is a plain Go module: `go build`, `go vet` and `go test` take it with no `vuka` command,
and it behaves as the `.vuka` version did. Eject the whole module (`./...`)
so the [link files](#build-run-test-vet-install) and the files embedding
CSS and assets are written too; they stay generated files.

Rendering remains authored in shape files. Page registrations are generated
from `pages/`, while services are explicitly registered on the app. Use
`vuka explain` to inspect lowering rather than copying generated rendering
or registration calls into application source.

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
pins, when the app requires an older one — (never `go mod tidy`), then runs `vuka vet ./...` and
`vuka test ./...` (`-no-test` skips the tests), and prints a summary. `-n`
(or `-dry-run`) prints the plan and changes nothing. go.mod and go.sum as they
were are kept in `.vuka/upgrade/`; a failed `go get` puts them back.

## release

For maintainers: releases vuka, ui, web, cmd/vuka and cmd/vukavet under one
version. A dry
run unless `-yes`. See [Releasing](/project/releasing).
