# RFC: Named routes and per-package route facts

Status: accepted 2026-11-24. Implementation lands in three steps (§7).

## 1. Problem

Folder routes cost a large app time on every save, whatever was edited.
Measured on an app of 216 packages (113 pages, 15 shared packages), M1 Pro:

| Step | Cost per save | Why |
|---|---|---|
| route check | ~1.4s | the page packages are type-checked again, all of them, after the transpile |
| `pages` package | 0.8–0.9s | `route.go` (33 KB) holds every page's registration and is compiled again whenever a page's API moves |
| `urls` package | every page reads it; any route change recompiles 117 importers | `urls_gen.go` is one 125 KB package (191 types, 461 funcs) |

The route check's memo is keyed by one hash of the whole module, and the
files of page directories count in full, bodies and markup included
(`routeTree.bodiesRead`): one character of markup in any page invalidates it.
It reads bodies for three facts only — whether a layout reads the request
(`readsRequest`), `@ui.URL` query defaults in `LiveSchema` (`queryDefaults`),
and the components with a bare `@ui.URL` field a page renders (`urlParams`,
E0340) — each of which the page's own transpile already sees.

Link time is not in scope: Go links the whole binary on every change.

## 2. What the user writes

Folders are paths, `[id]` and `[path...]` parameters, `(group)` folders
groups; `_layout.vuka.html`, `Guard` and `Middleware` stay as they are. No
new declaration. What changes is how a page links to another (§4); the
`urls` chain keeps working until the next major version.

## 3. Route facts, per package

While a page package is transpiled — type-checked anyway — vuka records its
**route facts**:

- its pages' and endpoints' types and constructors;
- method handlers and method funcs, with their parameters' names;
- path-parameter bindings: the field, its type as an expression and the
  import path it needs;
- the folder's `Guard`, `Layout` and `Middleware`, their signatures checked
  here;
- whether its layout reads the request, and whether any exported function
  of the package does (so a layout reaching the request through another
  package's function stays exact);
- `@ui.URL` query fields and their defaults;
- the components with a bare `@ui.URL` field its pages render.

The facts are kept with the package's session entry and its disk cache entry,
under the input key the transpile already uses: a package not transpiled
again brings its facts back for nothing.

The **assembler** merges the packages' facts into the route table and runs
the checks across folders — parameters along a path, clashes, E07xx, E0340 —
on plain data, without go/types. Same table, same diagnostics, same codes.

**Self-registration.** Each page package gets `vuka_route_gen.go`: an `init`
registering its pages, handlers and parameter names with web, under their
route names (§4). `pages/route.go` keeps the tree only: a blank import of
each page package, and each folder's layout, guard and middleware by path.
It changes when the tree does. `Routes()` stays its entry point.

## 4. Named routes

### Names

Every route is named by its folder path, with dots; parameter and group
folders add nothing. A method handler adds its method's name.

| Folder | Name |
|---|---|
| `pages/users/index.vuka.html` | `users` |
| `pages/users/[user]/edit/` | `users.edit` |
| `pages/(admin)/orders/[order]/` | `orders` (if no other page is named so) |
| `pages/orders/export/` handler `GET_download` | `orders.export.download` |

Two routes with one name are E0706, the error two alike names of a folder
are today. Names are unique because folders are.

### Linking

In markup and in a `.vuka` file's Go, `route` is a builtin (ctx is implicit,
as everywhere):

```html
<a href={route("users.edit", user.ID)}>Edit</a>
<a href={route("orders", query{"status": "open", "page": 2})}>Open orders</a>
```

```go
return web.Redirect(ctx, route("users.edit", u.ID))
```

Path parameters come in path order; a trailing `query{…}` gives query
values. In a hand-written `.go` file: `web.Route(ctx, "users.edit", id)`,
checked when it runs (a panic naming the closest route under `vuka run`
and in tests; logged, and `"#"`, in production).

A test of a component or module package builds no app, so it has no
table. `vuka test` and `vuka vet` add `vuka_routenames_gen_test.go` to each
package's tests (its external test package when it has one), in the
overlay only, whose `init` gives the module's route names to
`web.RegisterRouteNames`: a table web reads for a name the built app
doesn't have, or with no app built. Production binaries and the non-test
build graph don't change.

