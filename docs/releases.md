# Releases

From v0.50.0, vuka, ui, web and the `vuka` command (and from v1.0.0
`vukavet`) are released together under one version: a release is a
**set**, and `vuka v1.0.0` means all of them at v1.0.0. The set keeps
[the compatibility promise](/project/compatibility), which from v2.0.0
covers the `/v2` modules. An app requires vuka, ui and web at the same version; `vuka
version` shows the set and warns when an app's go.mod mixes sets, and `vuka
upgrade` moves an app from one set to another, running the `vuka fix`
rewrites in between. Patch releases come weekly, security fixes at once,
features in minor releases with upgrade notes. See
[Versions and compatibility](/project/versions).

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
vuka upgrade
```

Releases before v0.50.0 were tagged module by module; their headings name each
module's version, and each states the **runtime** its generated code needs —
the `github.com/vuka-lang/vuka` package your module requires.

## v2.6.1 — unreleased

- **`vuka lsp` answers every request in time while gopls is slow.** A
  completion in markup asks gopls about a scratch copy of the generated Go,
  and held the generated Go for its whole question; while typing, those
  questions queued, and the build of the edit queued behind them for as
  long as a busy gopls (a shared daemon serving another workspace) took to
  answer them. Each request on the edited file then waited for that build
  on the server's main loop, one after another, so a hover stayed on
  "Loading…" and a save's formatting ran on until the file was closed. A
  build now goes first (a completion's question to gopls gives way and the
  completion goes without it), a request waits for the build of an edit
  only once, a cancelled request is answered at once, and a request nothing
  has answered by the deadline is answered with nothing to show (formatting
  with no edits) rather than left waiting; an answer coming after that is
  dropped (#149).

## v2.6.0 — 2026-10-26

- **Commands pushed from a hook reach their handlers.** A Push step in a
  command a component pushed with `PushJS` — from its `@ui.Mount` hook, an
  event or an Update method — took the next id in the handler list of the
  component's last render, and the component's next render numbered that
  list afresh. A render between the push and the event it sends lost the
  handler: the join adopting the session the page's HTTP render mounted
  renders every component again, and so does another component's event or
  a message. A data table whose mount hook pushed
  `ui.JS.Push(d.startCount)` to start an async count got
  `live: no handler "c3:75"` on every page load, and the count never
  started until something else ran it. Pushed commands' handlers now have
  ids of their own (`c3:0p`, `c3:1p`, …), kept per component across its
  renders for its last 64 pushes, so they work whenever the browser sends
  them, generic components included, and never name another handler. An
  event naming no handler says whose it would have been and why it's gone:
  `live: no handler "c3:75": views.Table[…] (c3): its last render has 75`,
  or that the component was unmounted.

- **Live errors keep their detail out of the browser in production.** Since
  the change above, that reason — Go type names with their packages,
  instance ids and handler counts — reached every visitor's browser. Outside
  development the reply is now the short form, `live: no handler "c3:75"`,
  and the full reason goes to the log with the session, the page, the
  handler and the component. The same holds for an event payload that
  doesn't parse (`live: click event payload doesn't parse`, not the JSON
  decoder's message naming Go types), a value whose `UnmarshalText` fails,
  and a handler taking a type a field can't bind. In development
  (`vuka run`, `web.Dev()`, `VUKA_ENV=development`) the browser's console and
  the toolbar get the full text, as before. ui's no-handler error is a
  `*live.NoHandlerError` (`Target`, `Component`) whose `PublicMessage` is the
  short form.

- **`vuka run -watch` starts at once when nothing changed.** Each start
  transpiled the module and ran go build and the linker before the app
  answered, a few seconds on a mid-size app (more on a cold cache or a busy
  disk) even when nothing had changed since the last run. A good build's
  binary is now kept in vuka's cache under a key hashing everything the
  build reads (the sources, `go.mod`/`go.sum`/`go.work`, modules replaced
  by directory, embedded files, flags, the go command's environment, the
  module's Go and vuka), and a start whose sources match a kept build runs
  it before transpiling, printing `● reusing the last build (no changes)`.
  The build runs all the same in the background: one that comes out
  different is swapped in, with a warning naming the side whose inputs the
  key missed. The last three builds of a module are kept, so a branch
  switched back and forth hits too. On an app of 13 transpiled packages
  the app ran about 0.1 s after `vuka run -watch` started and answered in
  0.6–1.0 s (half a second of it the app's own start) instead of 4.2–6.7 s;
  on a busy machine it answered in 1.0–1.8 s instead of 9–11 s. A start with
  changes costs what it did. `-fresh` or
  `VUKA_REUSE=off` builds first; `vuka build` always builds
  ([#118](https://github.com/vuka-lang/vuka/issues/118)).
- **A module built by a newer Go than vuka's is an error, not a panic.**
  vuka reads the export data of a module's imports with the importer of
  the Go that built it, and the go command writes that data in the format
  of the Go that builds the module, which its go.mod's `go` or `toolchain`
  line (or `GOTOOLCHAIN`) may make newer. Go changes the format even in
  point releases (go1.27.2 writes data go1.27.1 can't read), and vuka
  panicked reading it. Now `vuka build`, `run`, `run -watch`, `test` and
  `vet` stop before transpiling when the module's Go is newer than vuka's,
  `vuka lsp` shows it as a diagnostic, and either names both Gos and the
  command that reinstalls vuka (and vukavet) with the module's:
  `GOTOOLCHAIN=go1.27.2 go install github.com/vuka-lang/vuka/cmd/vuka/v2@…`.
  `vuka version` prints the Go that built vuka and, inside a module, the
  Go that builds it, with the same warning when that is newer
  ([#114](https://github.com/vuka-lang/vuka/issues/114)).
- **`vuka run -watch` runs on Windows.** It built the app to a file without
  `.exe`, which Windows doesn't run, so every build failed to start with
  `executable file not found in %PATH%` and nothing served. Every binary
  vuka builds and runs now gets the system's suffix. On Windows the app is
  asked to stop with a `CTRL_BREAK_EVENT` (`os.Interrupt` in Go), as
  Windows has no SIGTERM, in a process group of its own, so a swap stops
  it alone; it is killed with what it started when it doesn't stop within
  two seconds. Its look for changes asks each file for its size and time,
  as Windows' directory listing can still show a file as it was before a
  save, which left the save unseen. Vite, Tailwind and typescript-language-server installed
  with npm run through their `.cmd` shims, the standalone Tailwind CLI is
  found on PATH under its released name (`tailwindcss-windows-x64.exe`),
  the hint for installing Tailwind fits the system (`brew` on macOS only),
  and the language server's file URIs carry drive letters
  (`file:///C:/…`). `vuka routes` prints its files' paths with slashes on
  every system. CI now builds and runs a new app on Windows
  ([#115](https://github.com/vuka-lang/vuka/issues/115)).
- **The language server no longer keeps gopls busy for minutes.** After a
  build it opened every generated Go file in gopls at once, and gopls
  loads each file it doesn't know with a `go list` of its own, writing
  every open buffer to disk first: on a module of a few hundred files
  that kept gopls at several cores for minutes, and the editor's own
  file waited behind it. The generated files now reach gopls a package
  at a time, in dependency order, each package once gopls has loaded the
  one before, and the packages of the files the editor shows go first.
  On a module of 46 packages and 489 files gopls used about 3 CPU-minutes
  instead of 19 and answered in 14 seconds instead of 5 minutes. A build
  that changes nothing sends gopls nothing, and an edit sends the files it
  changed ([#108](https://github.com/vuka-lang/vuka/issues/108)).
- **Builds transpile only what changed while an import doesn't
  resolve.** An import nothing provides — one being typed — made every
  later build of `vuka lsp` and `vuka run -watch` transpile the whole
  module again. Only the packages whose inputs changed are transpiled now,
  and an edit undone reuses what was transpiled before it
  ([#109](https://github.com/vuka-lang/vuka/issues/109)).
- **Cold builds read the module once.** A build on an empty cache read
  each of the module's directories again from every step that looked at it
  (the `build/` mirror once for every file in it), and started a `go list
  -export -deps` for each import that type-checking reached and the first
  `go list` hadn't covered: those of the module's plain Go packages, the
  runtime, ui's state package and component files named by relative path.
  Each `go list` reads every package the import depends on. On a busy disk
  that made a first build take minutes. A build now reads each directory
  once, one `go list` covers every import, a relative path is never asked
  of the go command, and the orm tool's binary is found once a process
  instead of `go tool orm` (or `go run`) being run for each package of
  models. `VUKA_CACHE=<dir>` puts vuka's caches in that directory, and
  `vuka run -watch` names a step of a rebuild that takes more than three
  seconds ([#104](https://github.com/vuka-lang/vuka/issues/104)).
- **A test file no longer hides a processor variable.** A `_test.go`
  declaration of a folder-page package named like a context processor's
  variable (`func page(…)`, `request`, `user`) made the package's
  templates read it instead of the variable, so the package stopped
  compiling — `undefined: page` — once the test file was there. The
  compiler now decides what a name means from the package's non-test
  files: a template reads the variable, in the test build too, and a test
  file may declare the name ([#101](https://github.com/vuka-lang/vuka/issues/101)).
- **`vuka test ./...` and `vuka vet ./...` in route folders.** A vuka
  command tidying the Go-name directories of route groups and `[id]`
  folders (`group_admin`, `param_id`) removed the ones a running `vuka
  test` or `vuka vet` had made — the editor's language server does on
  every build — so the go command failed with `chdir …/group_admin: no such
  file or directory`. Each run now marks the directories it uses, and
  they stay until no running vuka has them marked. A route folder's own
  path is a package pattern too: `vuka test './pages/(admin)'`
  ([#102](https://github.com/vuka-lang/vuka/issues/102)).
- **`vuka fix ctx-vars` finds what `vuka vet` reports.** In a folder page
  the fixer reported "nothing to fix" where vet reported W0402: it
  transpiled the module without the folder routes' stubs, so a page
  package wasn't linked to the main listing the processors, and no
  warning — so no rewrite — was made. It now transpiles as vet does, and
  names a file it skips because it doesn't compile
  ([#103](https://github.com/vuka-lang/vuka/issues/103)).
- **The language server always answers.** Semantic tokens, hover,
  completion and definition on a `.vuka` or `.vuka.html` file no longer
  wait forever for a busy gopls, such as a shared daemon still loading
  another workspace. After 15 seconds the editor gets what vuka knows
  without gopls, such as the markup's own tokens, and gopls is told to
  cancel. The editor's `$/cancelRequest` now reaches gopls for these
  requests too. A request sent while a build runs gets the same answer at
  the deadline instead of waiting for the build. Writes to gopls no longer
  wait for gopls to read them, so a gopls that stops reading doesn't stop
  `vuka lsp` answering the editor ([#100](https://github.com/vuka-lang/vuka/issues/100)).
- **The toolbar no longer says “building…” after a build is done.**
  `vuka run -watch` wrote the build's state, which the development
  toolbar reads, after printing how the build went, and wrote a good
  build's state after starting its app, so the app's first look could find
  the last state. On Windows the state wasn't written at all when the app
  (or a virus scanner) had the file open as vuka replaced it, so the
  toolbar could stay on “building…” after the build had failed or
  finished. The state is now written before the message and before the
  app starts, and on Windows the replacement is tried again until the file
  is let go ([#93](https://github.com/vuka-lang/vuka/issues/93)).
- **An opened file's type errors no longer go missing.** gopls's
  diagnostics of a file's Go that arrived while the editor's text of the
  file was newer than the last build were dropped, and gopls doesn't send
  the same diagnostics twice: when the build of the newer text left the
  Go as it was (opening a file, or an edit undone), the file showed no
  errors until the next edit that changed its Go. They are now kept and
  shown once that build is done
  ([#93](https://github.com/vuka-lang/vuka/issues/93)).
- **Routes in a page package's Go.** When `vuka lsp` serves `.go` files
  (as VS Code's gopls through `go.alternateTools.gopls`) and `.vuka` files
  of a folder-route package, a hover on a page's or an endpoint's type name
  adds below gopls's hover the routes it serves (`POST /auth/logout` ·
  endpoint `logout.Index.Post`) with its guards, layouts and middleware; on
  a handler's or an endpoint's method (`Post`, `Get`, `Delete`…) the route
  it serves; on a live page's event method the page it is an event of. The
  route code lens is above handler and endpoint methods, and an endpoint's
  type gets one with all its methods (`GET, POST /api/orgs · endpoint`).
  Paths are the ones `vuka routes` prints: `[id]` folders as `{id}`, route
  groups left out. A `.go` file outside the pages keeps gopls's answers
  as they are ([#123](https://github.com/vuka-lang/vuka/issues/123)).
- **`web.LiveCompression` sets how live sockets compress.** Live sockets
  already negotiated permessage-deflate without context takeover, so
  browsers got compressed messages. That behaviour is now an option,
  documented with its numbers: `web.NoContextTakeover` (the default),
  `web.ContextTakeover` and `web.Off`. Messages under 256 bytes are no
  longer compressed (the threshold was 512). On a session of a 200-row live
  list page, 3.3 MB of messages took 245 KB on the wire. Context takeover
  saved 2% more but held about 800 KB per open socket (8 GB at
  `web.LiveMaxConns`' 10,000, against 2 KB without it), so the default
  stays. The framework's Go test clients now negotiate compression as
  browsers do ([#127](https://github.com/vuka-lang/vuka/issues/127)).
- **Responses are compressed, and static files revalidate.** Pages,
  JSON and assets went out uncompressed, and `app.Static` files with no
  `ETag`, `Last-Modified` or `Cache-Control`, so browsers fetched them again
  on every page. Responses now go out in brotli, else gzip, when the browser
  takes it, with `Vary: Accept-Encoding`: a 200-row list page of a
  `vuka new` app takes 4.6 KB on the wire instead of 268 KB (7.7 KB in
  gzip), `/_vuka/live.js` 23 KB instead of 79 KB, `app.css` 4.5 KB instead
  of 25 KB. What handlers write is compressed as it goes out (brotli quality
  5, about a millisecond on that page), a flush sending what was written so
  far; bodies under 1 KB, images and other compressed types, `Range`, `HEAD`,
  `204`/`206`/`304`, a handler's own encoding, `no-transform`, event streams
  and WebSocket upgrades are sent as they are. `vuka build` embeds the
  stylesheet's, the components' scripts' and the island chunks' brotli and
  gzip (`web.RegisterEncoded`), the Vite plugin and `vuka build-islands`
  write a `.br` and a `.gz` beside a frontend's files, and each encoding has
  its own `ETag`. `app.Static` files get a content `ETag`, `Last-Modified`
  when the file system has times, a `304` on revalidation, and
  `public, max-age=0, must-revalidate` (cached for good when the name
  carries a hash); the `.br` or `.gz` beside a file is sent when there is
  one. Island chunks and modules under `vuka run` now have an `ETag` too,
  so a reload revalidates them instead of downloading them again.
  `web.Compression(level)` sets how hard, `web.Compression(web.Off)` turns
  it off behind a proxy that compresses; it is on under `vuka run` as well,
  and the toolbar's Request panel shows the bytes sent
  ([#126](https://github.com/vuka-lang/vuka/issues/126)).
- **A page's script is a component script.** `component<T>` works on a
  page shape with the same `ctx` as a component's: `ctx.live`,
  `ctx.state`, `ctx.ref`, `ctx.on` (the page's `PushEvent`), `navigate` and
  `patch`, and `update(next)` when a patch that keeps the page mounted
  changes its props. `page<T>((el, live, ctx) => …)` stays as its alias,
  `ctx` its new third argument; two-argument scripts keep working. Fixes
  found on the way: a script calling `ctx.state` mounted on the wrong
  element, as its states were written over the root's scope (they are
  `data-vk-st` now); a live shape's script couldn't push its methods
  (`live: … takes no push named Reorder`); a shape whose struct is in its
  `<script lang="go">` got no props or methods in the script's types; and
  `ctx.state(…).watch` missed changes when the script mounted before the
  live runtime.
- **TypeScript in shape scripts works out of the box in VS Code.** The
  extension bundles `typescript-language-server` and runs it with VS
  Code's Node and the TypeScript inside VS Code (the module's own
  `typescript` wins), on macOS, Linux and Windows. `vuka lsp` takes it as
  `-tsls`, `-tsls-node` and `-tsserver` (`VUKA_TSLS`, `VUKA_TSLS_NODE`,
  `VUKA_TSSERVER`), so other editors can point at one too.
- **The names a script passes as strings are typed.** `ctx.ref` completes
  the markup's `ref="…"` names, each its element's DOM type; `ctx.on` the
  events the Go's `PushEvent` calls send, with their payloads' types;
  `ctx.emit` the `on…` attributes of a component's root (a type error on a
  page, which has no parent); `navigate` the app's folder routes; and
  `patch({ q: "x" })`, a new form of `patch`, a page's `@ui.URL` keys. A
  live method's hover shows its Go doc, go to definition on
  `ctx.live.reorder` opens `func (p *Index) Reorder`, and the method's
  references list the scripts calling it. In a script, `listen` completes
  an event listener removed on `destroy`, and an import's specifier
  `vuka:component` and the module's npm packages. `vuka vet` reports a ref,
  state or event a shape lacks, and `ctx.emit` in a page
  ([W0308](/errors/W0308)). A guide, [Component Scripts](/ui/scripts),
  covers when to write a script, the whole `ctx` and worked examples
  ([#138](https://github.com/vuka-lang/vuka/issues/138)).

- **A template with a script or a scoped stylesheet can fill a
  component's slots.** Either makes the template a scoped component, and
  every element its markup writes got the scope's `data-vk-s` — the
  `<slot:…>` elements too, which render nothing. A page filling a table's
  repeated slot failed to build with
  `error[E0106]: <slot:column> has no attribute data-vk-s; its fields are Key, Label, Cell`
  (a single slot: `<slot:caption> takes no attributes`). Slots are no
  longer marked; the elements of their content are, with the scope of the
  template that wrote them. And a script's props went on the markup's first
  tag even when it was a component's: `<components.Table rows={rows}>`
  failed with `<components.Table> has no attribute data-vk-p`, and
  `<components.Table[Row] rows={rows}>` with
  `error[E0101]: unexpected '[' in <components.Table>`. When the markup
  starts with a component's tag or a slot, a
  `<vk-c style="display:contents">` around it carries the props and the
  scope: the root the script mounts on, once
  ([#135](https://github.com/vuka-lang/vuka/issues/135)).

- **Island imports can start at the module root.**
  `import Form "@/web/islands/Form.vue"` names the file from the directory
  holding `go.mod`, so a page deep in `pages/(admin)/users/[id]/` needs no
  `../../../..`, and moving it doesn't break the import. Relative paths
  work as before, and both spellings of one file are the same island.
  A missing file is E0303 with the written path and the file it resolved
  to; a path that leaves the module root (`@/../x.vue`) is refused
  ([#111](https://github.com/vuka-lang/vuka/issues/111)).

## v2.5.0 — 2026-10-24

- **Hooks on generic shapes.** `@ui.Mount`, `@ui.OnChange` and
  `@ui.Loads("group")` on a method of a generic component compile and run
  per instantiation: they go into its `LiveSchema`, now written as a method
  of the generic type (`func (t *Table[T]) LiveSchema(…)`), so `Table[Task]`
  and `Table[User]` on one page each mount. A hook attribute on a method of
  a type that isn't live is [E0130](/errors/E0130) instead of being lowered
  as a call of the islands' `ui.Mount(props)`
  ([#87](https://github.com/vuka-lang/vuka/issues/87)).
- **Field attributes on generic components.** `@ui.URL` (with `.Default`),
  `@ui.Bind`, `@ui.Defer` and the `validate` rules work on a generic live
  component's fields, each instantiation keeping its own URL parameters
  under its `urlKey`. [E0206](/errors/E0206) now covers only a generic
  type that isn't live, whose attributes would be registered for the type
  at package level (plain value attributes, `orm` models)
  ([#96](https://github.com/vuka-lang/vuka/issues/96)).
- **Context processors in folder pages.** A page under `pages/` — a route
  group's `(admin)` and a parameter folder's `[id]` included — reads
  `request`, `perms` and the app's processors' variables by name: main
  reaches those packages only through the generated `pages/route.go`,
  which the discovery now follows, also before `route.go` is first
  written. Hover and completion work there too
  ([#94](https://github.com/vuka-lang/vuka/issues/94)).
- **An app processor importing the app's packages.** A processor whose
  package imports another package of the module — one with shapes, or
  one the pages import too — no longer breaks that package's build: a
  processor's package and its imports don't get its variables (reading
  them would be an import cycle) and are compiled first. Where
  processors' packages and their readers still need each other, the new
  [E0428](/errors/E0428) names the processor and the cycle instead of
  an unrelated `could not import`
  ([#95](https://github.com/vuka-lang/vuka/issues/95)).
- **One spelling for request context in vuka files.** `vuka vet`'s
  [W0402](/errors/W0402) now reports `auth.Can(ctx, "x")`,
  `auth.Current(ctx)`, `auth.Current(ctx).Can("x")` and `web.Request(ctx)`
  (read through what `request` has) in markup and Go of `.vuka` and
  `.vuka.html` files, where `perms.Has("x")`, `user` and `request` give
  the same, tracked; when `main` doesn't list `auth.Context` or
  `web.RequestContext`, it says to. `vuka fix ctx-vars` rewrites them and
  drops the imports left unused, and the editor offers the same quick
  fix. [W0403](/errors/W0403) accepts `perms.Has("x")` in the event as its
  check. Plain `.go` packages keep `auth.Can`; from 3.0 the direct calls
  are a compile error in vuka files
  ([#97](https://github.com/vuka-lang/vuka/issues/97)).

## v2.4.0 — 2026-10-23

- **Context processors.** `web.ContextProcessors(…)` in `main` lists the
  functions that give every template request-level values: each a plain
  `func(ctx, deps…) (S, error)`, each exported field of `S` a variable.
  Built in, and listed the same way: `web.RequestContext` (`request`),
  `auth.Context` (`user`, `perms`), `web.MessagesContext` (`messages`, with
  `web.Flash`), `web.CSRFContext` (`csrfToken`), `ui.PageContext` (`page`).
  A processor runs only when a render reads one of its variables, once per
  request or live event; reads are tracked, so a staying layout showing
  `request.Path` updates that spot and stays. `web.Var[T](ctx, "name")`
  reads one in Go, `web.WithVars` gives other values below a context,
  `web.RefreshVars` runs a processor again, `web.Auth(p, gates…)` guards
  one, and `web.Browser(p)` (with `web.Personal` for one reading the
  visitor) lets islands read it; a `web.NotForBrowser` value such as
  `auth.Perms` is never sent. See [context processors](/web/context-processors).
- **Live renders follow the variables.** Each read is recorded against the
  component rendering it, and a processor's inputs with it
  (`web.RecordInput`; `web.Request` records the URL, `auth.Current` the
  identity and its permission version): an event, a broadcast or a
  navigation renders again only the components whose variables changed,
  and results carry across a live connection's pages while their inputs
  hold.
- **Permissions that change under a live page.** auth keeps a permission
  version per user, bumped by `Refresh`, sign-out, `auth.Impersonate` /
  `auth.StopImpersonating` and `RevokeUser`; `auth.Context` gives `user`,
  `perms` (`Has`, with wildcards) and `actor`, the real user while
  impersonating.
- **Flash messages and CSRF tokens.** `web.Flash(ctx, level, text)` (a
  signed cookie over HTTP, the next render on a live page) feeds
  `messages`; `web.ValidCSRFToken` checks `csrfToken`.
- **Testing with variables.** `viewtest.With(vars…)` and
  `viewtest.Context(ctx, vars…)` (`web/v2/viewtest`) give pages and
  contexts their own values.
- **Variables by name in templates.** The compiler reads `main`'s list,
  and a bare name in a shape, its script or a `.vuka` file that names
  nothing else reads the variable, typed: `{me.Name}` is
  `web.Var[*users.User](ctx, "me").Name`. Every package a main imports
  reads them, and one imported by several mains reads what all give
  ([E0422](/errors/E0422)); libraries read them with `web.Var`. Errors for
  a clash ([E0421](/errors/E0421)), a read without a `ctx`
  ([E0423](/errors/E0423)), a typo with a suggestion
  ([E0424](/errors/E0424)) and a list entry that is no processor
  ([E0425](/errors/E0425)).
- **vet for context processors.** `vuka vet` warns about a field or local
  shadowing a variable ([W0401](/errors/W0401)), `web.Request(ctx)` in
  markup where `request` would do ([W0402](/errors/W0402)), a control
  shown behind `perms.Has("x")` whose event checks no `x`
  ([W0403](/errors/W0403)) and a variable stored in a staying layout's
  `Load` ([W0703](/errors/W0703)); vukavet refuses a `web.Browser`
  processor reading the visitor without `web.Personal`
  ([E0426](/errors/E0426)) and checks every `web.Var` read the app links in
  against its list ([E0427](/errors/E0427)).
- **Context processors in the editor.** `vuka lsp` completes the variables
  and their fields, hover gives a variable's type and processor (`from
  users.Context (users/context.go)`), go to definition jumps to the
  processor and its entry in `web.ContextProcessors`, and variables have a
  semantic token type of their own, `vukaContextVar`.
- **Scaffolds list the processors.** `vuka new` writes
  `web.ContextProcessors` with the built-in processors into `main`, so
  they are visible and removable.
- **URL state on components.** A component's field marked
  `@ui.URL("tab")` is filled from the page's query as it renders and
  whenever the URL changes (a patch, back, forward); an event assigning it
  patches the page's URL, the other parameters kept, in place of the
  current history entry. A prop the parent passes wins, then the URL, then
  `@ui.URL("page").Default(1)` or the zero value; a field holding its default
  leaves the parameter out. A bare `@ui.URL` reads the route's path
  parameter of the field's name, and `vuka` reports a folder page rendering
  such a component without that parameter (E0340). See
  [URL State](/web/url-state).
- **`urlKey` keeps two components apart.** `<Table urlKey="requests"/>`
  keeps that instance's parameters as `?requests.page=`; `vukavet`'s new
  `urlstate` check warns when two components one markup renders keep one
  parameter (W0307).
- **`web.Query` from events.** `web.Query.Set(ctx, "status", "open")`,
  `Del`, `Update(ctx, func(url.Values))` and `Get` edit the live page's
  query as one patch per event, without a history entry unless given
  `web.Push`. An `Update` taking `web.URLParams` itself keeps a page mounted
  while its components follow the URL; a Live page's field marked
  `@ui.URL("q")` binds `?q`. `ui.JS.PatchReplace` and livetest's `Back`,
  `Forward` and `History` are new.
- **URL state in the editor.** `vuka lsp` hovers `@ui.URL` with the
  parameter, its default and its `urlKey` form, and completes the package's
  keys inside `@ui.URL("…")`.

## v2.3.0 — 2026-10-23

- **Shape outline and sticky scroll.** `vuka lsp` gives a `.vuka.html`
  shape's struct the whole file down to the end of its markup, holding its
  fields, methods and the markup's elements (`section#todos.list`), so sticky
  scroll keeps the struct pinned over the markup; the methods the markup
  compiles to, and symbols that map nowhere, leave the outline. The Go header
  folds as one. See [keeping the struct in view](/tools/editors#outline).
- **Format Selection.** `textDocument/rangeFormatting` for `.vuka` and
  `.vuka.html` files: `vuka fmt`'s changes on the selected lines only.
- **Markup completion.** Every ARIA attribute with its values and the
  WAI-ARIA roles; the event handlers each element fires (`onSubmit` on a
  form, `onClose` on a dialog, playback events on media), each saying the
  handler forms; `ref`, and the file's refs inside `ui.Ref("…")`. See
  [completing attributes, events and refs](/tools/editors#attributes-events-refs).
- **Keyed loops.** In a range loop over items with an `ID`-like field, the
  first element completes with `key={item.ID}`, and `for` on a markup line
  offers a keyed loop, so [W0102](/errors/W0102) doesn't come up. See
  [keying a loop's items](/tools/editors#keying-loops).
- **VS Code extension 0.8.0.** Defaults that make Tailwind CSS IntelliSense
  find classes in shapes (`class="…"`, `class={…}` strings, `…Class` helpers)
  and turn Emmet on in `.vuka.html` files. See [Tailwind
  CSS](/tools/editors#tailwind).
- **Shape names coloured by what they read.** `vuka lsp` gives the names in
  a `.vuka.html` file's markup semantic tokens of their own — a prop,
  server state, browser state, an event, a component tag, a template local —
  after gopls's in the legend; the VS Code extension maps them to scopes
  themes colour apart. See [telling shape names apart](/tools/editors#shape-tokens).
- **Hover on holes and events.** A hole's name says what it reads, its type
  and where it is set (`set in mount (@ui.Mount)`); an `on…` attribute says
  whether it runs on the server and patches the page, or runs in the
  browser. See [seeing what a name reads](/tools/editors#holes-events).
- **Component tags.** Completion in a component's tag lists its attributes
  with their types, required ones marked (a function's parameters; a
  struct's props may be left out); hover on the tag shows them and links
  its file. See [writing component tags](/tools/editors#component-tags).
- **Routes in the editor.** A code lens above each page and handler (`GET
  /orgs/{org} · live · layout org.Layout · guard org.Guard`), `urls` chains
  and literal `href`s as links and definitions to the page, references on a
  page listing them, and renaming a page file or folder under `pages/`
  renaming the `urls` chains naming it. See [following
  routes](/tools/editors#routes) and [renaming pages](/tools/editors#renaming-pages).
- **Refactoring shapes.** `vuka lsp` extracts a selection of a shape's
  markup into a component of its own (**Extract component…**): a shape file
  in the script form, in `_parts/` beside a page or in the shape's package,
  its props the fields, variables and methods the selection reads, typed by
  the package's type check, and the tag passing them in the selection's
  place. **Move to the logic file** moves a shape's methods, one or all but
  `Title` and its hooks, from its script to the `.vuka` file beside it,
  imports following. Quick fixes for [W0102](/errors/W0102) (a key from the
  item's ID-like field or the index), [W0703](/errors/W0703) (the page's
  title read in the layout's markup; a current link marked with `ui.Link`'s
  `active`), [E0155](/errors/E0155), [E0142](/errors/E0142),
  [E0329](/errors/E0329) and [W0701](/errors/W0701) (on a `pages/` tree's
  `route.go`: `pages.Routes()` passed to the owning main's `web.New`, as
  `vuka fix routes` does). See [refactoring](/tools/editors#refactoring).

- **`<script lang="go">` shapes.** A `.vuka.html` file's Go goes in a
  `<script lang="go">`, the file's first element, and its markup follows
  `</script>`; the file is HTML to every HTML tool. Before and after:

  ```html
  package counter

  type Index struct{ count int }

  func (p *Index) Add() { p.count++ }
  <button onClick={Add}>{count}</button>
  ```

  ```html
  <script lang="go">
  package counter

  type Index struct{ count int }

  func (p *Index) Add() { p.count++ }
  </script>

  <button onClick={Add}>{count}</button>
  ```

  The end is found by Go's tokens, so `</script>` in a string or comment
  doesn't end it ([E0156](/errors/E0156) when nothing does); one script per
  file, first ([E0157](/errors/E0157)). Diagnostics, `vuka lsp` and source
  maps keep the file's positions. The header form still compiles in 2.x and
  is removed in 3.0; `vukavet` reports it ([W0107](/errors/W0107)), the editor
  offers a code action, and `vuka fix shape-script` or `vuka fmt -rewrite`
  converts files. Scaffolds, the docs, the ui kit and the examples use the new
  form. See [laying out the file](/ui/templates#the-file).
- **`<style>` in a shape.** `<style scoped>` after the markup is scoped to the
  shape's elements by the `data-vk-s` attribute, an id from the package and
  the struct's name (`:global(…)` escapes it; slot content keeps the caller's
  scope); a plain `<style>` is the app's global CSS, bundled once even when
  two shapes hold the same stylesheet. See [scoped
  stylesheets](/web/assets#scoped-stylesheets).
- **`vuka fmt` formats the whole shape.** The Go as gofmt does, the markup
  indented two spaces a level with its statements and holes gofmt'ed and long
  tags wrapped an attribute a line, void and childless component tags closed
  (`<br />`, `<ui.Badge />`), and the CSS of a component's `<style>` —
  changing only whitespace that doesn't render. `vuka.toml`'s `[fmt]` table
  sets the `width` (100) and turns on Tailwind class ordering
  (`tailwind = true`, off by default). New flags: `-check` for CI, `-rewrite`
  for the script form. Format-on-save and Format Selection use the same rules.
  See [formatting shape files](/tools/cli#fmt-shapes).
- **Scoped slots.** A slot field of a func type returning `ui.Node`
  (`Row func(T, int) ui.Node`) takes `<slot:row let={t, i}>…</slot:row>`:
  the content is the function's body, `let` names its values with the
  field's types, generic components included (`<components.List[Task]>`).
  See [passing values to a slot](/ui/templates#scoped-slots).
- **Repeated slots.** A slice-of-structs field (`Columns []Column[T]`) takes
  one element per `<slot:column …>`, in order: attributes set the struct's
  fields, the content its one func or `ui.Node` field. `ui.Table` and
  `ui.DataTable` take their columns this way, their Go API unchanged. See
  [repeating a slot](/ui/templates#repeated-slots).
- **Slot fallback content.** In a component's own markup,
  `<slot:actions>…</slot:actions>` renders Actions, or the content when the
  caller left the slot out (`ui.SlotOr`); `ui.Has(node)` reports whether a
  slot was filled, false for nil and white space. See [giving a slot
  fallback content](/ui/templates#fallback).
- **Required slots.** A field tagged `vuka:"required"` is filled at every
  tag, else [E0158](/errors/E0158). See [requiring a
  slot](/ui/templates#required).
- **Slot errors name the slot.** A slot the component doesn't have suggests
  the nearest (`did you mean <slot:actions>?`); a `let` on a slot that takes
  no values, or naming more than it passes, says so.
- **Slots in the editor.** `<slot:` inside a component's tag completes its
  slots with their types (scoped and repeated ones with a `let`); a `let`'s
  names are template locals for the slot's content, coloured as such, and
  hover on one shows its type. See [writing component
  tags](/tools/editors#component-tags).
- **Text for a node.** Quoted text setting a `ui.Node` prop is that text:
  `<ui.Card title="Members">`.
- **Closing a generic tag.** `</List[Task]>` closes `<List[Task]>` as
  `</List>` does, in the compiler and in `vuka fmt`.

### Fixes

- A shape being written — its struct and no markup yet, or a first tag half
  typed — crashed the compiler (`index out of range [-1]`), and with it
  `vuka lsp` on every keystroke until the editor stopped restarting it. It
  is [E0148](/errors/E0148) now, in both forms.
- `vuka lsp` no longer exits on a compiler panic: the file gets an
  [E0001](/errors/E0001) diagnostic with the panic's message, the stack goes
  to the server's log, and the server keeps serving.
- `vuka add page` in an app whose `main` didn't pass `pages.Routes()` to
  its app yet — one made with templ views or as an API, or any app without
  a `pages/` tree — wrote the page and left it unserved. It now adds the
  `pages` import and `pages.Routes()` to the `web.New` of the main that owns
  `pages/`, in `main.go` or `main.vuka` (into the option list `web.New`
  spreads, when it spreads one), and leaves a main that passes it already
  as it is; `vuka add module` does the same when it writes the module's
  page. When it can't tell which main serves the pages, or can't find the
  call, it prints the line to add and where. `vuka fix routes` makes the
  same change, and [W0701](/errors/W0701) points to it.
- `vuka lsp` drops gopls's `go.mod` diagnostics about `malformed import
  path ".../(admin)"` (and `[id]`) for Go files in route-group and parameter
  folders; those packages are still type-checked through the packages vuka
  maps them to.

## v2.2.0 — 2026-10-23

An admin shell — a menu of links in collapsible groups, a header with the
page's title and breadcrumbs, the page below — stays across navigations
and follows the page. See [a header that follows the
page](/web/folder-routes#page-headers) and [the current
group](/web/navigation#the-current-group).

- **Breadcrumbs from the page.** A page with `Crumbs() []ui.Crumb` gives
  its trail, each `ui.Crumb` a `Label` and an `Href` (`""` for the page
  itself), read in a layout with `ui.PageCrumbs(ctx)` as `ui.PageTitle(ctx)`
  reads its title; web reads it when it reads `Title`, and
  `ui.WithPageCrumbs` sets it. A layout that stays renders its markup again
  for every page, so a heading and crumbs read there are the new page's
  while its `Load` doesn't run again.
- **The current link's group.** An element marked `data-vk-active-within`
  is `"true"` while it holds the current link (`ui.Link`'s `active`) and
  `""` when not; a `<details>` so marked opens when the current link comes
  inside it, as the page loads and on navigation, and is the visitor's to
  close.
- A layout reading the page's title or breadcrumbs in its `Load`, or in
  what `Load` calls, reloads on every navigation (`web.ReloadLayout`), as
  one reading the request does; vukavet's [W0703](/errors/W0703) says so
  and points at reading them in the markup.

> [!NOTE]
> Upgrading: a layout storing `ui.PageTitle(ctx)` in its `Load` showed the
> first page's title after 2.1.0's navigations; it now loads on every
> navigation, and vukavet warns. Move the read to the markup and it stays.

### Fixes

- `vuka new` writes a `package-lock.json` beside each frontend's
  `package.json` (the Vue, React and Svelte frontends and the islands'
  `web/`), and the first install runs `npm ci` from it: npm 10, which
  Node.js 20 and 22 ship, crashed resolving those packages from scratch
  (`Cannot read properties of null (reading 'edgesOut')`). Before running
  npm, vuka checks its version: npm 10 installing from a lockfile is a
  warning, and npm 10 installing without one, or adding packages, stops
  with [E0610](/errors/E0610) and the fix, `npm install -g npm@11`.
  `vuka doctor` shows npm's version. pnpm, yarn and bun aren't checked.
- A `<details>` the visitor opened or closed keeps that state when a
  render leaves its `open` as it was; in 2.1.0 the render of a layout that
  stays, on every navigation, closed the menu groups the visitor had
  opened.
- A `.vuka.html` file imports a sibling file's package only for a real
  selector (`time.Now()`): a string or a comment naming it, `"any time."`,
  no longer adds an import the go command reports unused.
- Assigning several tracked fields at once compiles in every form: fields
  of a tracked struct (`c.q.Path, c.q.Page = "", 1`), a field beside a
  field of another (`c.note, c.q.Path = "gone", ""`), and values with no
  type of their own, `nil` or `-1` into an `int64`, which take their
  place's type. See [selectors and mutable
  values](/ui/stateful#selectors-and-mutable-values).
- `delete(p.items, k)`, `clear(p.items)` and `copy(p.slots, src)` on a
  tracked field are writes of it, so the session sees the change; in 2.1.0
  they changed the value unmarked, and the page kept its last render.
- A component's prop of type `ui.JSChain` (or `any`) takes an `on…`
  attribute's statements, `<Chip onClick={manage = !manage}/>`, as an
  element's does, to forward to its button; a prop of a func type is
  [E0101](/errors/E0101) with the prop to declare. See [handing a click to a
  child](/ui/state#click-to-a-child).

## v2.1.0 — 2026-10-23

Folder routes learn three folder conventions, so a page tree can say which
pages share a shell, which share a document, and which files are no page.
See [Folder routes](/web/folder-routes#route-groups).

- **Route groups.** A folder in parentheses, `pages/admin/(console)/`, adds
  no segment to the URL; its `_layout.vuka.html`, guard and middleware apply
  to the pages inside it alone, nested inside the layouts above it. A
  sign-in page at `pages/admin/auth/login.vuka.html` is then
  `/admin/auth/login` outside the admin shell and its guard, beside
  `pages/admin/(console)/index.vuka.html` at `/admin`. The go command sees a
  group as `group_console/`; typed URLs leave it out
  (`urls.Admin.Auth.Login`); `vuka routes` names its layout and guard;
  `vuka add page /admin/(console)/members` makes a page in it.
- **Root layouts.** A layout with no layout above it is a root layout, the
  document of the pages below it: with no `pages/_layout.vuka.html`, each
  top-level folder or group may have its own, also below a parameter
  folder (`pages/[lang]/_layout.vuka.html`). Each route's root layout is in
  `vuka routes -json` (`root`), in web's `RouteInfo.RootLayout`, and on the
  page as `data-vk-root`; in-app navigation between two roots is a full
  page load, under one root a patch as before.
- **Private folders.** A folder of `pages/` named with a leading
  underscore, `pages/_parts/`, routes nothing: its components and helpers
  are a package the pages import, by its Go name
  `example.com/app/pages/private_parts`.
- **Layouts that stay.** A live page navigating to another page under the
  same folder layouts keeps them: from the outermost down to the first
  that differs (another layout, or other path parameters), a layout's
  `Load` doesn't run again, and its elements, scroll, client state, live
  components and islands stay in the page; only what is below the deepest
  shared layout is swapped. Guards run on every navigation all the same.
  A layout whose methods read the request — `web.Request`, its URL or
  query, an `*http.Request` — loads and renders on every navigation as
  before: `vuka` registers it with the new `web.ReloadLayout`, and
  vukavet warns ([W0703](/errors/W0703)). See [layouts that
  stay](/web/folder-routes#layouts-that-stay).
- **The current link.** `ui.Link`'s `active` (`"exact"` or `"prefix"`)
  and `activeClass` mark the link matching the address with
  `aria-current="page"` and the class, in the browser, on load, after
  every navigation and on back and forward: a layout's menu follows the
  page without reading the request. See [the current
  link](/web/navigation#the-current-link).
- **Prefetch.** A `ui.Link` the browser loads as a page is fetched ahead
  on hover, focus or touch (`prefetch="viewport"`: once in view;
  `prefetch="off"`: never), same-origin `GET`s only, none when the visitor
  saves data, kept 30 seconds. A prefetched live page's session waits
  parked for the page to open, so its mount hook runs once. See
  [prefetch](/web/navigation#prefetch).
- A navigation's reply carries the new page's title, which the runtime
  sets on the document, and resets the scroll of the page's `<main>` along
  with the window's.
- ui: `ui.Keyed(key, node)` scopes the stateful components in a node as an
  element's `key` does, without markup; `live.Route.Keep` keeps a
  navigation's instances in place as a patch does.
- New diagnostics: [E0722](/errors/E0722) two routes on one URL told apart
  only by a group, naming both files; [E0723](/errors/E0723) a name in
  parentheses that is no group; [E0724](/errors/E0724) the home page
  outside the top-level groups' root layouts.
- **Go in route folders.** A `.go` file in a group or a `[id]` folder —
  logic or `_test.go` — is the folder's package: vuka copies it, with a
  line directive naming it, where the go command sees the folder
  (`group_console/`, `param_id/`), for `vuka build`, `run`, `test`, `vet`
  and `gen`, and `vuka lsp` type-checks it there, its errors and
  definitions at the real file. So does one beside Vuka source in a private
  folder. In 2.0.0 each was [E0716](/errors/E0716), which now names only a
  `.go` file of another package than its folder's, or one named `[id].go`.
  The Go-name directories are the overlay's alone: vuka no longer makes
  empty `param_id/`, `group_admin/` or `private_parts/` directories in the
  source tree (`vuka test` and `vet` make them for their run only), and
  removes empty ones an older vuka left; `vuka gen -inplace` still writes
  its files there.
- **Route files on disk under `vuka run`.** `vuka run` and `vuka run
  -watch` write `pages/route.go` and `pages/urls/urls_gen.go` to disk
  when their text changes (the overlay holds the same), so an editor
  resolves the `pages` and `pages/urls` imports of hand-written code; in
  2.0.0 they were in the build's overlay alone. They are generated:
  gitignore them. Nothing else `vuka run` generates is written.
- Upgrading: a folder under `pages/` named `(x)` or `_x` meant nothing
  before and is a group or a private folder now; a private folder holding
  Vuka source compiles as `private_x`, so its importers name that path.
  The vuka command of v2.1.0 needs web v2.1.0: it registers a layout
  reading the request with `web.ReloadLayout`, and the full-load
  navigation between root layouts and the layouts that stay come with it.

### Fixes

- A page's `Post`, `Put`, `Patch` or `Delete` handler may take a provided
  dependency as a pointer beside its body struct (`Post(ctx, store
  *Store, form PetForm)`); 2.0.0 read the pointer as a second body,
  [E0709](/errors/E0709). A page whose markup sends one of those names as
  an event is E0709 now, rather than the event being taken as the handler.
- The constructor Vuka writes for a type whose fields differ only by case
  (`Greeter`, `greeter`) gives each its own parameter.
- `p.rows = load()?` on tracked state sets it once the call succeeds, as
  `rows := load()?; p.rows = rows` would; and `p.a, p.b = p.b, p.a`
  assigns tracked fields too. Both were [E0150](/errors/E0150).
- A stateful shape's value in a hole (`{Card{…}}`, `{c}`) renders as the
  component, by its address; one whose address can't be taken is
  [E0128](/errors/E0128) instead of text printed with `fmt`.
- Statements among a component tag's children (`label := …`) run before
  its `<slot:x>` content, which may read them.
- A component attribute reading browser state (`<ui.Show when={tab ==
  "b"}>`) is lowered as an element attribute's is, once the markup's bare
  names are known; one whose prop can't take a state is
  [E0125](/errors/E0125), not a Go type error.
- `vuka lsp` offers no completion in markup text or a quoted attribute's
  value: a sentence ending in a package's name (`…failing to sync.`) no
  longer gets Go items whose acceptance imports the package.
- Another package's model reads as one of this package's:
  `users.User.Objects` is `orm.Objects[users.User]()` (in 2.0.0 it was a
  method expression of `orm.Model`'s `Objects`), and `users.User.Name` the
  typed field.
- A shape's field named like a built-in (`Result`, `Option`) is the field
  in its markup.
- A component attribute names an initialism's field in lowerCamel: `id`
  for `ID`, `uid` for `UID`.
- A `{…}` naming a value in a `<style>` is [E0128](/errors/E0128): a
  stylesheet has no holes, and `style:--x={v}` sets a CSS variable.
- `vuka fmt` leaves a `.vuka.html` header's one-line struct as gofmt
  writes it.
- A `_test.go` file writing a page's tracked state is
  [E0155](/errors/E0155) naming the `_test.vuka` file to move it to, where
  the write is lowered.
- A shape's methods may live in a `.vuka` file beside its `.vuka.html`
  file: reads and writes of its tracked server state (`p.rows = v`,
  `len(p.rows)`, `p.n++`, `p.rows[i].Done = true`) and writes of its
  `ui.State` fields are lowered there exactly as in the header. In 2.0.0
  only the header's methods were, so a method beside the shape failed with
  a Go type error ([E0002](/errors/E0002)) naming `uigen.Assign`. See
  [where its methods live](/ui/stateful#where-its-methods-live).
- A `.go` file reading or writing tracked state — which the compiler can't
  rewrite there — is now [E0155](/errors/E0155), naming the field, the
  shape file declaring it and the function, and saying to move the method
  to a `.vuka` file or into the header.
- `vuka vet` runs [vukavet](/tools/vukavet)'s checks by default. In 2.0.0
  plain `vuka vet ./...` ran go vet's own analyzers, so vukavet's reports
  (a live list without keys, [W0102](/errors/W0102), among them) appeared
  only with `-vettool=$(go tool -n vukavet)`. It now uses the app's `go
  tool vukavet` when its `go.mod` declares the tool, else a `vukavet`
  beside `vuka` or on `PATH` of its version, else one it builds at its
  version into the user's cache, and says on stderr when there is none.
  `-vettool=…` still overrides it; `VUKA_VUKAVET` names one, or `off` for
  go vet's own analyzers only. An app that passed `vuka vet` may now see
  vukavet's warnings.
- A folder page's unexported dependency fields are injected: a page
  `pages/route.go` builds with `&T{}` gets each unexported field of a
  provided type (`users *users.Service`) set for the page, its mount hook
  and its `Post`, `Put`, `Patch` and `Delete` handlers, as a page-backed
  island's are; so do a live folder page and a folder's layouts and
  guards. In 2.0.0 the field was nil unless the page had a `NewT` taking
  the dependency. Tracked state, `ui.State` fields, exported props and
  fields of types nothing provides are left alone; a dependency field the
  URL would bind too is [E0419](/errors/E0419). See [dependency
  fields](/web/dependency-injection#dependency-fields). Needs web v2.1.0.
- A composite literal setting a shape's tracked state, `Badge{count: 0}`
  or `&Badge{Label: "a", count: n}` (keyed or in order), in a `.vuka`
  file or a shape's header sets the field's tracked value; in 2.0.0 it
  was a Go type error naming `uigen.Assign`. In a `.go` file it is
  [E0155](/errors/E0155), naming the literal and saying where to move it.

## v2.0.0 — 2026-10-23

vuka 2.0 has one way to write each part of a UI: markup lives in
`.vuka.html` files, each declaring the one struct it renders above its
markup; a page is a file under `pages/`, its path its URL; a hook is a
method marked with its attribute; and a handler's parameters are filled by
their types. Two vuka apps now look alike, and the docs teach one form of
each thing. The design and its decisions are in the
[one-way RFC](https://github.com/vuka-lang/vuka/blob/main/docs/rfc/one-way.md).

Every module of the set moves to a `/v2` path, and the orm with it:

| 1.x | 2.0 |
|---|---|
| `github.com/vuka-lang/vuka` | `github.com/vuka-lang/vuka/v2` |
| `github.com/vuka-lang/ui` | `github.com/vuka-lang/ui/v2` |
| `github.com/vuka-lang/web` | `github.com/vuka-lang/web/v2` |
| `github.com/vuka-lang/orm` | `github.com/vuka-lang/orm/v2` |
| `github.com/vuka-lang/vuka/cmd/vuka` | `github.com/vuka-lang/vuka/cmd/vuka/v2` |
| `github.com/vuka-lang/vuka/cmd/vukavet` | `github.com/vuka-lang/vuka/cmd/vukavet/v2` |

The runtime is v2.0.0: generated code needs `github.com/vuka-lang/vuka/v2`
at v2.0.0. A 1.x app keeps building on its 1.x requirements; the
[compatibility promise](/project/compatibility) now applies to the v2 set.

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@v2.0.0
```

### One struct per `.vuka.html` file

- A `.vuka.html` file is a Go header — package clause, imports, one struct,
  its hooks and methods — then markup, from the first line starting with
  `<`. The struct no longer lives in a sibling `.go` file, and a file holds
  no second template: a partial is its own file, used as a tag.
- `.vuka` and `.go` files are logic only: services, models, helpers,
  `main`. Markup anywhere else is [E0151](/errors/E0151); a template file
  declaring no struct is [E0152](/errors/E0152), a second template in one
  [E0153](/errors/E0153).
- A file whose markup is one island tag is the page that island backs; its
  struct's exported fields are the island's props
  ([E0154](/errors/E0154)).
- A struct's fields say what they are: exported fields are props (on a page,
  filled from the path segment of their name, else the query), unexported
  fields are server state the page tracks — or dependencies, when their type
  is a pointer or interface the container provides — and `ui.State[T]`
  fields are browser state. A struct is live when it embeds `ui.Live`,
  declares a hook or wires a server handler in its markup.
- Writing `ui.HTML` by hand is [W0106](/errors/W0106); it stays the
  compiler's output.

### Client state is a field

`ui.State[T]` is declared as a field (or a package variable in a logic
file), read as a plain variable in markup and written by assignment in an
`on…` attribute: `onClick={open = !open}`, `onClick={tab = "env"; Save()}`.
Its Go API is `Get` and `Set`. `ui.UseState`, `ui.UsePageState`,
`Toggle`, `Inc`, `Dec`, `Reset`, `Derived`, `At` and `ui.Assign` moved to
`github.com/vuka-lang/ui/v2/uigen`, the package generated code calls;
`vukavet` reports a hand-written use ([W0105](/errors/W0105))
([Client state](/ui/state)).

### Folder routes

- A path parameter is a bracketed name: `pages/pets/[id].vuka.html` is
  `/pets/{id}`, a `[employer]/` folder holds a segment shared by a layout,
  several pages and an `api/` subtree, and `[path...]` is the catch-all. The
  parameter's type and `validate:` rules are the field of its name on the
  struct that reads it. `params.go` folders and their `Params` structs are
  gone ([E0714](/errors/E0714)).
- A folder's layout is its `_layout.vuka.html` ([E0715](/errors/E0715)).
  `guard.go` and `middleware.go` stay Go.
- The `vuka` command writes one file, `pages/route.go`, whose `Routes()` is
  the tree's registration; `main` passes it to `web.New`. A 1.x
  `pages_gen.go` in its way is [E0718](/errors/E0718).
- Pages are declared under `pages/` only: a page registered with a
  decorator or a `web.Routes` call is [E0720](/errors/E0720), a page under
  `pages/` whose markup is written in Go [E0721](/errors/E0721), and a
  hand-written `web.PageRoute`, `LayoutOf`, `GuardOf`, `UseOf` or `Receiver`
  [W0702](/errors/W0702). `app.Get` and the other route methods stay, for
  APIs in Go.
- A `pages/` directory beside a `package main` is that main's routes, so a
  module holds several apps ([Several apps in one module](/web/folder-routes#several-apps-in-one-module)).

### Hooks are attributes

`@ui.Mount`, `@ui.OnChange` and `@ui.Loads("group")` on a method, and
`@ui.Bind`, `@ui.URL` and `@ui.Defer` on a field, are the only way to
declare a hook. A method named `Mount` or `Changed` is an ordinary method —
and, being exported, an event the browser can call
([E0337](/errors/E0337)); the constructor hooks `OnMount`, `Loads` and
`OnChange` on `ui.Live` are [E0338](/errors/E0338). A hand-written
`LiveSchema` method is reported by `vukavet`; the compiler still writes one
([Hooks](/ui/stateful#hooks)).

### Parameters by type

A hook, a page handler, a live event and an endpoint read their parameters
by type: `context.Context`, a struct (the form or JSON body), `url.Values`,
a scalar (the path segment of its name, else the event's value),
`*http.Request`, `http.ResponseWriter`, the signed-in user, and any type
registered with `app.Service(NewX)` or `web.Provide`. A parameter with no
source is a start-up error naming it. `web.Params` is gone: the command
records parameter names from the source. `@web.Service` and the `inject:`
tag are [E0420](/errors/E0420); a service is a struct with a constructor,
registered in `main` ([Parameters](/web/parameters),
[Dependency injection](/web/dependency-injection)).

### Removed

- The diagnostics of retired forms are gone with them: E0140 (a template
  file with no type to render), E0145 (a template named like an element)
  and W0104 (an exported template without a doc comment). Their codes are
  not reused.
- The conformance suite's hand-written Go twins are deleted: each app of
  `examples/conformance` is written the one way and compared with the Go
  it ejects to, with and without the generated speed-ups.
- vuka's `internal/format`, `internal/load` and `internal/tsprops` are
  `toolchain/format`, `toolchain/load` and `toolchain/tsprops`, for the
  command and the editors; they are outside the promise.
- The docs show one form of each sample; the Vuka / Go switch is gone.

### Moving an app from 1.x

There is no migration command: an app moves to 2.0 by hand, and the
compiler guides it. Each retired form is an error whose help names the one
way to write it, and each error's page shows the change, wrong and right.

**1. The import paths.** Move go.mod to the v2 set, then rewrite the
imports in the app's source:

```sh
go get github.com/vuka-lang/vuka/v2@v2.0.0 github.com/vuka-lang/ui/v2@v2.0.0 github.com/vuka-lang/web/v2@v2.0.0
go get github.com/vuka-lang/orm/v2@v2.0.0     # when the app uses the orm
go get -tool github.com/vuka-lang/vuka/cmd/vuka/v2@v2.0.0 github.com/vuka-lang/vuka/cmd/vukavet/v2@v2.0.0

find . \( -name '*.go' -o -name '*.vuka' -o -name '*.vuka.html' -o -name '*.templ' \) -not -path './node_modules/*' -print0 |
  xargs -0 perl -pi -e '
    s#github\.com/vuka-lang/vuka/cmd/(vuka|vukavet)\b(?!/v2\b)#github.com/vuka-lang/vuka/cmd/$1/v2#g;
    s#github\.com/vuka-lang/(vuka|ui|web|orm)\b(?!/v2\b|/cmd/vuka)#github.com/vuka-lang/$1/v2#g'
```

The rewrite also changes `github.com/vuka-lang/...` in comments and string
literals, links included; read the diff. Then drop the 1.x requirements and
tools from go.mod (`go mod edit -droprequire github.com/vuka-lang/web`, and
so on, `-droptool` for the old command and `vukavet`). Sub-packages follow
their module: `github.com/vuka-lang/ui/validate` is
`github.com/vuka-lang/ui/v2/validate`, `github.com/vuka-lang/web/auth`
`github.com/vuka-lang/web/v2/auth`.

**2. Pages into `pages/`, as `.vuka.html` files.** A page's struct moves
from its `.go` file into the header of its template, the template moves to
the page's path under `pages/`, and a `params.go` folder becomes a
bracketed one, its field moving onto the struct that reads it:

```text
1.x                                      2.0
pages/pets/pet/params.go   Params{Pet}   (gone)
pages/pets/pet/index.go    type Index    pages/pets/[pet]/index.vuka.html
pages/pets/pet/index.vuka.html           (one file: struct, methods, markup)
pages/layout.go + layout.vuka.html       pages/_layout.vuka.html
pages/pages_gen.go                       pages/route.go (written by vuka)
```

```diff
- // pages/pets/pet/index.go
- package pet
-
- type Index struct {
- 	Params
- 	store *store.Pets
- }
-
- // pages/pets/pet/index.vuka.html
- package pet
- <h1>{store.Name(Pet)}</h1>
+ // pages/pets/[pet]/index.vuka.html
+ package pet
+
+ import "example.com/shop/store"
+
+ type Index struct {
+ 	Pet   int64 `validate:"min=1"`
+ 	store *store.Pets
+ }
+
+ func NewIndex(s *store.Pets) *Index { return &Index{store: s} }
+ <h1>{store.Name(Pet)}</h1>
```

A partial (a `func` template after the main one) becomes its own
`.vuka.html` file, a struct whose exported fields are its parameters, used
as a tag; a component whose struct was in Go moves the same way. JSX in a
`.vuka` file moves to a `.vuka.html` file beside it.

**3. Hooks as attributes.**

```diff
- func (p *Search) Mount(ctx context.Context) error { … }
- func (p *Search) Changed(ctx context.Context, c ui.Changed) error { … }
+ @ui.Mount
+ func (p *Search) mount(ctx context.Context) error { … }
+
+ @ui.OnChange
+ func (p *Search) changed(ctx context.Context, c ui.Changed) error { … }
```

A `LiveSchema` method or constructor hooks become the same attributes, and
a field's `l.Field(&p.Q, ui.Bind{…}, ui.URL("q"))` becomes
`Q string @ui.Bind{…} @ui.URL("q")`.

**4. `main` passes the routes.** The blank import of the pages package
becomes a call, and services are registered on the app:

```diff
  import (
- 	_ "example.com/shop/pages"
+ 	"example.com/shop/pages"
- 	"github.com/vuka-lang/web"
+ 	"github.com/vuka-lang/web/v2"
  )

  func main() {
- 	a := web.New()
+ 	a := web.New(pages.Routes())
+ 	a.Service(store.NewPets)
  	log.Fatal(a.Run(""))
  }
```

**5. Build, and follow the errors.** `vuka build` (or `go vet
-vettool=$(go tool -n vukavet) ./...` in a Go app) reports each form still
in the 1.x shape, with its fix: [E0151](/errors/E0151)–[E0154](/errors/E0154)
for markup, [E0714](/errors/E0714)–[E0716](/errors/E0716) and
[E0718](/errors/E0718) for folders, [E0720](/errors/E0720),
[E0721](/errors/E0721) and [W0702](/errors/W0702) for pages outside
`pages/`, [E0337](/errors/E0337) and [E0338](/errors/E0338) for hooks,
[E0420](/errors/E0420) for dependencies, [W0105](/errors/W0105) and
[W0106](/errors/W0106) for the compiler's own API written by hand. Then run
the tests.

### Known issues

- `vuka upgrade` doesn't rewrite 1.x import paths; use the recipe above.
- A hole, `{expr}`, inside `<script>` or `<style>` is written out literally,
  not evaluated. Pass a value to a script through an element's attribute
  (`data-total={total}`) and read it there.

## v1.5.0 — 2026-10-22

- The development toolbar starts as a small right-edge indicator. Hover
  nearby or focus it to reveal the handle; click to open the full drawer.
  Touch devices keep a larger tap target, and reduced motion is respected.
- Live tables now display partial checkbox selection using the browser's
  indeterminate state on initial render and subsequent patches.

- Request panic recovery now covers middleware, custom routers and page
  rendering as well as compiled handlers. Uncommitted responses answer a
  safe 500 through `OnError`; panic details and stack traces stay in the
  server log. A broken error handler falls back to a generic 500.
- A panic after a response is written or flushed aborts that response,
  preserving `http.ErrAbortHandler`. HTTP/2 push, streaming and WebSocket
  upgrades remain available. Hijacked connections are closed on unexpected
  panics.
- A panic during a live render or guard closes only the affected socket
  with code 1011 and a generic reason, discarding its session. Startup
  errors and panics in application-created goroutines retain their
  existing behavior.

## v1.4.0 — 2026-10-22

- Templates can read the current render's `ctx` in main markup, struct
  views and helper templates, without retaining a request on the component.
  Existing explicit context parameters and `Render()` methods remain
  compatible. The compiler uses `ui.Ctx`, preserving HTML escaping and
  live frame trees ([Render context](/ui/templates#render-context)).
- Live components that read context are rendered again instead of reusing
  output solely because their props and Assigns are unchanged. Permission
  and identity refresh remains the auth layer's responsibility; handlers
  still authorize protected actions server-side.
- Regression coverage: per-user output, escaping, cancellation, implicit
  and explicit context, and context-dependent live updates in both HTML
  and tree protocols.

## v1.3.0 — 2026-10-21

- Middleware in folder routes: a `middleware.go` declaring `Middleware` —
  `func(next http.Handler) http.Handler`, or `func(deps…) web.Middleware`
  with its dependencies from the container — wraps every page, handler and
  endpoint below its folder, the root's outermost: headers, logging, rate
  limits, a short-circuited response. It runs before the guards and layouts
  load and sees the response on the way out; on a live page it wraps the
  `GET` and the socket's join, not the events
  ([Middleware](/web/folder-routes#middleware)). A wrong signature is
  [E0710](/errors/E0710); `vuka routes` prints a MIDDLEWARE column.
- `vuka.RegisterType[T](attrs…)` reaches stateless folder pages, method
  handlers and endpoints, as it already did live pages:
  `vuka.RegisterType[Login](auth.Public())` opens a stateless login page
  and its `Post` ([Attributes on a page type](/web/folder-routes#attributes-on-a-page-type)).
- Example: `examples/folder-routes` stamps and logs every request from
  `pages/middleware.go` and rate-limits its API from
  `pages/api/middleware.go`.

## v1.2.0 — 2026-10-21

- Endpoints in folder routes: an exported type under `pages/` with `Get`,
  `Post`, `Put`, `Patch` or `Delete` methods and no markup answers those
  methods on its path, so a JSON API is folders like the pages
  (`pages/api/employers/`). A `GET`'s struct parameter is its query string,
  the others' their body; a value answers JSON, an error its status, a nil
  error alone 204, a `ui.Node` a page, a `web.Redirect` a redirect.
  `pages/urls` links to them and `vuka routes` marks them `(endpoint)`
  ([Endpoints](/web/folder-routes#endpoints)).
- Guards: a `guard.go` declaring `type Guard struct` with a `Load(ctx,
  deps…) error` checks every page, handler and endpoint below its folder
  and renders nothing — an API key, a permission. Guards and layouts load
  root-first, a folder's guard before its layout, and the first error
  answers the request, before the page mounts or a body is read. web adds
  `web.GuardOf[T]()` ([Guards](/web/folder-routes#guards)).
- A page or endpoint in `pages/` with nothing to inject needs no
  constructor: `vuka gen` writes `New<T>` for it.
- `Mount` and `Changed` by name are a live page's hooks again, the simple
  form: `func (p *T) Mount(ctx context.Context) error`. Their signatures
  are checked and the browser can't call them; `LiveSchema` and the
  constructor stay the way to unexported hooks and to field attributes
  ([Hooks](/ui/stateful#hooks)).
- Example: `examples/folder-routes` has a JSON API under `pages/api`
  behind a guard.

### Changes

- On a stateless page the layouts' `Load` now runs before the page's
  `Mount` (in 1.1 it ran after), so a guard or a layout protects `Mount`
  too.
- An exported page-shaped type in `pages/` is a route even without a
  `New<T>`. A component the pages share belongs in another package
  (`components/`) or unexported; one that became a route shows in `vuka
  routes`, and a path it takes from another page is E0701.
- `Mount` and `Changed` found by name no longer warn (W0303 is only for
  `Load<Group>` now), and `vuka fix hooks` leaves them as they are.

## v1.1.0 — 2026-10-21

- A live page's hooks in its constructor: `l.OnMount((*Login).mount)`,
  `l.Loads("stats", (*Login).loadStats)`, `l.OnChange(…)` on its `ui.Live`,
  by method expression; a page whose only schema is its hooks needs no
  `LiveSchema` method ([Hooks](/ui/stateful#hooks)). `vukavet` checks them
  (E0336, W0304, W0305). Additive: `LiveSchema` is unchanged.
- Folder routes: the module's `pages/` folders are the site's paths, each
  page type a route — `Index`, `New`, `Edit`…; a folder with a `params.go`
  is a typed parameter (`employer/` is `{employer}`, a `web.Rest` field
  `{file...}`, a segment that doesn't parse a 404); a `layout.go` wraps every
  page below it, composed parent to child, its `Load` fetching what it shows
  and guarding every page and handler beneath it; `Post`, `Put`, `Patch` and
  `Delete` methods on a page answer on its path; `Title()` names the
  document (`ui.PageTitle`); an `about.vuka.html` with no Go type is a page
  of its own; a catch-all folder serves a client app's shell under its
  path, the pages beside it still winning. `vuka gen` writes the
  `web.Routes` calls and `pages/urls`, a typed link to every page
  (`urls.Employers.Employer(7).Edit.Path()`); `vuka routes` prints the
  table, and conflicts are errors at generation (E0701–E0709). web adds
  `web.Rest`, `web.PageRoute()`, `web.LayoutOf[T]()` and
  `web.Receiver(ctor)`; a stateless page may have a `Mount`. Example:
  `examples/folder-routes` ([Folder routes](/web/folder-routes)).

## v1.0.0 — 2026-10-20

The first release under [the compatibility promise](/project/compatibility):
vuka, ui, web, the `vuka` command and `vukavet` at v1.0.0, released with
orm v1.0.0. From here on, an app that builds and passes its tests on v1.x
keeps doing so on every later v1. The runtime is v1.0.0: generated code
needs the vuka module at v1.0.0, and `vuka fix runtime` (which every
upgrade runs) requires it.

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@v1.0.0
vuka upgrade v1.0.0
```

In an app written in Go, with the command and the analyzer as tools of the
module:

```sh
go get github.com/vuka-lang/vuka@v1.0.0 github.com/vuka-lang/ui@v1.0.0 github.com/vuka-lang/web@v1.0.0 github.com/vuka-lang/orm@v1.0.0
go get -tool github.com/vuka-lang/vuka/cmd/vuka@v1.0.0 github.com/vuka-lang/vuka/cmd/vukavet@v1.0.0
go vet -vettool=$(go tool -n vukavet) ./... && go test ./...
```

### Plain Go is the language, `.vuka` the sugar

A vuka app can now be a normal Go module: `.go` files and `.vuka.html`
templates, built with `go build` and run with `go run .`, with no vuka
command at all. The orm, web (routes, live pages, islands, auth, deferred
fields, validation) and ui behave the same as in a `.vuka` app, because a
`.vuka` file compiles to exactly that Go
([vuka as a library](/guide/two-ways)):

- **Generated code uses the public API only** and reads as hand-written
  Go: gofmt'd, one import block, real names (`areaCircle`, not
  `area__Circle`), doc comments carried over, `//line` directives only where
  the lines stop following the source, no absolute paths.
- **`vuka eject [-rm] [files | dir | dir/...]`** writes a `.vuka` file's
  Go beside it; `-rm` deletes the `.vuka` and leaves plain source — its
  markup moved to `.vuka.html` templates — that builds and behaves the same.
- **The generated speed-ups are optional.** Without `orm generate`'s
  scanners the orm reads rows by reflection (1.05–1.07× end to end on
  SQLite), without render tracking a live component is diffed whole, and
  without `vuka_calls_gen.go` `vuka.Wrap` calls by reflection. One line opts
  back in: `//go:generate go tool vuka gen`.
- **Each page has a Vuka / Go switch**: the docs show every sample both
  ways, and the Go ones are type-checked against the libraries.

### `.vuka.html` templates

Markup lives in template files beside the Go (or `.vuka`) it belongs to,
compiled as the package's Go:

- `orders.vuka.html` starts with its package clause; the markup before its
  first `func` line is the `Render` of `Orders` (or the `View` of a struct
  component, which is then itself a `ui.Node`), its fields and methods by
  bare name; `func` lines start partials and function components.
- Go statements start lines (`if`, `for`, `switch`, `x := …`), holes are any
  Go, attributes take `class:late={…}`, `{attrs...}`, `bind:value` and
  `style:prop`, and an event's call with no result is its handler
  (`onClick={p.Cancel(o.ID)}`).
- Component tags are struct components (`<Card title="…">`, attributes
  camelCase, `<slot:footer>` filling a `Node` field), stateful components,
  and the package's partials and functions returning `ui.Node`
  (`<navLink item={i}/>`, `<layouts.NavLink …/>`), every parameter but
  `ctx`, `children` and slots required (E0144; an unknown tag is E0143 with
  a did-you-mean). A function component taking `ctx` first is a tag too.
- A template's `<script lang="ts">` and `<style scoped>` are its
  component's, bundled and scoped by the vuka command; `<script is:inline>`
  takes `{json(x)}`.
- In plain Go the same markup is `ui.HTML`, its holes labelled with their
  Go and filled by the values after it; JSX in `.vuka` files lowers to it.
- `vuka run|build|test|vet` compile templates through the overlay, `vuka
  gen` writes `x_html.go` for a plain `go build`, `vuka fmt` formats them,
  and `vuka lsp` serves them (below).

The ui kit is written this way: each component is a struct whose fields
are its props, its markup a `.vuka.html` beside it — `ui.Card{Title: "…"}`
is a `Node`, a `<Card title="…">` tag in a template.

### Registration in Go

Every decorator is a call a person can write, and the transpiler writes
that call:

- **`web.Routes`**, the registry every app takes at `web.New`, and `App`'s
  methods of the same names: `web.Routes.Get("/pets/{id}", ShowPet,
  web.Params("id", "store"))`, `Live`, `Controller`, `Service`, `Handle`.
  Options are typed values — `web.Use(mw)`, `web.Layout(l)`,
  `web.Template(f)`, an auth gate such as `auth.Required()`,
  `ui.Island(…)` — and a gate on a route applies to its requests and its
  live page's events. Constructors are `NewX`.
- **Type decorators and declarers** take `vuka.TypeFor[T](NewT)` and
  `vuka.DeclOf(fn)`; a call decorator is `vuka.Wrap(fn, vuka.FuncInfo{…},
  decorators…)`, with typed adapters in `vuka_calls_gen.go` as a speed-up.
- **Overloads are named as Go names such functions**: `areaCircle`,
  `AddIntInt`, a live component's `UpdateNewMessage` (`ui.IsUpdate` takes
  it, and still `Update__T` from older commands).
- **Field references by address**: `vuka.RefTo(func(p *Post) *string {
  return &p.Title })`.

### Schema, LiveSchema and typed fields

Attributes are values, given in one method per type:

- **orm models**: `func (p *Post) Schema(s *orm.Schema)` with
  `s.Model(orm.Meta{Table: "posts"})` and `s.Field(&p.Title, orm.Char{Max:
  200})`; `.vuka` attributes lower to it. A pointer to no field, a nil
  attribute or a field behind a pointer is E0501–E0503.
- **Typed fields**: `orm generate` writes `PostFields` beside the model
  (`post_gen.go`) — `PostFields.Author.Email.IEndsWith(…)` through
  relations, `PostPath` for paths — and number fields take arithmetic
  (`PostFields.Views.Add(1)`). In `.vuka`, `Post.Title` is that field and
  `Post.Objects` is `orm.Objects[Post]()`, the model's manager.
- **Live pages**: `func (p *Search) LiveSchema(l *ui.LiveSchema)` with
  `l.Field(&p.Q, ui.Bind{Debounce: 300 * time.Millisecond}, ui.URL("q"))`,
  `l.Loads("lists", p.loadLists)`, `l.OnChange(p.changed)`,
  `l.Mount(p.mount)`. In `.vuka`: `Q string @ui.Bind{…} @ui.URL("q")` and
  `@ui.Mount` on an unexported method. Hooks may be unexported, so the
  browser can't call them; only exported methods without a hook are
  `live.*`. Fields and hooks are checked at boot and by vukavet (E0335,
  E0336). `ui.NoDebounce` sends a bound field at once.
- **A routed live page owns its exported fields**: what its handlers,
  hooks and bound fields set stays set across renders, a patch sets only
  the fields its URL binds, and a focused input takes a value the server
  changed (a form cleared once sent clears). An event's struct payload is
  checked against its `validate:` tags, as a route's.

### Islands for plain Go

- `ui.RegisterIsland[Chart]()` finds its component as a bare `@ui.Island`
  does; a route's option places a page backed by its island:
  `app.Live("/board", NewBoard, ui.Island(ui.Layout(page)))`.
- `go tool vuka build-islands` reads the registrations from Go source and
  builds the islands into the frontend's build (`web/dist/.vuka/`), with
  `islands.d.ts` and `vuka-live.d.ts` typed from the Go structs; a missing
  component is E0310 naming the call, a missing frontend E0609. Opt in with
  `//go:generate go tool vuka build-islands`.
- `web.Dev()` runs Tailwind, Vite and `vuka build-islands -watch` for
  `VUKA_DEV=1 go run .`, when they're installed, and does nothing in a
  production binary. Script islands (`.ts`) need no Node.

### vukavet

`go vet` with vuka's checks, for apps in plain Go, with the codes the boot,
the compiler and `vuka vet` use: routes (E0401 across imported packages,
E0403, E0405, E0406, E0418, E0419, E0331), live and island pages
(E0322–E0330, E0333, E0335, E0336), `ui.HTML` against its holes (E0127,
E0128, E0132) and orm `Schema` methods (E0501–E0503). It is released with
the set from v1.0.0 (`cmd/vukavet/v1.0.0`).

```sh
go get -tool github.com/vuka-lang/vuka/cmd/vukavet@v1.0.0
go vet -vettool=$(go tool -n vukavet) ./...
```

### Conformance

Every app of `examples/conformance` — `library-users`, `crud`, `live`,
`components`, `islands`, `islands-vue` — exists as `.vuka` and as
hand-written Go, and CI builds each five ways (`vuka build`; Go with and
without the generated speed-ups; ejected, with and without them) and
requires the same HTTP responses, live replies, SQL and generated files from
each. A difference is a bug.

### Editor

- `vuka lsp` serves `.vuka.html`: diagnostics in the template's own
  positions, hover, definition and completion of the Go in holes and
  statement lines as in a Go file of its package (the component's fields and
  methods first), of tags and their parameters, and semantic tokens. A
  template with a syntax error leaves the rest of its package checked.
- The VS Code extension, 0.5.0 (its own version), highlights `.vuka.html`
  and `ui.HTML` markup, and is published to Open VSX. Zed reads templates as
  HTML, coloured from `vuka lsp`.

### Breaking changes since v0.50.0

`vuka upgrade v1.0.0` moves vuka, ui and web to v1.0.0, and the orm to
v1.0.0 when the app requires it; then it runs these `vuka fix` fixers, then
`vuka vet ./...` and `vuka test ./...`. The go.mod and go.sum it started
from are kept in `.vuka/upgrade/`.

| Fixer | What it rewrites | Runs |
|---|---|---|
| `runtime` | requires the vuka runtime v1.0.0 | every upgrade |
| `templates-package` | starts each `.vuka.html` with its directory's package clause (a missing one is now E0142) | every upgrade |
| `db-schema` | `orm.Schema{…}` (the names-set struct) → `orm.DBSchema{…}`; `orm.Schema` is now a model's `Schema` method's | every upgrade |
| `attributes` | a live page's `bind:`, `url:`, `defer:`, `validate:` tags and a model's `orm:` tags → `@ui.Bind`, `@ui.URL`, `@ui.Defer`, `@validate.…`, `@orm.Column(…)`, … (the tags still work, with W0302) | v1.0.0 |
| `hooks` | `Mount`, `Changed`, `Load<Group>` found by name → `@ui.Mount`, `@ui.OnChange`, `@ui.Loads("group")` (still found, with W0303) | v1.0.0 |
| `ui-structs` | `ui.Card(ui.CardProps{…})` → `ui.Card{…}`, `ui.Table(ui.TableProps[T]{…})` → `ui.Table[T]{…}`, in `.go` and `.vuka` | v1.0.0 |
| `queries` | queries made for `orm.Query`: `vuka.Or` of fields → `orm.Or`, package `fn` → the orm's (`fn.Lower` → `orm.Lower`, `fn.In` → `InQuery`), a `First` read as an Option → `orm.Optional(….First(ctx))`, `Post.Objects.Manager()` → `Post.Objects` | v1.0.0 |

The other fixers (`static-names`, `orphans`, `attr-of`, `ui`) are unchanged;
`vuka fix -n` lists what any of them would do.

What `vuka fix` can't do, and what to do instead:

- **`Post.Objects` is the model's manager.** In `.vuka`, a model's
  `Objects` was an `orm.Query`, whose terminals returned `Result`s; it is
  now `orm.Objects[Post]()`, whose terminals return `(T, error)`. With `?`
  nothing changes. Code calling a `Result`'s methods on a terminal uses `?`
  or the value and error instead. `Update` takes an `orm.Set`, and
  `GetOrCreate` / `UpdateOrCreate` take a lookup and defaults:

  ```diff
  - Post.Objects.Filter(Post.ID.Eq(id)).Update(ctx, orm.Assign(Post.Views, n))?
  + Post.Objects.Filter(Post.ID.Eq(id)).Update(ctx, orm.Set{Post.Views.Name(): n})?
  - out := Session.Objects.UpdateOrCreate(ctx, s, Session.Key)?
  + s, created, err := Session.Objects.UpdateOrCreate(ctx, orm.Q{Session.Key.Name(): key}, orm.Defaults{…})
  ```

- **Go code naming an overload by its generated name** uses the new one:
  `area__Circle` is `areaCircle`. `@export` names are unchanged.
- **Go code that called `ui.Bind(init, expr, deps…)`** (a state binding,
  what older commands generated) calls `ui.BindExpr`; `ui.Bind` is the
  attribute `ui.Bind{Debounce: …}`.
- **Committed generated code** (`vuka gen -inplace`) is regenerated with
  `vuka gen -inplace`; forms older commands wrote (`vuka.TypeWith`,
  `vuka.FuncDecl`, `vuka.NewCall`, `vuka.RefOf` with index paths,
  `Base_Objects`) are deprecated or gone.

Deprecated, and still working through v1: the struct tags and named hooks
above (`vuka vet` warns, W0302 and W0303), and, marked `// Deprecated:` with
their replacement, the kit's `XProps` types (aliases of the structs),
`orm.Query` and package `fn`, `Meta()` and `SetFieldOptions` on models,
`vuka.TypeWith`, `TypeOf`, `FuncDecl`, `MethodDecl`, `DeclareWith`,
`DecorateType`, `vuka.Func` and `NewCall`, and `RefOf` with index paths.

### ORM v1.0.0

orm v1.0.0 is released with the set, requires vuka and web v1.0.0, and
makes the same promise. Since v0.9.1:

- **`Schema` method and typed fields**, above: `func (p *Post)
  Schema(s *orm.Schema)`, `PostFields` beside the model, `PostPath` through
  relations. `Model[T]` is what a Go model embeds (`orm.Base` in `.vuka` is
  written `orm.Model[T]`); `orm.Objects[T]()` is its manager and
  `orm.For[T]()` registers it.
- **The names-set struct is `orm.DBSchema`** (`orm.Of[T](orm.DBSchema{…})`,
  `DBSchema.Verify`); `vuka fix db-schema` renames it.
- **Terminals return `(T, error)`** — `All`, `First`, `Get`, `Count`,
  `Exists`, `Update`, `Delete`; `orm.Optional(qs.First(ctx))` turns
  `ErrNotFound` into `None`. `Update` takes `orm.Set{column: value}`;
  `GetOrCreate` and `UpdateOrCreate` take `orm.Q` and `orm.Defaults` and
  return the row and whether it was created.
- **`NumberField` and arithmetic**: `orm.Set{"views":
  PostFields.Views.Add(1)}`, in conditions too; `SaveFields` takes typed
  fields, Go names or columns.
- **`orm.Transaction(ctx, fn, opts…)`**, also as an around decorator in
  `.vuka` (`@orm.Transaction`, `@orm.TxOn("crm")`).
- **Reflection fallback**: a model without generated scanners works, by
  reflection (`ORM_REFLECT=1` runs the suite that way); `orm generate
  --missing` reports what is out of date, for a build tool's overlay.
- Option fields get generated scanners, typed fields work in `orm.Lower`
  and `Q` values, and `ModelConfig.Fields` skips fields per model at run
  time. `orm.Query` and package `fn` are deprecated.

New apps from `vuka new` require orm v1.0.0. The orm needs Go 1.27.

## v0.50.0 — 2026-10-19

The first set: vuka, ui, web and cmd/vuka at v0.50.0. Runtime unchanged,
v0.16.0 (the vuka module v0.50.0 satisfies it). New apps use orm v0.9.1.

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@v0.50.0
vuka upgrade v0.50.0
go get github.com/vuka-lang/orm@v0.9.1
```

### One version for the toolchain

- **A release is a set.** vuka, ui, web and the `vuka` command are tagged
  together at v0.50.0. An app requires the three libraries at one version
  and uses the command of that version; `vuka new` writes a go.mod pinned to
  its own set. The ORM keeps versions of its own.
- **`vuka version`** prints the command's set and, inside an app, the
  versions its go.mod requires, warning when they aren't one set (after a
  `go get` of one module alone, say).
- **`vuka upgrade [version]`** moves an app to a set, the latest by default:
  `go get` of vuka, ui and web (never `go mod tidy`), the `vuka fix`
  rewrites registered for the releases in between, then `vuka vet ./...`
  and `vuka test ./...`, with a summary. `-n` only says what it would do.
  The go.mod and go.sum it started from are kept in `.vuka/upgrade/`; to
  undo an upgrade, move them back.
- **A release train and a compatibility promise.** Fixes ship as a weekly
  patch release, security fixes at once, features in minor releases with
  upgrade notes; breaking changes are deprecated first and come with a
  `vuka fix` rewrite. See [Versions and compatibility](/project/versions).

### Diagnostics with codes

- **Every error has a code.** Errors of the compiler, `vuka vet`, the editor
  and the libraries' checks at boot carry a stable code (`E0203`; `W0301`
  for a warning), a place, a code frame on the terminal (coloured on a TTY,
  plain under `NO_COLOR`) and a `help:` line with the fix, with a
  did-you-mean where one fits.
- **`vuka explain <code>`** prints the code's page of the
  [catalogue](/errors/): what it means, and an example before and after.
- **`-diag=json`** prints diagnostics as JSON, for scripts and tools.
- **The editor** shows each diagnostic's code, linked to its page, and its
  help.
- **More is caught before boot.** `vuka vet` and the editor now report from
  the source what used to fail only when the app started: an island
  component that isn't found or is ambiguous (E0310, E0311), two islands of
  one name (E0312), bad `@ui.Island` arguments (E0320), and on pages backed
  by a component a bad `bind:`, `defer:` or `validate:` tag (E0322, E0323,
  E0328), a deferred group without its loader (E0324), a loader of the
  wrong shape (E0325) and two handles of one name (E0326); W0301 warns about
  a live page routed with `@web.Get`. Toolchain problems are coded too:
  Node, packages, the Vite plugin and TypeScript 7 (E0602–E0608).
- **Boot errors of ui and web are diagnostics**, placed at the declaration
  they are about — a route declared twice names both declarations (E0401).
  In Go, `diag.Of(err)` gives the code.

What to do: a test matching an error's whole text may need the new form
(`error[E0401]: GET /pets is declared twice …`). Match the code instead.

### Fixes

- **`vuka run -watch` on Linux** no longer restarts the app on every save.
  It compared whole binaries there, which differ on any change; it now reads
  the Go build ID, so a save that leaves the program as it was (a comment, a
  test file) keeps the app running, as on macOS.
- **Live pages: a broadcast right after joining is no longer lost.** A
  page's topics are now subscribed before its join's (or event's, or
  navigation's) reply is sent, so a broadcast the client causes as soon as
  it has the reply reaches it.
- **Dev assets:** generated asset files are written through a rename, so a
  page loading while an island is rebuilt no longer reads half a file or the
  previous manifest.
- **The compiler and `vuka fmt`** no longer fail on the inputs fuzzing found:
  a file with a decorator or an enum's import and no package clause, a
  truncated or empty `match` case, an empty receiver list, a positional
  struct pattern, a positional `ui.Class` literal, an empty composed
  decorator. The formatter runs to a fixed point, so formatting a formatted
  file changes nothing.

### Island ids changed

An island host's `data-vk-s` is now the island's place among its file's
components instead of a byte offset into the file: `pets.vuka#865` becomes
`pets.vuka#3`. Reformatting a file no longer changes it. Apps don't read
it, but a test or script that does — a selector like
`[data-vk-s="pets.vuka#865"]`, a snapshot or golden of rendered HTML — needs
the new value: run the page and copy it, or select the island another way
(its component's own markup, an `id` or a `data-testid` you give it).

### ORM v0.9.1

Go 1.27.2 writes compiled packages in a format the ORM's tools (orm v0.9.0
and older) can't read. They then saw no models: `go tool orm
makemigrations` planned to drop every model, and pages using the ORM
returned 500. orm v0.9.1 reads the new format, and its tools now stop with
an error naming the cause when they can't read a package, instead of seeing
no models. Apps building with Go 1.27.2 or newer should move to it:

```sh
go get github.com/vuka-lang/orm@v0.9.1
```

New apps from `vuka new` require it.

### Tests

The libraries' and the command's suites run on every push: vet, race
tests, the end-to-end suites with Vite and Chrome, and the fuzzers. A test
whose tool (Node, Chrome, Tailwind) is missing fails under CI instead of
skipping.

## cmd/vuka v0.45.3, web v0.36.3 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use web v0.36.3.

- **Moving between pages backed by Vue components with popovers, modals or
  selects no longer crashes the next island.** A navigation morphs the body
  into the new page's, and removed what a component had teleported to the
  body — Nuxt UI's (reka-ui's) portals and their empty anchors, which every
  row's popover keeps there even closed. Unmounting the old app then walked a
  teleported fragment whose anchors were gone and failed, the failure was
  swallowed, and the next island's mount on that host patched against the
  old app's tree: "There is already an app instance mounted on the host
  container", then `can't access property "nextSibling", e is null`. The
  morph now leaves the nodes the page's scripts put in the body where they
  are (web), and the Vue adapter forgets an app whose unmount fails, logging
  why, so the next mount starts clean.

## cmd/vuka v0.45.2, web v0.36.2 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use web v0.36.2.

- **Lazy components in Vite islands load.** An island built with Vite is
  built with a relative base, so a dynamic import — a
  `defineAsyncComponent(() => import('./Modal.vue'))`, a UI library's
  lazily loaded component — fetches its chunks and stylesheets beside the
  chunk importing it, under `/_vuka/i/`. Before, Vite's preload helper asked
  for them at the site's root (`/chunk.*.js`), where the app answers 404,
  and the lazy component never rendered. A `base` in `vite.config` no longer
  matters to the island build; drop one set to work around this.
- **One island per host.** Moving between pages backed by components, and
  a rebuild under `vuka run` while a page was open, could mount a second app
  on an island's host (Vue: "There is already an app instance mounted on the
  host container", then a `nextSibling` error): a host whose island wasn't
  built yet was remembered after another island mounted on it, and the next
  build mounted that island again. The loader now unmounts what a host has
  before mounting on it, and forgets such a host once an island mounts. The
  Vue adapter unmounts an app left on its host, too.
- **An island that fails to load says so.** A module that doesn't load, or
  a mount that throws, marks the host `data-vk-error="failed"` and, under
  `vuka run`, shows why on the page; a build bringing another module for it
  mounts it.

## cmd/vuka v0.45.1 — 2026-10-16

Runtime unchanged, v0.16.0.

- **`vuka run` rebuilds the islands for every file their build reads.** A
  Vite (or Node) island build is redone when anything under the frontend
  root changes — `vite.config`, the `app.css` entry, a shared component an
  island imports, an island in a subfolder, `_setup`, `package.json`, the
  lockfile — and when a file under a directory `app.css` names with
  `@source` does; before, only the island's own file was watched.
  `node_modules`, `dist`, hidden paths, `.d.ts` files (vuka's and those Vite
  plugins write) and Go sources are left out, so a build doesn't start
  another. A vanilla island is rebuilt when a file it imports changes.
- **The island cache keeps up.** A build is cached under what it saw before
  it ran, so an edit made while Vite builds is built next time, and a
  component outside the frontend root is keyed with its directory.

## cmd/vuka v0.45.0, web v0.36.0, ui v0.30.0 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.30.0 and web v0.36.0.

- **Deferred fields.** A field of a page backed by a component tagged
  `defer:""` isn't in the first render: its group's loader,
  `Load<Group>(ctx) error`, runs after it and the field reaches the
  component as an update — a slow count or a list no longer holds up the
  page, with no code in the component. `defer:"lists"` groups fields under
  one loader (`LoadLists`); a bare `defer:""` is the field's own group.
  On a live page the groups load concurrently once the page joins, each
  sent as it finishes, their context ending with the page; on a stateless
  page the component posts one signed, gated load once it mounts. A loader
  runs on a copy of the page and only its group's fields are kept.
  `live.stats.value` is `undefined` and `live.stats.pending` true until
  then; a failed loader is `live.stats.error`, the page carrying on.
  `vuka-live.d.ts` types the field optional and its handle
  `LiveDeferred<T>`; loaders aren't on `live`, and hover on one names its
  fields. [Deferred fields](/web/islands#deferred-fields).
- **`Changed(ctx, changed ui.Changed)`** says which bound fields an update
  set: `changed.Has("status")` (wire name, handle or Go name) and
  `changed.Names()` (wire names, sorted). `Changed()` and `Changed(ctx)`
  still work. [Which fields changed](/web/islands#which-fields-changed).

## cmd/vuka v0.44.0, web v0.35.0, ui v0.29.0, vuka v0.32.0 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.29.0 and web v0.35.0; the
command requires vuka v0.32.0.

- **`@web.Live` on a page backed by a component.** An `@ui.Island` struct
  embedding `ui.Live` is routed with `@web.Live("/path")`, its options
  (`web.Layout`, the type's gates) as for any live page. `@web.Get` stays
  the stateless form; `@web.Get` with `ui.Live` still serves a live page,
  and `vuka vet` and the editor suggest `@web.Live` for it.
  [Pages backed by a component](/web/islands#pages-backed-by-a-component).
- **Layouts in the decorator.** `@ui.Island(ui.Layout(layouts.Page("Orders")))`
  is the layout of the page the type backs: a `func(ui.Node) ui.Node`, or
  a `func(ctx, ui.Node) ui.Node` reading the request (the user, the path,
  `ui.PageTitle(ctx)`); several nest, the first outermost. It wins over the
  route's `web.Layout`, then `web.LiveLayout`, then a bare document, and
  applies only where the type is routed as a page. A `Title() string`
  method titles the page: the bare document's `<title>`, `ui.PageTitle`,
  and a document without one. Pages sharing a layout keep its elements
  across live navigation. [Layouts](/web/islands#layouts).
- **`ui.Strategy("load")`** sets an island's default strategy:
  `@ui.Island(ui.Strategy("load"))` keeps the inferred path. ui v0.29.0's
  `Island` takes `...any`: a path, a strategy, then options.
- **`vuka add page --island`** writes `@web.Live` for a live page (`@web.Get`
  with `--stateless`) and `@ui.Island(ui.Layout(layouts.Page("Title")))`
  when the app has `layouts/`; `--layout expr` names another layout, `--layout
  none` leaves it out.
- vuka v0.32.0: the compiler finds the type an `@ui.Island` decorates past
  any argument expression — nested calls, strings with parentheses, over
  several lines — so a live island page with options still gets its
  Render. The vuka command reads the literal path and strategy and skips
  the other options; hover on a decorator with `ui.Layout` says where the
  layout applies.

## cmd/vuka v0.43.0, web v0.34.0, ui v0.28.2 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.28.2 and web v0.34.0.

- **Islands move the page.** `live.navigate(href, opts?)` and
  `live.patch(href, opts?)` on `useLive()`, and `navigate` / `patch` on
  `useVuka()` in any island, do what a `data-vk-link` click does: over the
  live connection when the page is joined (gates, a patch's `Update`, the
  address, scroll, focus, `vk:navigate`), else a page load. `{ replace:
  true }` replaces the history entry. The promise resolves with `{url,
  patch}` once applied, and rejects for a URL that isn't `http` or `https`
  or when another move takes its place. `window.vuka.navigate(href,
  {patch, replace})` is the same call for any script.
  [From an island](/web/navigation#from-an-island).
- **`<VukaLink>`** in `vuka:live` for Vue, React and Svelte: an `<a href>`
  marked `data-vk-link` (`patch`, `replace`), with `activeClass` (default
  `active`, `exact` for the same path) and `aria-current="page"`, typed in
  `vuka-live.d.ts` for the frameworks the islands use. A link with
  `data-vk-replace` replaces the history entry.
- web v0.34.0: a navigation that morphs an island's host into another
  island's (another name, or another live instance) unmounts the old one
  and mounts the new one; before, the old component stayed.
- ui v0.28.2: a patch of a page backed by its island runs its `Update`
  taking `web.URLParams`; before, the page stayed as it was.

## cmd/vuka v0.42.1, web v0.33.0, ui v0.28.1 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.28.1 and web v0.33.0.

- **Breaking: a route's bound struct is validated with the islands' rules.**
  After a handler's struct parameter is bound — JSON body, form, multipart,
  query string or path segments — its `validate:` tags are checked, nested
  structs and slices of them included: `required`, `omitempty`, `min`,
  `max`, `len`, `oneof`, `email`. Before, only `required` was checked and
  other rules were ignored. A failure answers **422** (it was 400 `name is
  required`) with every failing field's message:
  `{"error": "invalid: email: required", "fields": {"email": "required"},
  "status": 422}` — keys are the field's path by json names for a JSON
  body (`address.city`, `items.1.city`), by form names otherwise; an HTML
  route answers its 422 error page. Clients that matched 400 or the `"x is
  required"` text should read status 422 and `fields`. A rule web didn't
  check before (a typo, `min=x`) now fails `Build` naming the route, and a
  rule it ignored now applies. The error is a `web.Invalid`, so
  `web.OnError` reads its fields with `errors.As`. Any JSON error from a
  `web.Invalid` now carries `fields` too. [Validation](/web/parameters#validation).
- ui v0.28.1: `ui.ValidateTags(v, "form", "json")` validates naming fields
  by the first tag they carry.

## cmd/vuka v0.42.0, web v0.32.0, ui v0.28.0 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.28.0 and web v0.32.0.

- **Pages backed by a component check `validate:` tags.** A `bind:` field's
  tag (`` Q string `bind:"q" validate:"max=50"` ``) is checked when the
  component sets it, every field of one set before any is applied, and a
  method's struct arguments are checked, with the structs they hold at any
  depth, before it runs. A failure applies nothing, runs neither `Changed`
  nor the method, and answers 422 with each field's message —
  `{"status": 422, "message": "invalid: q: at most 50 characters", "fields":
  {"q": "at most 50 characters"}}`; a method argument's fields by their path
  inside it (`years`, `level.code`, `items.0.code`) — live and stateless
  alike. Rules: `required`, `omitempty`, `min`, `max`, `len`, `oneof`,
  `email`; another rule, or a bad value, stops the app at start (ui
  v0.28.0's `ui.Validate` and `ui.CheckValidate`).
  [Islands](/web/islands#validation).
- **`live.q.error`.** A bound field's handle has the server's message for
  it, or `null`; what was typed stays in the input, shown with its error,
  until the field is set again or a method succeeds. A failed call's
  promise rejects with an `Error` carrying `status` and `fields`.
  `vuka-live.d.ts` types it: `LiveBound<V>` has `readonly error: string |
  null`.

## cmd/vuka v0.41.0, ui v0.27.0, vuka v0.31.0 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.27.0; the command requires
vuka v0.31.0.

- **`@ui.Island` finds its component.** Written bare, `@ui.Island` on
  `Orders` is `islands/Orders.<ext>` under the frontend root — in the
  module's folder for a package under `modules/<m>/`
  (`web/islands/sales/Orders.vue`) — the extension whichever of `.vue`,
  `.tsx`, `.jsx`, `.svelte`, `.ts`, `.js` exists. None, or two, is an error
  naming them. `@ui.Island("", "load")` keeps the inference and sets the
  default strategy; `@ui.Island("charts/Revenue.vue")` works as before.
  [Islands](/web/islands#declare-the-props-in-go). ui v0.27.0's `Island`
  takes its arguments as `...string`.
- **Generated code infers.** `vuka add page --island`, and `vuka new`'s
  sample island, write a bare `@ui.Island`; with `--dir` elsewhere the path
  is written.
- **The editor knows islands.** Hover on `@ui.Island` shows the component
  file (inferred or written, relative to the module root) and its
  framework, go to definition opens it, and a missing or ambiguous file is a
  diagnostic on the decorator without a build. Hover on a page's `Mount` or
  `Changed` says it is a hook the server runs, not callable from `live`.
- **Fixed: a method's parameter types in `vuka-live.d.ts`.** A type used
  only by a page's method (`Chasing(ctx, name string, data Data)`) was named
  but never declared, so `live.chasing(…)` didn't type-check. Every struct a
  field or a parameter reaches is declared now, at any depth: another
  package of the module's (`shared.Level`), in slices, maps and pointers,
  recursive ones by name, embedded structs' fields promoted as
  `encoding/json` does (json names, `omitempty` and pointers optional), and
  generic ones as generic interfaces (`Page<Tag>`).

## cmd/vuka v0.40.1, web v0.31.2 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use web v0.31.2.

- **A page whose component has fields but no methods has `live`.** A page
  backed by a component with `bind:` and read-only fields and only `Mount`
  and `Changed` gave the component `useLive()` as `null`, so reading
  `live.title` failed in the browser. Every page backed by a component now
  gets its handles, live and stateless alike (web v0.31.2; no change to
  your code or to ui).

- **An island build that fails shows in the browser.** Under `vuka run`
  (and `-watch`) a failed build of the islands — a Vite build of a `.vue`
  component that doesn't compile, say — used to reach the terminal only,
  while the page went on with the last good bundle. The
  [development toolbar](/web/toolbar#while-vuka-run-watch-rebuilds) now
  says `island build failed` (`asset build failed` for styles and scripts)
  with the build's error in its drawer, the end of what Vite said
  included, colours stripped; it clears when a later build succeeds.
- **An island the bundle doesn't have says so in its place.** In
  development, `<Levels … />` whose component isn't in the last good bundle
  renders a box — "Island Levels isn't built", and that the island build
  failed when it did — instead of nothing, and mounts once a build has it,
  without a reload. In production nothing changes: no box, and `vuka build`
  stops on an island that doesn't build.
- The last good bundle keeps serving meanwhile: the other islands work.

## cmd/vuka v0.40.0, web v0.31.0, ui v0.26.0 — 2026-10-16

Runtime unchanged, v0.16.0. New apps use ui v0.26.0, web v0.31.0.

- **A page's component reads and sets its fields through `live`.** In a
  page backed by a component, every exported field is a handle on
  `useLive()`: `live.rows.value` (or `get()`) reads it and follows the
  server. A field tagged `bind:"q"` is one the component sets —
  `live.q.set(v)`, or `v-model="live.q.value"` — and the value is the
  browser's at once while the set waits for a pause in typing (300ms;
  `bind:"q,debounce=500"`, `set(v, { now: true })`). Sets of several fields
  within one pause go as one update; the server sets the fields, runs the
  type's `Changed` and answers with the props — over the connection on a
  live page, as the signed round trip on a stateless one. A field without
  `bind:` is read only: no `set` in the types, and the server refuses a set
  of it (403). See [Pages backed by a component](/web/islands#pages-backed-by-a-component).
- **Each method has its own `pending` and `error`** (`live.cancel.pending`),
  beside the page's `live.pending` and `live.error`. Calls and sets go one at
  a time, in order; a method sends the sets waiting first.
- A method and a field of one name on `live`, or a `bind:` tag that doesn't
  fit, stop the app at start with what to rename. `Changed` isn't callable.
- Components reading their props with `defineProps<T>()` (React: props,
  Svelte: `$props()`) and calling `live.method(…)` work as before.
- `vuka-live.d.ts` types the handles (`LiveBound<V>`, `LiveField<V>`) and
  each method's state.
- `vuka add page --island` writes a page whose search field is `bind:` with
  a `Changed`, and a component setting it through `live` (`v-model="live.q.value"`,
  React `value`/`onChange`, Svelte `bind:value`). With `--module m` the
  component goes in `web/islands/m/`, as `vuka add island --module m` does.

## cmd/vuka v0.39.3 — 2026-10-16

Runtime unchanged, v0.16.0.

- **A Vue page backed by a component builds when TypeScript 7 is about.**
  Vue's compiler reads `defineProps<T>()` of a type imported from
  `vuka:live` through TypeScript's JavaScript API, which TypeScript 7
  doesn't have; a UI library depending on `typescript` could hoist 7 into
  the frontend, and the island build failed, leaving the page blank.
  `vuka add island` / `page --island` now pin `typescript` `~6.0.3` beside
  Vue when the frontend has none, and the build's error says what to run.
- **Editors find `vuka:live`.** A frontend without a `tsconfig.json` gets
  one (mapping `vuka:live` to `vuka-live.d.ts`) from `vuka add island` and
  from the build that writes `vuka-live.d.ts`.
- **`@ui.Island` paths from the frontend root.** A path that doesn't start
  with `.` and isn't beside the declaring file is found from the frontend
  root `web.FrontendRoot` names: `@ui.Island("islands/Orders.vue")` from any
  package, instead of `"../../web/islands/Orders.vue"`. A path beside the
  file, or starting with `./` or `../`, reads as before.
- **`vuka add page` makes a live page by default** (`@web.Live`, or
  `ui.Live` in a page backed by a component); `--stateless` makes the
  `@web.Get` one. A path with `{parameters}` stays stateless unless `--live`.
- The Vue component `vuka add page --island` writes binds its search box
  with `v-model` and calls the method from a `watch`.
- `vuka add island` picks the framework the frontend already uses; the help
  now says the flags are for adding one in another.

## cmd/vuka v0.39.2 — 2026-10-16

Runtime unchanged, v0.16.0.

- **`vuka add island` writes the `.vuka` side too.** Besides the component
  under `web/islands/`, it writes a component placing it —
  `components/chart.vuka` with `Chart(ChartProps)` — so a page writes
  `<components.Chart Title="Sales" Count={3} />` instead of importing the
  file itself. `--module m` puts both in the module: `modules/m/chart.vuka`
  and `web/islands/m/Chart.vue`.
- **`--module` for services and entities, and the app's own folders.**
  `vuka add service Mailer --module notes` and `vuka add entity Comment
  --module posts` name the module as the other kinds do (`notes/Mailer`
  still works); without a module they go in `services/` and `entities/`.
- **`vuka add entity --unmanaged`** maps an existing table:
  `@orm.Meta{Table: "…", Unmanaged: true}`, no timestamps, test or
  migration, and the app needs no `migrations/`. (In zsh, quote an optional
  field: `'code:string?'`.)

## web v0.30.1 — 2026-10-16

Security fix. Runtime unchanged, v0.16.0; ui unchanged, v0.25.0. The vuka
command is `cmd/vuka/v0.39.1`. New apps use web v0.30.1.

- **A stateless page's signed props are the visitor's.** A page backed by a
  component without `ui.Live` sends its props to the browser signed, and
  each call carries them back. The signature bound the page's path and an
  expiry but not who the page was rendered for, so anyone holding a page's
  props could replay them — as another user, or signed out — until they
  expired (calls still passed the page's gates). The signature now also
  binds the visitor's sign-in (with `web/auth`: the user and the session,
  token or JWT they signed in with; no one, for an anonymous visitor) and
  the page's type. Props replayed by another visitor, anonymously, after a
  sign-out and in again, or at another page answer like a forged signature
  (403). Pages already open when the app is upgraded are refused once and
  work again after a reload. No change to apps or components:
  `go get github.com/vuka-lang/web@v0.30.1`.
- `web.LiveIdentity` keys a sign-in, not only a user: live adoption, too,
  only adopts a page for the sign-in that rendered it.

## v0.30.0 — 2026-10-16

Runtime: unchanged, v0.16.0. The vuka command is
`cmd/vuka/v0.39.0`. New apps use ui v0.25.0, web v0.30.0.

- **Import a component as an island.** `import Week "./web/islands/Week.vue"`
  in a `.vuka` file makes `<Week …/>` a tag: the compiler reads the props
  from the component's TypeScript — Vue's `defineProps`/`defineModel`/
  `defineEmits`, a React component's typed props, Svelte's `$props()`, a
  vanilla `mount(el, props)` — and type-checks every attribute against them,
  with no Go type to write and no Node to run. `<ui.Island>` is needed only
  for a fallback. React (`.tsx`/`.jsx`) and Svelte islands build beside Vue.
  `vuka lsp` completes the props.
- **Island props both ways.** On a live page every prop follows the server
  (`once` keeps the first value). A function prop takes a method of the page
  — the component calls it, the method runs on the server — or JS commands. A
  `ui.State` passed as a prop is shared: the component writes it back
  through `defineModel`, a React `setX` prop or a Svelte `$bindable`, and
  every binding on the page follows.
- **Pages backed by a component.** `@web.Get` on an `@ui.Island` type serves
  the component as the page: exported fields are its props (`url:"q"` fields
  kept in the address), exported methods what it calls with
  `useLive<T>()` from `vuka:live`, typed from the Go struct in
  `vuka-live.d.ts`. Embedding `ui.Live` makes it a live page; without it a
  call is a POST carrying the page's signed props back, so the page needs no
  connection. Errors arrive as `live.error` (`web.Invalid` for fields), a
  `web.Redirect` navigates, and the type's gates (`@auth.Required`) hold for
  calls as for the page. Every island knows the signed-in visitor
  (`useVuka()`). See [Pages backed by a component](/web/islands#pages-backed-by-a-component).
- **Client state.** A write (`open.Set(false)`), a `ui.Node` or an attribute
  list built from states and passed as a prop or child is passed as it is,
  not read as a state. `ui.BindAttr(name, s)` binds a state into a Go
  attribute list. An `auto` popover the browser closes (Escape, a click
  outside) writes its `when` state back, and `<ui.Show>` without a transition hides
  at once, so swapped panels never show together.
- **`vuka add` makes the pieces of an app:** `service`, `component`,
  `island` (Vue, React or Svelte, with its packages installed), `page`
  (`--live`, `--island`) and `entity` (an orm model with its migration).
  `--dry-run` lists what it would write; `--force` overwrites. See
  [add](/tools/cli#add).
- **Long steps say they are going.** A package install, the Vite island
  build and `vuka new`'s module fetch show a spinner on a terminal and a
  heartbeat line elsewhere; a failure prints the tool's last lines.
- **Faster rebuilds.** What a package was transpiled to, and the API it was
  checked against, is kept on disk (the user's cache directory,
  `vuka/transpile`): `vuka build`, `run`, `test`, `vet` and `gen` transpile
  only the packages whose inputs changed, and `run -watch` starts from that.
  The Vite island build is skipped when nothing it reads changed.
  `VUKA_CACHE=off` turns both off. `vuka run` leaves the files
  `web.Frontend` serves out of the binary (they are read from disk in
  development), so the linker doesn't copy them on every rebuild; `-embed` or
  `VUKA_EMBED_STUB=off` builds them in.

**Upgrading.** `web.Get`, `Post`, `Put`, `Patch`, `Delete` and `Handle` now
return a `vuka.Bundle` (made with the new `vuka.Dual`: a route on a
function, a page on a type) rather than a `func(*vuka.Decl)`. `@web.Get(…)`
in a `.vuka` file is unchanged. Go code that used the result as a function
changes:

<!-- go:context
var d *vuka.Decl
-->

<!-- go:fragment -->
```go
// before: web.Get("/pets")(d)
vuka.DeclareWith(web.Get("/pets"), d) // now
```

```go
// before: func ApiRoute(p string) func(*vuka.Decl) { return web.Get(p) }
func ApiRoute(p string) vuka.Bundle { return web.Get(p) } // now
```

A composed decorator (`decorator ApiRoute(p string) = @web.Get(p) …`)
needs no change. web v0.30.0 requires vuka v0.30.0:
`go get github.com/vuka-lang/web@v0.30.0` brings it along.

## v0.29.0 — 2026-10-15

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.38.1`. New
apps use ui v0.24.0, web v0.29.0.

- **Client state.** `ui.State(v)` declares a value the browser keeps — a
  menu's `open`, the picked tab, a filter. Commands change it with no round
  trip (`open.Toggle()`, `tab.Set("env")`, `n.Inc()`), and the attributes,
  text and classes that read it follow, written as plain Go expressions:
  `<ul hidden={!open}>`. `ui.Derived(expr)` names a value built from
  states, `ui.Class` toggles classes, `<ui.Show when={open}>` shows and hides
  with transitions, and a struct state's fields are read one by one. The
  server renders the initial values, so the HTML is right before any script
  runs; an expression that can't run in the browser is a compile error
  naming what to compute on the server. See [Client state](/ui/state).
- **`vuka lsp`:** type-aware highlighting (semantic tokens) in
  cmd/vuka v0.37.0, and no flicker of stale diagnostics while typing in
  v0.36.0. cmd/vuka v0.38.1 reports its own version correctly.

## v0.27.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.32.0`. New apps
use ui v0.21.0, web v0.24.0.

- **UI islands (Phase 3 — the live bridge).** Islands are now reactive both
  ways. Server → island: a `*ui.Assign[T]` island prop serializes its value, and
  a live re-render pushes the changed value to the mounted component's
  `update()` with no remount (the live-view morph updates an island host's
  attributes but leaves its client-owned DOM). Island → server: `on<Emit>`
  attributes on the tag bind the component's emits to live-view handlers —
  `<DataTable rows={live(d.rows)} onRowClick={d.RowClick}/>` lowers the handler
  to a registered id (like an element `onClick`, no `AllowPush`), and the client
  adapter forwards the Vue emit to it; the emit's argument arrives as JSON bound
  into the handler's struct parameter (`ui.Event.Data`). Verified end to end:
  an island emit runs a server handler that bumps a live prop, which updates the
  island in place.

## v0.28.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.34.0`. New apps
use ui v0.22.0, web v0.25.0.

- **Stable island `data-vk-s`.** A `<ui.Island>` now lowers to
  `ui.MountIsland("<site>", …)` with a stable per-placement id (the file + the
  wrapper's source offset), used as the host's `data-vk-s`. Previously the host
  got a per-render counter, so every island re-render churned `data-vk-s` in the
  live-view diff (spurious patch weight) and broke `ctx.ref` after an update
  (the captured scope no longer matched). The id is now identical across
  renders. Requires ui v0.22.0 (`MountIsland` gained the leading `site`
  parameter).

## cmd/vuka v0.35.0 — 2026-10-13

Runtime unchanged (vuka v0.28.0); a command-only release.

- **`vuka lsp`: auto-imports consolidate into one block.** Accepting a
  completion (or a decorator / static accessor) that needs an import now folds
  it into a single sorted `import ( … )` block — folding any existing lone
  `import "x"` lines in too — instead of appending another lone `import` line
  each time. (`addImports` now returns a set of edits rather than one insertion.)

## cmd/vuka v0.33.0 — 2026-10-13

Runtime unchanged (vuka v0.26.0); new apps use ui v0.21.0, web v0.25.0.

- **Vite island build (for Nuxt UI and other Vite-plugin UI libraries).** When
  a framework island's frontend root is a Vite project (has a `vite.config.*`),
  the vuka command builds the `.vue` islands by running the project's **Vite**
  (with its `[vue(), ui(), tailwindcss(), vuka()]` plugins) instead of the
  Node-esbuild path — so Nuxt UI's components auto-resolve and its Tailwind v4
  CSS is produced. vuka reads Vite's manifest, serves the island + adapter +
  shared chunks at `/_vuka/i/`, and injects the island CSS (`<link>` on first
  hydration). A frontend root without a `vite.config` keeps the esbuild path;
  vanilla `.ts` islands always use esbuild. Needs web v0.25.0 (its Vite plugin
  gains the island-build mode + island-CSS serving).
- **`vuka new --islands` now scaffolds a Vite + Nuxt UI frontend root** by
  default: `vite.config.mts` (`[vue(), ui(), tailwindcss(), vuka()]`), the dep
  set (vite, @vitejs/plugin-vue, @nuxt/ui, @tailwindcss/vite, tailwindcss, vue,
  vue-router), `app.css` (`@import "tailwindcss"; @import "@nuxt/ui";`), and a
  sample island using a Nuxt UI `<UButton>`.
- Dev (`vuka run`) rebuilds Vite islands on change (a full island build, not yet
  HMR off the Vite dev server — a follow-up).

## cmd/vuka v0.31.0 — 2026-10-13

Runtime unchanged (vuka v0.26.0); a command-only release.

- **`vuka new --islands`.** Scaffolds a sample Vue island wired end to end: a
  `web/` frontend root (`package.json` with esbuild + esbuild-plugin-vue3 + vue,
  a `web/dist/index.html` stub, `web/islands/Counter.vue`), an `@ui.Island` Go
  type, a live page placing `<ui.Island><Counter … client:visible/></ui.Island>`,
  and `web.Frontend(dist, "web/dist", web.FrontendRoot("web"))` in main. Offered
  for `--ui=live --frontend=none` (refused otherwise, with a clear message);
  default off, so every existing `vuka new` combination is byte-for-byte
  unchanged. After scaffolding, run `cd web && npm install` once; `vuka build`
  then bundles the island on every build.

## cmd/vuka v0.30.0 — 2026-10-13

Runtime unchanged (vuka v0.26.0); a command-only release.

- **UI islands (Phase 2 — Vue).** `@ui.Island("…/X.vue")` now builds: the vuka
  command compiles the SFC with Node esbuild + `esbuild-plugin-vue3` (one
  code-split build per app — the Vue adapter and every `.vue` island as entry
  points, with the Vue runtime deduped into a shared chunk), serves the chunks
  at `/_vuka/i/`, and records each island with its module + a Vue adapter chunk;
  the client loader mounts it with `createApp`, re-pushing changed props on a
  live re-render and unmounting when it leaves. The build deps (`vue`,
  `esbuild`, `esbuild-plugin-vue3`) come from the frontend root
  (`web.FrontendRoot`); a server-rendered app without a frontend adds a minimal
  `package.json` there. An optional `_setup.ts` (default export `(app)=>{}`)
  runs per island app. Vanilla islands are unchanged; React/Svelte await a later
  phase. Design: docs/design/islands.md.

## v0.26.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.29.0`. New apps
use ui v0.19.0, web v0.22.0.

- **UI islands (Phase 1 — vanilla).** Astro-style client islands: a
  server-rendered page stays zero-JS, and a component becomes an interactive
  island hydrated on a `client:*` strategy. A `<ui.Island>` wrapper around an
  island-component tag lowers to a `ui.MountIsland(...)` call — the body's
  attributes become the island's props, `client:load`/`idle`/`visible`/`media`/
  `only` the hydration strategy, its children the slot, and a nested
  `<ui.Template fallback>` the fallback shown until hydration. An island is a Go
  struct annotated `@ui.Island("islands/Counter.ts")` (Go owns the props); the
  vuka command bundles the component into a lazy chunk served at `/_vuka/i/`,
  records it in the asset manifest, and generates `islands.d.ts` from the struct
  into the frontend root (`web.FrontendRoot`). The client loader arms the
  strategy (IntersectionObserver / idle / media / load), lazy-imports the module
  on trigger, and mounts it; a live re-render re-pushes changed props. Phase 1 is
  vanilla `.ts`/`.js` only (a vanilla module exports `mount` itself); Vue/React/
  Svelte adapters, the `live()` prop bridge, and the opt-in SSR sidecar are later
  phases. Design: docs/design/islands.md.

## v0.25.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.28.0`.

- **Inferred type arguments for generic stateful components.** The type-argument
  inference that v0.24.0 brought to function components now covers **stateful**
  components — struct-type tags whose pointer implements the target's `Stateful`.
  `<Box Value={7} />` for `type Box[T any] struct{ ui.Live; Value T }` infers
  `T = int` from the attribute, where before it required `<Box[int] Value={7} />`.
  Each attribute binds to its exported field, and the field's declared type
  (`Value T`, `Items []T`, `At *T`, `Cols []Column[T]`, …) is unified structurally
  with the value's concrete type to pin the parameters, reusing the same unifier
  as the function-component path. Because Go can't infer a composite literal's
  type, the inferred arguments are written onto the generated literal
  (`&Box[int]{Value: 7}`). Explicit type arguments on the tag still work and take
  precedence; an under-constrained or conflicting tag still asks for them — now
  naming the conflicting parameter. Pure compile-time resolution; no runtime or
  JSX-contract change.

## v0.24.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.27.0`.

- **Inferred type arguments for generic JSX components.** A component whose
  single parameter is a generic props struct — `func Table[T any](p
  TableProps[T]) Node` — can now be used as a tag without writing its type
  arguments: `<Table Rows={rows} Cell={fn} />` infers `T` from the attribute
  values, where before it required `<Table[User] …/>`. Each attribute binds to
  its field, and the field's declared type (`Rows []T`, `Cell func(T) string`,
  `Cols []Column[T]`, `Rows []*T`, …) is unified structurally with the
  attribute value's concrete type to pin the parameters; multi-parameter
  components (`Pair[K, V]`) are inferred from several attributes. Explicit type
  arguments on the tag still work and take precedence. When no attribute
  constrains a parameter, or two attributes imply conflicting types, the tag
  still asks for explicit type arguments — now naming the conflict. Pure
  compile-time resolution; no runtime or JSX-contract change.

## v0.23.0 — 2026-10-13

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.26.0`.

- **Self-target JSX.** A package that itself declares `const VukaJSX = 1` — a
  JSX runtime such as a component kit — can now author JSX in its own `.vuka`
  files. Its JSX lowers to unqualified `El`/`F`/`Child`/… and same-package
  component tags (`<Badge/>`) resolve in-package, instead of requiring an
  imported target. A file that imports an external JSX target still uses it
  (external wins); nothing changes for apps that import a kit. No runtime or
  JSX-contract change.

## v0.22.0 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.25.0`.

- **Faster pages.** JSX lowers to a lazy frame (`ui.FT`) when the target
  declares it ([ui](https://github.com/vuka-lang/ui) v0.13.0 and up): the
  element tree moves into a build closure with the dynamics passed as
  arguments, so a server-rendered page writes its static HTML straight through
  and renders only the parts that change. A 50-row table page is about 2×
  faster with ~35% fewer allocations, and the HTML and the live render tree
  are byte-for-byte what the old lowering produced. Generated code built
  against an older ui still lowers the old way; nothing else changes.

## v0.21.0 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.24.0`.

- **[Enums](/features/enums).** `enum Status { Draft, Sent, Paid }` and
  `enum Role string { Admin = "admin", … }` declare value enums with
  `Status.Values()`, `Status.Parse`, `String`, `Valid`, JSON (`MarshalText` /
  `UnmarshalText`) and `database/sql` (`Scan` / `Value`) methods, so an enum
  field works in JSON and the ORM as it is. `enum Shape { Circle{Radius
  float64}; Empty }` declares variants that carry data. Variants are
  reached as `Status.Draft` and `Shape.Circle{Radius: 2}`, and plain Go uses
  `StatusDraft`, `ParseStatus` and `ShapeCircle`.
- `match` takes variants as patterns, `case Shape.Circle{Radius: r}:`, and a
  match over an enum that misses a variant is an error naming the missing
  ones. Patterns separated by commas may now be type tests:
  `case Shape.Circle{}, Shape.Rect{}:`.
- The editor completes an enum's variants, `Values` and `Parse` after
  `Status.`, with their doc comments on hover; `vuka fmt` lays enums out;
  the VS Code grammar highlights them.
- **Model packages link themselves.** A package whose struct types embed a
  type with a `vukaDeclares()` marker method is auto-linked like a declarer
  package; with orm v0.5.1, whose `Base` and `Model` have it, a model package
  nothing imports is still registered and migrated, with no blank import.
- **Fixed:** `Some` and `None` (and statics) in a field attribute's value are
  qualified and inferred like any other code: `@orm.Int{Min: Some(0)}`.
- **Fixed:** an attribute type's `vukaTargets` result may be an alias or a
  named type of the targets array, and its targets are checked.

## Libraries — 2026-10-12

- **[ui](https://github.com/vuka-lang/ui) v0.12.0 and
  [web](https://github.com/vuka-lang/web) v0.19.0:** server-rendered HTML
  pages are about 40% faster, with half the allocations, and the output
  byte-for-byte the same. An element or frame is built in one allocation,
  text and attribute values are escaped only when they hold `& ' < > "`, and a
  page with nothing live renders straight into a pooled buffer instead of
  through a live session. New API `ui.RenderPage`.
- **[orm](https://github.com/vuka-lang/orm) v0.5.1:** `orm.Base` marks the
  types embedding it as declaring a model, so Vuka links a model's package
  into the program and the ORM sees models no code imports (with the next
  vuka release, which recognises the marker).

## cmd/vuka v0.23.2 — 2026-10-12

Runtime: unchanged, v0.16.0.

- **Editor:** after a struct field, `@pkg.` completes that package's field
  attributes — the types and the functions returning one, with their docs —
  and a half-typed field attribute no longer breaks the file's other
  diagnostics and hover.
- New apps use [web](https://github.com/vuka-lang/web) v0.18.0, whose
  `auth.Config.UserCache` keeps a loaded user for a while, so a signed-in
  request reads only its session or token; `auth.Refresh(ctx, uid)` drops
  one after a change.
## v0.20.1 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.23.1`.

- **Fixed:** auto-linking no longer links a package under an `internal`
  directory into a program outside that directory's tree (Go refuses such
  an import, so a module with examples failed to build).
- With [web](https://github.com/vuka-lang/web) v0.17.0, a live page checks
  the sign-in when it mounts (join, navigate) and its events read no
  session; sign-outs through auth close the page at once, and a periodic
  check (`LiveCheckEvery`, 30s) catches revocations made outside auth.

## v0.20.0 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.23.0`.

- **Page and route packages link themselves.** Every package of the module
  that declares something — `@web.Live`, `@web.Get`, `@web.Cron`, or a
  composed decorator holding one — is linked into the program by
  `vuka run`, `build`, `test` and `vet`: no blank import to remember, and
  an error in such a package always fails the build. The command says what
  it linked (`● linking 3 declarer packages (…)`); `vuka gen -inplace`
  writes the same file (`vuka_link_gen.go`) for a plain `go build`.
  `//vuka:nolink` above a package clause opts that package out, or, on a
  main package, turns linking off for that program.

## v0.19.1 — 2026-10-12

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.22.2`.

- **`vuka explain` shows what the build compiles.** It now takes the file's
  generated Go from the same full transpile the build runs, so it no longer
  reports errors the build doesn't (a static reached through an embedded
  type from another package said "has no field"). A file that truly fails
  prints the error and exits non-zero.

## cmd/vuka v0.22.1 — 2026-10-12

The vuka command only. Completion after `entities.Letter.` now works in a file
that hasn't imported the type's package yet — picking a member also inserts
the import. Restart the language server after installing.

## cmd/vuka v0.22.0 — 2026-10-11

The vuka command only (vuka v0.19.0, runtime v0.16.0 unchanged). New apps use
ui v0.11.0 and web v0.16.0.

- **Completion knows statics.** `entities.Letter.` offers `Objects`, the
  fields and every static reached through embedding, with docs; hover on a
  static shows its declaration; the file keeps compiling while the member is
  half-typed.
- **Fixed:** picking a decorator from another package in a file with no
  import block inserted the import before the `package` line.

Restart the language server in your editor after installing.

## v0.19.0 — 2026-10-11

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.22.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.11.0 and [web](https://github.com/vuka-lang/web) v0.16.0.

- **The editor completes statics.** Typing `entities.Letter.` offers
  `Objects`, the model's fields and every static the type reaches through
  embedding, with their docs; hover on a static shows its declaration. A file
  keeps compiling while the member is still being typed.
- **web v0.16.0: `Mount` runs once per page view.** The socket join adopts
  the page the HTTP render mounted when it arrives in time (`web.LiveAdopt`,
  10s) and as the same signed-in user — so a `Mount`'s queries no longer run
  twice. Anything uncertain mounts fresh, exactly as before.

## v0.18.0 — 2026-10-11

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.21.0`.

- **A faster editor.** `vuka lsp` shows Vuka syntax errors as you type
  (about 1 ms, from the file being edited alone), type errors about 100 ms
  after you pause, and answers hover and completion without waiting for the
  whole module to rebuild: it keeps the transpile cache `vuka run -watch`
  uses and rebuilds the packages you edited first. Restart the language server
  in your editor after upgrading.

## cmd/vuka v0.20.0 — 2026-10-11 — security release

The vuka command only; the language and runtime are unchanged (v0.17.0, runtime
v0.16.0). New apps use [`github.com/vuka-lang/ui`](/ui/) v0.10.0,
[web](https://github.com/vuka-lang/web) v0.15.0 and
[orm](https://github.com/vuka-lang/orm) v0.4.0, which fix the issues a security
review found. Upgrade apps with `go get` of those versions, and build them with
Go 1.27.2 or newer.

What apps may notice:

- **Live pages.** A navigation keeps a page mounted only when it declares an
  `Update` taking `web.URLParams` — and that `Update` must repeat the checks
  `Mount` makes; any other page mounts again. Errors that aren't `*web.Error`
  read "Internal Server Error" outside development (logged in full). Live
  connections have limits: join within 10s, idle 30m, 10,000 in all, 100 per
  client address, 4 per page; checks run every 30s and before broadcasts.
- **JS commands.** `SetAttr`, `RemoveAttr` and `ToggleAttr` refuse event
  handler attributes and `srcdoc`; URL values and `Navigate`/`Patch` take only
  paths and http(s) URLs.
- **Auth.** Gated calls are strict by default (`auth.System(ctx)` for jobs and
  `main`; crons get it automatically; `LaxGates` opts out). Failed logins take
  at least 300ms. Throttling counts per account and folds spellings. Policies
  match `T` and `*T`. Live pages reload when a user's permissions change.
- **Requests.** Security headers by default; request bodies capped at 32 MiB;
  forwarded headers trusted only from `web.TrustedProxies`; static files never
  list directories or serve dotfiles; `.env` files can't set `VUKA_*`.
- **ORM.** A `ReadOnly` database is read-only on the server; pools are capped by
  default (20 open); `/healthz` shows states only outside development;
  `orm migrate` asks before unapplying.

## v0.17.0 — 2026-10-11

Runtime: unchanged, v0.16.0. The vuka command is `cmd/vuka/v0.19.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.9.0 and [web](https://github.com/vuka-lang/web) v0.14.0.

- **JS commands reach more elements, more safely.** `ui.Ref` and `ui.Sel` take
  several names; `ui.GetAttr(name, values…)` picks elements by attribute, and
  a literal value nothing in the component has is a compile error with a
  did-you-mean. Conditions combine with `And`, `Or`, `Not`, `ui.All`,
  `ui.Any` and `.Some()`; `ui.JS.When(cond, chain).Else(chain)` needs no
  `End`; `ui.JS.Busy()` marks a form or button busy and undoes it on a reply,
  a timeout or the back button. See [JS commands](/ui/js).
- **Fixed:** a decorated method with an unnamed receiver no longer fails to
  compile when a parameter is called `r`.
- web v0.14.0 also brings trailing-slash redirects, failed form posts that
  re-render the whole page, and `auth.Config.Hashers` for passwords from
  another system.

## cmd/vuka v0.18.1 — 2026-10-11

The vuka command only. The editor no longer reports `expected declaration` on a
method chain that goes on to the next line (`ui.JS.` then `If(…)` below it);
`vuka build` always accepted it.

## v0.16.0 — 2026-10-11

Runtime: **v0.16.0** — generated code registers attributes with it; run
`vuka fix runtime` in your apps after upgrading the vuka command. The vuka
command is `cmd/vuka/v0.18.0`. With [`github.com/vuka-lang/ui`](/ui/) v0.8.0 and
[web](https://github.com/vuka-lang/web) v0.13.0.

- **Attributes readable from a value.** `vuka.FuncOf(fn)` (a method value,
  method expression or function), `vuka.MethodAttrs(t, name)` and
  `vuka.TypeAttrs(t)` return the attributes written on a declaration, so a
  library can read `@auth.Requires(…)` where it dispatches. See [Reading
  attributes from a value](/features/attributes).
- **web's auth gates checked before a live handler runs** and at each gated
  call, with the identity carried by `ctx`; `auth.Config.StrictGates` refuses
  a gated call that has none. See [Authentication](/web/auth).
- **`web.IsLiveJoin(ctx)`** tells a live page's route it is running again for
  the socket's join, so side effects such as mail happen once. See [Live
  components](/web/live).

## cmd/vuka v0.17.0 — 2026-10-11

The vuka command only; the language and runtime are unchanged (v0.15.0, runtime v0.10.0).
New apps use [web](https://github.com/vuka-lang/web) v0.12.0 and [orm](https://github.com/vuka-lang/orm) v0.3.0.

- **`vuka new` builds a working app.** It asks for the UI (live pages with
  Tailwind, templ views, or an API), the database (SQLite by default, Postgres,
  MySQL, none), mail, sign-in and an optional React, Vue or Svelte frontend —
  or takes flags and `--yes`. The default app has a login page already
  connected to a users table and sessions in the database, an admin created
  from `.env` with a generated password, migrations run at start, a signed-in
  layout with the Vuka logo, shared components, and tests that pass. See
  [Getting started](/web/getting-started).
- **`vuka add module <name>`** adds a module — model, service, page, migration
  and test — to an app made by `vuka new`.

## cmd/vuka v0.16.0 — 2026-10-11

The vuka command only; the language and runtime are unchanged (v0.15.0, runtime v0.10.0).
With [web](https://github.com/vuka-lang/web) v0.11.0: [authentication](/web/auth),
[mail](/web/mail), [crons](/web/crons) and [embedded frontends](/web/frontend).

- **Frontend apps in `vuka run` and `vuka build`.** A `frontend/package.json`
  starts the project's own Vite beside the app (dependencies installed on
  first run), kept running through `-watch` rebuilds; you open the app's
  address and get hot module reloading. `vuka build` runs `vite build` before
  embedding. A prebuilt `dist` with no `package.json` is served as is.

## v0.15.0 — 2026-10-11

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.15.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.7.0 and [web](https://github.com/vuka-lang/web) v0.10.0.

- **Styles and scripts per page.** `web.Style(...)` and `web.Script(...)` on
  `@web.Live`, `web.Layout` and `web.New` (several files, globs, `web.CSS` and
  `web.JS` inline, `web.Global` unscoped). CSS is scoped to the component —
  a marker the compiler adds to its own markup, so it never travels in live
  diffs — with `:global`, `:deep`, `:scope`, scoped `@keyframes`, Tailwind's
  `@apply` inside, and a warning for a selector that matches nothing. Scripts
  are TypeScript or JavaScript, compiled by the vuka command with no Node:
  `mounted`/`updated`/`destroyed` per instance, `ctx.push("Method")`,
  `ctx.on`, `ctx.ref`, and `ui.JS.Call` reaching them. Only methods a
  component's scripts name with `ctx.push("…")` can be called by name; the
  compiler checks each name. See [Styles and scripts](/web/assets).
- **Faster `vuka run -watch`.** A save answers in about 0.8–1s on a small app,
  down from 1.6–1.8s: file events from the operating system instead of
  polling, transpiling only what changed (checked against a full transpile in
  the tests), no debug info in watch builds (`-debug` keeps it), and a save
  during a build cancels the stale build.

## v0.14.0 — 2026-10-11

Runtime: unchanged, v0.10.0 (`vuka.ObserveCalls` is new in the package; generated
code doesn't need it). The vuka command is `cmd/vuka/v0.14.0`. With
[web](https://github.com/vuka-lang/web) v0.9.0's [development toolbar](/web/toolbar).

- **`vuka.ObserveCalls`** lets a tool see each step of every decorated call —
  each decorator, then the function — on the call's goroutine. The toolbar's
  Timeline uses it to show `@timed` and `@logged` inside a request. With no
  observer it costs one atomic load per step.
- **`vuka run` names a session directory** (`VUKA_DEV_DIR`). Under `-watch`
  it holds the next build's state — building, failed with the compiler's
  output, or ok — so the toolbar shows `building… 1.2s` or `build failed` on
  the page while the last good build keeps serving.

## v0.13.0 — 2026-10-11

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.13.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.5.0 and [web](https://github.com/vuka-lang/web) v0.8.0.

- **Templ components diff on live pages.** In a module on ui v0.5.0 or
  later, the vuka command compiles `.templ` files so a live render records
  their fixed markup apart from the values that change, as it does for JSX.
  An event inside a templ layout sends only what changed, and moving between
  pages that share a templ layout sends what differs inside it, not the whole
  document. Plain HTTP renders write exactly the same HTML as before; templ
  code compiled elsewhere still renders as one block. See [templ](/ui/templ).
- **`vuka run -watch`** rebuilds the app when a `.vuka`, `.templ` or `.go`
  file, `go.mod` or an embedded file changes, and swaps it in once the build
  succeeds. The running app keeps serving while the next one compiles; a
  failed build prints its errors at the `.vuka` line and keeps the last good
  build running; a change that leaves the binary the same (a comment) restarts
  nothing. Tailwind keeps running through the restarts, and with web v0.8.0
  live pages rejoin the new build instead of reloading. See
  [vuka run](/tools/cli#build-run-test-vet-install).

## v0.12.0 — 2026-10-10

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.12.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.3.0's [optimistic updates and
`Call`](/ui/js#optimistic-updates) and [web](https://github.com/vuka-lang/web)
v0.6.0, which runs them in the browser and styles pages with Tailwind.

- **Tailwind for web apps, with no setup.** In a module that uses web,
  `vuka run` keeps `tailwindcss --watch` running and pages pick up each new
  stylesheet without reloading; `vuka build` and `vuka install` build it
  minified into the binary; `vuka gen` writes the file that registers it, so a
  plain `go build` works too. Classes are found in your `.vuka`, `.templ`,
  `.html` and `.go` files and in the component packages you link. Your own
  `app.css`, `styles/app.css` or `web.Tailwind(path)` replaces the built-in
  stylesheet; `web.NoTailwind()` or `VUKA_TAILWIND=off` turns it off. See
  [Styling](/web/styling).

## v0.11.0 — 2026-10-10

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.11.0`. With
[`github.com/vuka-lang/ui`](/ui/) v0.2.0's [JS commands](/ui/js) and
[web](https://github.com/vuka-lang/web) v0.5.0's browser engine for them.

- **Refs.** A JSX target declaring `Ref` gets refs: `ref="menu"` on an
  element names it for the function's code, and each `ui.Ref("menu")` with a
  literal name in a function with JSX must name one of its elements — `no
  ref="mnu" in Menu's render; did you mean "menu"?`.
- The editor no longer offers gopls' deep completions (`x.Y().Z`) as a
  props struct's attributes.

## v0.10.1 — 2026-10-10

Runtime: unchanged, v0.10.0. The vuka command is `cmd/vuka/v0.10.1`.

- **A dependency that doesn't compile is reported as such**, once, at the
  import that needs it, with its own error and file — not as "no export data"
  on every package above it. A dependency built for an older Vuka gets a hint
  (`web v0.3.0 was built for an older Vuka; upgrade it (go get …@latest)`);
  code of your own written for one, `run vuka fix ui`. Healthy packages are no
  longer blamed, and errors that only follow from a failed import ("its type is
  unknown") are left out. The editor shows the same.

## v0.10.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.10.0` — the UI layer is gone from it.
New library: [`github.com/vuka-lang/ui`](/ui/) v0.1.0. Run `vuka fix ui`.

- **JSX targets.** JSX is syntax; what it renders with is a library the
  file imports, its [JSX target](/features/jsx): a package
  declaring `const VukaJSX = 1` and `Node`, `El`, `Text`, `Child`,
  `Fragment`, `Nodes`, `Try` (optionally `On`, `Stateful` + `Component`,
  `WithChildren`, `F`). A file with JSX imports one target; without one it
  is a compile error saying so. Component resolution, attribute binding,
  generic and overloaded components are checked against the target's
  declarations; event payload rules, `Mount`'s shape and `*Live` embedding
  are the target's runtime checks now.
- **`github.com/vuka-lang/ui`**: everything that was `vuka.Node`,
  `vuka.Live`, `vuka.Event`, `vuka.Assign`, `vuka.Tree`, `vuka.String`, … and
  the `live`, `live/livetest`, `term` and `templx` packages, unchanged but for
  the package name. `vuka.TemplComponent`, `RegisterTempl` and
  `TemplComponents` are `templx.Component`, `Register` and `Components`.
- **`vuka fix ui`** rewrites a module: names, imports, the ui import in
  every `.vuka` file with JSX, `go get github.com/vuka-lang/ui`.
- **The runtime requires nothing** (no templ) and needs Go 1.23.
- **The vuka command is its own module**, `github.com/vuka-lang/vuka/cmd/vuka`
  (`go install github.com/vuka-lang/vuka/cmd/vuka@latest` as before). It
  compiles `.templ` files with templ's parser and generator — a plugin of the
  command, on for modules that require templ.
- **Decorators:** optional arguments, parameter attributes, composition,
  type-level advice and attribute targets.
  - A decorator whose parameters are all optional can be written bare:
    `@transaction` calls `transaction()`, and `@transaction(On("db"))` is
    the same decorator with settings.
  - **Parameter attributes:** `func Show(@Path("id") id int, @Body in NewPet)`,
    read by declarers (`vuka.Param.Attrs`) and call decorators
    (`c.ParamAttr(i, &x)`).
  - **Composed decorators:** `decorator ApiRoute(path string) = @web.Get(path)
    @web.Use(auth) @timed`, exported and used like any decorator.
  - **Type-level advice:** a call decorator on a type wraps every method of
    it declared in `.vuka` files; `@vuka.NoAdvice` opts a method out.
  - **Attribute targets:** `@vuka.Targets(vuka.OnField)` on an attribute type
    makes using it elsewhere a compile error.
  [Decorators](/features/decorators), [Attributes](/features/attributes).
- VS Code extension 0.4.2: highlighting for parameter attributes and composed decorators.
- Docs: [UI](/ui/) is its own section; the language's [JSX](/features/jsx)
  page is library-neutral; [Live protocol](/reference/live-protocol) is the
  reference for transport authors.

## v0.9.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.9.0` (`vuka fix runtime` upgrades). New `vuka.Frame`/`vuka.F` (JSX's compile-time shape), `vuka.Tree`,
`vuka.TreeList`, `vuka.TreeRef`, `vuka.BuildTree`, `vuka.Assign`,
`vuka.RootAttrs`; `live.Session.RenderTrees`, `HTML`, `live.TreeUpdate`,
`live.Verify`, `live.Marshal`, package `live/livetest`. Generated code needs
it: JSX now lowers through `vuka.F`.

- **Live render trees (protocol v2).** A client joining with
  `{"type":"join","v":2}` gets each instance's render tree — statics sent
  once per connection, dynamics, block items, instance references — and
  after an event only the changed dynamics: list steps with keyed moves,
  branch switches, long markup by reference or token patch. v1 clients are
  unchanged. A one-cell change in a 1000-row table is 89 bytes instead of
  122 KB. See [Protocol v2](/reference/live-protocol#protocol-v2-render-trees).
- **`vuka.Assign[T]`:** a component whose state is all Assigns is rendered
  again only when one is set or its props change; tests verify skipped
  components against a full render (`VUKA_LIVE_VERIFY`).
- `live/livetest`, a reference client of both protocol versions in Go.
- Fixed: a stateful component tag with an expression key (`<Row key={i} />`)
  crashed the compiler.
- Server messages are written without HTML escaping (`<` rather than
  `\u003c`); the JSON is the same once parsed.
- `vuka gen -inplace` writes line directives relative to the generated
  file, so committed generated code no longer carries the checkout's path.
- [vuka-lang/web](https://github.com/vuka-lang/web) v0.3.0 speaks protocol v2
  in the browser.

## v0.8.0 — 2026-10-10

Runtime: unchanged, v0.7.0.

- **Seamless JSX editing.** Completion for every HTML element (and SVG
  basics) with descriptions, each element's own attributes before global
  ones, event handlers, `aria-*`/`data-*`, enumerated attribute values,
  closing tags, and `{for}`/`{if}`/`{match}` blocks with placeholders; hover
  on elements and attributes with MDN links; linked editing renames a tag's
  closing tag as you type; JSX elements and blocks fold; at most one
  diagnostic while a tag is half-typed.
- **Statics:** a generic type's static takes any initializer —
  `&Store[Self]{}`, `NewStore[Self]()`, `new(Store[Self])`, literals — with
  its type inferred, `Self` per instantiation.
- **VS Code extension 0.4.1:** closing tags after other tags on a line and
  inside attribute braces, `{` pairs inside elements, Enter indents between
  tags, `editor.linkedEditing` on for `.vuka` files.
- Docs: the language pages are library-free; the web framework and the ORM
  have their own sections.

## v0.7.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.7.0` (new: `vuka.Live`, `vuka.Component`,
`vuka.On`, `vuka.Event`, the `live` package; field references `vuka.Ref` with
`OrderedRef`, `StringRef`, `CompareRef`, `NullableRef`, `vuka.Pred`,
`vuka.Order`, `vuka.FieldPath`, `vuka.Related`, `vuka.FieldAttrs`,
`vuka.FieldsOf`; `vuka.Field` has `Attrs`).

- **Stateful components.** A struct embedding `vuka.Live`, with `Mount`,
  `Render` and event handlers: `<Counter Start={5}/>` keeps its state on the
  server, `onClick={c.Inc}` (methods or closures, values, form structs) runs
  there, and only the components that changed are sent back. Components nest,
  `key` keeps list items attached, `Update(msg)` overloads receive broadcasts.
  Rendered without a session they are plain HTML. The `live` package is the
  transport-independent session; [vuka-lang/web](https://github.com/vuka-lang/web)
  serves it over WebSocket. [Stateful components](/ui/stateful).
- **Field attributes.** Typed attributes after a struct field, after its tag:
  `` Title string `json:"title"` @Char{Max: 200} ``, recorded at init.
  [Field attributes](/features/fields#field-attributes).
- **Field references.** `Post.Title` is a typed reference to the field;
  `Post.Author.Name` follows struct, pointer and `vuka.Related` fields. They
  build typed predicates (`Post.Views.Gt(100)`) and orderings, the
  foundation of [vuka-lang/orm](https://github.com/vuka-lang/orm)'s queries.
  [Field references](/features/fields#field-references).
- **JSX:** brace-less `if`/`for`/`match` inside block bodies; JSX in
  package-level variables; props from embedded struct fields; a block header
  may hold composite literals.
- **Statics** may use other statics, field references and `Self` in their
  initializers; initialization cycles are reported with their chain.
- **match:** exhaustiveness checks nested patterns (`Ok(Some(p))`, `Ok(None)`,
  `Err(e)`), and a `match` on a value from `x := f()?` sees its unwrapped type.
- Errors quote field references and statics as the source spells them.
- **Editor:** `vuka.File` paths are links (click to open, hover lists a
  `.templ` file's components); positions stay right while a file with
  several edits doesn't compile; completion offers stateful components, their
  props and event handlers.
- **VS Code extension 0.4.0:** highlighting for field attributes, match
  patterns, JSX blocks, events and component tags; 16 snippets (components,
  routes, live pages, ORM models, transactions); auto-closing tags; the vuka
  version in the status bar.

## v0.6.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.6.0` (new: `vuka.Decl`, `vuka.File`,
`vuka.TemplComponent`).

- **Declaration decorators.** A decorator of type `func(*vuka.Decl)` — or a
  call returning one, such as `@web.Get("/pets/{id}")` — runs once at init with
  the function's name, attributes, parameter names and types, and the function
  itself: the way to register routes, commands or jobs without wrapping calls.
  [Decorators](/features/decorators#declaration-decorators).
- **`vuka.File`.** A string literal where a decorator or attribute takes a
  `vuka.File` is checked when the program compiles (it must exist, in the
  package's directory or below) and embedded in the binary. For a `.templ` file
  `vuka.TemplComponents(f)` gives its components with their parameter names; a
  `.templ` file may sit in a subdirectory that is its own package
  (`views/pets.templ`, `package views`), which Vuka compiles and imports.
  [Files](/features/decorators#files).
- The export-data cache no longer keeps modules replaced by a directory
  (`replace … => ../vuka`), which could serve a stale runtime.
- [**vuka-lang/web**](https://github.com/vuka-lang/web), a web framework built on
  declarers: routes from `@web.Get`, handler parameters bound from the path,
  query and body, dependencies injected, results rendered as JSON or through
  `@web.Template` views (`.html`, `.templ` or JSX).

## v0.5.0 — 2026-10-10

- **Components and JSX.** Markup is an expression in `.vuka` files; components
  are functions, with attributes bound to parameters by name or to a props
  struct, `{for}`, `{if}` and `{match}` blocks, and errors at the tag.
  [Components and JSX](/features/jsx).
- **Rendered by templ.** `vuka.Node` is `templ.Component`: Vuka components and
  templ components call each other, and `.templ` files compile with the rest of
  the package (no `templ generate`), with editor support mapped into them.
- Components can be generic (type arguments inferred, or `<List[User]>`) or
  overloaded; `?` and `match` work inside `{…}`; a Vuka component that would
  drop its children is an error.
- `vuka.Handler`, `vuka.Write`, `vuka.String`; a walkable node tree
  (`vuka.Walk`, `vuka.Renderer`) and a terminal renderer, `vuka/term`, which
  lays out templ components' HTML too.
- **`vuka fmt`**: Go as gofmt prints it, JSX Prettier-style, never changing
  what a page renders. Format on save in the editor.
- Editor: completion after `<`, in a tag's attributes and after `</`; hover,
  go to definition and rename on tags, closing tags and attributes; a tag
  being typed no longer breaks the rest of the file.
- templ's language server can use vuka as its gopls, so `.templ` files see
  code from `.vuka` files.
- VS Code extension 0.3.0: JSX highlighting, templ's icon for `.templ` files,
  format on save, and an offer to serve `.templ` files through Vuka.

**Breaking:** the decorator helper `vuka.Attr[T](c)` is now `vuka.AttrOf[T](c)`
(`vuka.Attr` is JSX's attribute). `vuka fix attr-of` rewrites it. Go 1.25 or
newer is required (templ needs it).

Runtime: v0.5.0.

## v0.4.1 — 2026-10-10

- **`vuka mod`**: `go mod` that sees the imports of `.vuka` files. `vuka mod tidy`
  keeps requirements plain `go mod tidy` would drop.
- Editor: a hover or go-to-definition gopls can't answer (a broken import, code
  mid-edit) is now no answer instead of an error, and VS Code no longer opens
  the output panel on errors.
- An import nothing provides that names a folder of your module gets a hint:
  *it's in this module: import "hello/data"*.

Runtime: v0.3.0.

## v0.4.0 — 2026-10-10

- **One gopls.** Run as `gopls`, vuka is a drop-in gopls, so `.go` files in a
  package with `.vuka` files see their code; every editor session shares one
  gopls daemon.
- **`vuka explain`** shows each line beside the Go it becomes.
- **`vuka fix`**: `runtime`, `static-names`, `orphans`.
- Faster builds: one `go list` per build, export data cached across builds.
- Workspaces reached through symlinks.
- This documentation site.

Runtime: v0.3.0.

## v0.3.1 — 2026-10-09

- Typing `@` lists the project's decorators, narrowing as you type.

## v0.3.0 — 2026-10-09

- **Dependency injection**: a type decorator on a struct gets `t.New`, a
  constructor taking its dependency fields (`inject:"-"` opts out).
- Picking a decorator after `@` inserts its name; a factory gets a call.

Runtime: v0.3.0.

## v0.2.1 — 2026-10-09

- Completion keeps working while an `@` line is being typed; code actions over
  attributes no longer fail; a clear message when the runtime is too old.

## v0.2.0 — 2026-10-09

- **Python-style decorators**: `decorator name(c) { … }`, `c.Next`, `c.Return`,
  `c.Err`, `c.Context`, `c.Attr`; type decorators.
- **Statics** and static methods, statics of generic types and through
  embedding; **Self** inference; **`self` methods**.

Runtime: v0.2.0.

## v0.1.1 — 2026-10-09

- The language server works beside VS Code's Go extension.

## v0.1.0 — 2026-10-09

- Result and Option, `?`, `match`, overloading, attributes, `@export`.
- `vuka build`, `run`, `test`, `gen`, `new`; the `build/` module.
- `vuka lsp` and the VS Code extension.
