# The vuka command

```text
vuka new <dir> [module path] [--ui=… --db=… --mail=… --auth=… --frontend=… --name=… --yes --force]
vuka add module <name>
vuka build|run|test|vet|install [go flags] [packages]
vuka run -watch [-debug] [go flags] [package] [args]
vuka mod tidy|why|vendor|graph|… [args]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka explain [-full] file.vuka
vuka fix [-n] [fixer…]
vuka fmt [-l] [-w] [-d] [paths…]
vuka lsp [-gopls path] [-log file] [-shared=false]
vuka version
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

## build, run, test, vet, install

The go command's, with every `.vuka` file in the module transpiled into an
overlay (`go … -overlay`): nothing is written among your sources. `vuka build`
also refreshes [`build/`](/guide/how-it-works#the-build-module). Flags and
packages are passed to go as they are.

**Declarer packages are linked automatically.** Every package of the module
with a [declarer](/features/decorators#declaration-decorators), a type
decorator or a composed decorator on a declaration — routes, crons, live
pages, commands — is blank-imported into each main package through a
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

`VUKA_FRONTEND=dir` names another directory; `VUKA_FRONTEND=off` skips it.

### run -watch

`vuka run -watch` (or `--watch`) keeps the program current while you edit:
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
  toolbar on); the build's state is `build.json` there.
- **Vite keeps running** across the restarts: a frontend's page keeps its
  state through a rebuild of the app.
- Ctrl-C or SIGTERM stops the app, the watcher, Tailwind and Vite, waiting for the
  app to finish; a second one kills an app that doesn't.

Without `-watch`, `vuka run` starts the program once, as `go run` does.
`vuka run -help` sums this up.

## mod

`go mod`, seeing the imports of `.vuka` files too. Plain `go mod tidy` reads
only `.go` files and would drop requirements only Vuka code uses; for the
command's duration, each package gets a small file of blank imports standing
for its `.vuka` files' imports (and the runtime, when the code uses it), removed
when it ends. It reads imports without transpiling, so it works while a
dependency is still missing.

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

## explain

Shows what Vuka makes of a file: each rewritten line beside the Go it becomes,
then the code added after the source (decorator wrappers, statics), each
labelled with the line it comes from. `-full` prints the whole generated file.

## fix

Repairs what tooling can. `-n` only reports; name fixers to run only those.

| Fixer | |
|---|---|
| `runtime` | upgrade the module's Vuka runtime when generated code needs a newer one |
| `static-names` | write statics as `User.Table` in `.vuka` files, not by their Go names |
| `orphans` | remove files `vuka gen -inplace` wrote for `.vuka` files that are gone |
| `attr-of` | rename `vuka.Attr[T](c)` to `vuka.AttrOf[T](c)`, in `.vuka` and `.go` files (v0.5.0) |
| `ui` | move UI code to [`github.com/vuka-lang/ui`](/ui/) (v0.10.0): `vuka.Node` → `ui.Node` and the other UI names, `vuka/live`, `vuka/term`, `vuka/templx` → `ui/…`, `vuka.TemplComponents` → `templx.Components`; import ui in `.vuka` files with JSX; `go get` ui |

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