### Checked when it is transpiled

- an unknown name: E0725 at the string, with the closest name;
- a wrong count or type of parameters: E0726, naming the route's path and
  its parameters (`users.edit (/users/[user]/edit) takes user int64`);
- a query key the target page has no `@ui.URL` field for: E0727.

The language server completes names inside the string, shows the path and
its parameters on hover, and goes to the page on definition.

### What it compiles to

`route("users.edit", id)` is `web.RouteURL(ctx, "users.edit", id)`: a call
into web, which every page imports already. No page imports a generated
package to link, so no route change recompiles a page that doesn't change.
The table is filled by the pages' `init`s; names are interned there, and a
link costs a lookup and the path's formatting.

A page's input key carries the facts of the routes it names, and only
theirs: a route whose parameters change sends its linkers through the check
again; their Go is usually the same, and go build then compiles nothing.

### The mount prefix

An app served under a prefix keeps its links inside it, as the `urls` chain's
`URL(ctx)` does today through `web.MountPath`: `route` returns the path with
the request's mount prefix in front — `X-Forwarded-Prefix` from a trusted
proxy, else what `http.StripPrefix` removed — and the path alone at the root
or without a request (a job, a cron). `routePath("users.edit", id)` (in Go
files `web.RoutePath`) is the path without the prefix, as `Path()` is today,
for matching and for building an absolute URL with another base. A redirect,
a form's action, `ui.JS.Navigate` and `PushPatch` given a `route(...)` must
carry the prefix exactly once, as they do given `URL(ctx)` today; tests
serving the app under a prefix pin each of them, for `route` and the `urls`
chain alike.

### Moving a folder

`vuka mv <from> <to>` moves a folder and rewrites every `route(...)` naming
a route under it. The old and new names come from the route table before
and after the move, checked in a copy of the module first; the language
server's rename of a route name is the same move.

## 5. The `urls` chain

`urls.Users.User(7).Edit.URL(ctx)` keeps working, generated as now, until
the next major version. `vuka fix routes` rewrites the chains it can name
statically to `route(...)`; W0706 marks the rest. New docs teach `route`
only.

## 6. Cost, expected

| Change | Before | After |
|---|---|---|
| a page's body or markup | 1.4s + 0.9s | ~0 |
| a page's declarations | 1.4s + 0.9s | one package's facts |
| a route added | 117 packages | the page and `route.go` |
| a route moved or renamed | 117 packages, links failing at go build | its linkers checked again; a broken link E0725 at its line |
| a cold build | the route type-check, 117 reads of 125 KB | facts collected in the transpile |

## 7. Steps

Each lands with before/after numbers on a synthetic app of this shape, and
build-graph tests (`-debug-actiongraph`): an edited body leaves `route.go`
and `urls` unwritten and uncompiled; an added route compiles its page and
`route.go` only.

1. **Facts and self-registration.** Route facts from the transpile, the
   assembler, `vuka_route_gen.go`, the tree-only `route.go`. The route table
   (`vuka routes -json`) byte-identical on the fixtures; conformance (as
   Vuka and ejected) and the E07xx/E0340 goldens unchanged.
2. **`route`.** The builtin, `web.Route`/`web.RouteURL`, E0725–E0727, the
   language server's names, `vuka fix routes`, W0706. *Shipped.* Two paths
   that differ only by parameters each keep theirs (`orders`,
   `orders.order`). The links are recorded with the route facts and checked
   by the assembler, so a target's change re-checks its linkers without
   transpiling them again, and the input keys stay as they were.
3. **`vuka mv`**, and docs teaching `route`. *Shipped.* The urls chains are
   rewritten as the new chain, not as `route(…)`; the editor's rename of a
   route name and its explorer's moves under `pages/` both use the plan;
   `vuka routes` prints the names.

The concurrent transpile (`transpileRun` as a scheduler) waits for step 1,
since both change how the transpile runs.

## 8. Risks

- Hand-written Go names routes by string: a mistake is found when it runs,
  at once under `vuka run` and in tests, not when it compiles.
- Positional parameters can be swapped when two have one type
  (`[org]/[user]`, both `int64`); hover and E0726 name them in order.
- A page is registered by being imported; `route.go` imports each one, as
  it does today.
