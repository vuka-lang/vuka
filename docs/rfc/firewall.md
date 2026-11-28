# RFC: The compile firewall — editing a shared package without recompiling its importers

Status: draft v3 2026-11-27 — padded layout + facade (§8). The sharded
design of §3 was rejected (§7, §7.1); what carries is a whole-package alias
facade over code laid out so that body edits leave its export data as it
was. Prototype behind `VUKA_FIREWALL=1` in the watch; numbers in §8.4,
exported variables by linkname in §8.6.

## 1. Problem

Editing a widely imported package of the module costs a save the compile of
every importer. Measured on a 216-package app (113 pages, 145 entity types,
M1 Pro, `vuka run -watch`, warm caches):

| Save | Transpile | Go compile + link | Total to live |
|---|---|---|---|
| Go in one page | 0.9–1.4s | 2.0–3.8s | 5–7s |
| Go in a shared component (87 importers) | 1.9s | **14.6–19.1s** | 19–23s |
| an entity package's API (95–143 importers) | 1.0s | **21.5–21.9s** | 25s |

The transpiler is not the cost; Go recompiling the importers is. The app's
shape makes it structural: every page imports the entity packages and the
component package, so any edit to one puts the whole app in the blast
radius. Splitting packages by hand (an entities core and per-domain
packages) helps exactly in proportion to how far the split is taken, and
asks the author to design their packages around the compiler.

## 2. The fact the design stands on

Go's build recompiles a changed package's **direct importers** always —
any change to a package's object invalidates the compile actions that
consumed it, exported API changed or not. But the cascade stops at the
first level whose **outputs come out unchanged**: a package recompiled
against a body-only change produces a byte-identical object, so *its*
importers get cache hits.

Verified with a three-level module (dep ← u1,u2 ← main, compiled as the
watch compiles the module, `-l -dwarf=false`): a body-only edit to dep
recompiles dep, u1 and u2 — and not main.

So Go already has a recompilation firewall; it just sits one level above
the edited package. The design puts a generated, nearly-constant package
on that level.

## 3. Design: shards behind an alias facade

The transpiler splits a shared package into **shards** and makes its
import path a generated **facade** of aliases. The author writes one
package, as today; importers keep their import line; the files live only
in the build overlay, like the route and devstatics files — nothing is
written into the source tree.

For an entity package `m/entities`:

- Each entity type (with its methods, manager, and generated field set and
  scanner) becomes a shard package under a synthetic path
  (`m/entities/.fw/applicant`). Types that reference each other — mutual
  relations, shared embedded bases — land in one shard: shards are the
  strongly-connected groups of the type-reference graph, so no shard
  cycles exist.
- The facade at `m/entities` is pure re-export: `type Applicant =
  fwapplicant.Applicant`, `var Applicants = fwapplicant.Applicants`,
  constants and functions forwarded. Aliases preserve type identity, so
  assertions, the ORM's schema caches and JSON are untouched.
- Package-level `init` and `var` initialization move with their shard;
  order within the original package is kept by making each shard import
  its predecessor's shard when an initializer reads across groups (falling
  back to one shard when order can't be proven).

What a save costs then:

- **A body, helper or validation edit** to one entity: its shard
  recompiles (~0.1–0.3s), the facade recompiles (milliseconds; its output
  is byte-identical because the shard's API didn't change), every importer
  cache-hits, and the link runs. ~22s becomes roughly the link (~1.4–3s).
- **An API change** (new field, changed type): the shard's export data
  changes, the facade's output changes, importers recompile — correctly,
  they genuinely need re-checking.

The same mechanism covers the component package: one shard per component
(`.vuka.html` + its Go), the facade aliasing each component type. The 19s
component-edit save collapses the same way.

**Which packages get a firewall:** any module package imported by at least
N other module packages (default ~8), measured from the import graph the
transpile already has; `[firewall]` in vuka.toml names packages in or out,
and `VUKA_FIREWALL=off` turns it off for a session. Pages and leaf modules
never qualify, so the common case shards a handful of packages.

**Dev-only.** The watch's overlay builds with the firewall; `vuka build`
compiles the module as written. Production binaries, symbol names and
paths are exactly today's.

## 4. What it shows the author

Nothing to declare, and the dev console says what happened:

```
12:04:01 ✓ rebuilt in 2.1s   entities/applicant.vuka — 1 shard, importers unchanged
12:06:15 ✓ rebuilt in 21.9s  entities/applicant.vuka — API changed, 95 importers
```

The session already computes per-package API hashes (`check`'s api/core),
so the message costs nothing new.

## 5. Risks and limits

- **`reflect.Type.PkgPath`** of a sharded type reports the shard's path in
  dev. The ORM keys on `reflect.Type` identity and names, which aliases
  preserve; anything keying on PkgPath (a registry, error text) must be
  audited. Dev-only scope bounds the exposure; the prototype measures it.
- **Unexported cross-references.** A function in the original package
  reading another shard's unexported field forces the two into one shard
  (same SCC rule). Worst case the whole package is one shard and the
  firewall degrades to today's behavior — never worse, sometimes no
  better.
- **go:embed** in a sharded package moves with its declaring file's shard;
  the embed root rules are the watcher's as today.
- **Init order** across shards must match the original package's; the
  prototype asserts it with an ordering fixture.
- **Debug fidelity**: stack traces in dev name shard paths. The panic
  rewriter (respeller) can map them back to the written package.
- The first build pays the facade compiles (~ms each) and a slightly
  larger package graph; the action-graph tests pin the overhead.

## 6. Prototype plan (this RFC's gate)

1. **Facade experiment**: extend the three-level module with an alias-only
   facade between dep and its users; assert a dep body edit leaves the
   users cache-hit (`go build -x` counts compiles). If this fails, the RFC
   dies here.
2. **Prototype the shard writer** for entity-shaped packages in the watch
   overlay, behind `VUKA_FIREWALL=1`: shard by type SCCs, facade of
   aliases, managers forwarded. Enough to run the watch bench's entity and
   component saves before/after on the synthetic app.
3. Decide from the numbers: target is the ~22s saves under 4s with
   byte-identical rendered pages and green suites.

Productization (vuka.toml keys, the console line, panic mapping, lsp
transparency) is a follow-up RFC section once the prototype holds.

## 7. Experiment result (§6 step 1) — the gate failed

Module `dep ← facade ← u1..u20 ← main`, the facade only aliases and
re-exports dep (`type Item = dep.Item`, `var Items = &dep.Items`, `const
Max = dep.Max`, `var Find = dep.Find`); a control module drops the facade
(`dep ← u1..u20 ← main`). Built as the watch builds the module
(`-gcflags=example.com/m/...=-l -dwarf=false`, go1.27.3 darwin/arm64), each
edit applied to a warm cache, recompiles counted from `go build -x`
(`compile … -p <pkg>`):

| Edit to dep | with facade | without facade |
|---|---|---|
| (a) body edit, same lines | dep, facade | dep, u1..u20 |
| (a′) body edit adding a line in the last func | dep, facade | dep, u1..u20 |
| (a″) body edit adding a line above later decls | **dep, facade, u1..u20, main** | dep, u1..u20, main |
| (b) unexported func appended | **dep, facade, u1..u20, main** | dep, u1..u20, main |
| (b′) unexported func above other decls | **dep, facade, u1..u20, main** | dep, u1..u20, main |
| (b″) unexported func in a new file | dep, facade † | dep, u1..u20 † |
| (b3) unexported var appended | **dep, facade, u1..u20, main** | dep, u1..u20, main |
| (c) exported func appended | dep, facade, u1..u20 | dep, u1..u20, main |

† a cache-history artifact, corrected in §7.1: rerun on a fresh module
path, (b″) fans out like (b).

Raw output (facade module):

```
-- (a) body-only edit (same lines)
dep facade
-- (a') body edit adding a line
dep facade
-- (b) add unexported func
dep facade main u1 u10 u11 u12 u13 u14 u15 u16 u17 u18 u19 u2 u20 u3 u4 u5 u6 u7 u8 u9
-- (b') add unexported func before others
dep facade main u1 u10 u11 u12 u13 u14 u15 u16 u17 u18 u19 u2 u20 u3 u4 u5 u6 u7 u8 u9
-- (a'') body edit adding a line before later decls
dep facade main u1 u10 u11 u12 u13 u14 u15 u16 u17 u18 u19 u2 u20 u3 u4 u5 u6 u7 u8 u9
-- (b'') add unexported func in a separate file
dep facade
-- (b3) unexported var
dep facade main u1 u10 u11 u12 u13 u14 u15 u16 u17 u18 u19 u2 u20 u3 u4 u5 u6 u7 u8 u9
-- (c) add exported func
dep facade u1 u10 u11 u12 u13 u14 u15 u16 u17 u18 u19 u2 u20 u3 u4 u5 u6 u7 u8 u9
```

What it shows:

- §2 holds for what it measured: the cascade stops one level above the
  edited package when that level's output comes out unchanged, and an
  alias facade does sit on that level for (a) and (a′).
- But the facade's output is unchanged only while dep's **export data** is.
  Any change there changes the facade's own archive (§7.1: three different
  unexported additions give the facade one and the same new content id, so
  what it carries is dep's declaration layout, not a hash of its API), and
  its importers recompile. A facade can hide dep's object code, never its
  export data.
- dep's export data changes on far more than API: an unexported func or
  var added anywhere, and declaration positions — so a body
  edit that adds or removes a line moves every later declaration and fans
  out (a″). Through the watch's `//line` directives that is the position in
  the `.vuka` source, so most real saves (adding a line to a component or
  an entity method) are (a″)-kind.
- Sharding doesn't change this: the edited declaration's shard has its
  export data changed by the same edits, and the facade above it passes the
  fingerprint on. Shards would only shrink the edited package's own
  compile, which is not the cost (§1).

The remaining win — an alias facade over a package, unsharded, firewalls
same-line body edits — is too narrow to carry the design; the measured
19–25s saves are dominated by edits that change export data.

### 7.1 Position pinning (v2) — also negative

Hypothesis: the (a″)/(b) fan-outs come from source positions in export
data, and the transpiler controls positions through `//line`, so pinned
positions could keep export data stable. Same module and flags, facade in
place; **each case on a fresh module path** (the export data records
absolute paths, so no case can hit a cache entry another case left — the
§7 (b″) result was such a hit). Layouts of dep:

- plain — one file, no directives (the control)
- pinned — one file, `//line dep.go:1000`, `:2000`, … before each decl
- per-decl — one file, `//line <decl>.go:1` before each decl
- files — one real file per decl
- padded — one file, each decl at a fixed physical line (100, 200, …),
  blank lines absorbing growth

New decls get their own directive, file or slot in each layout.

| Edit to dep | plain | pinned | per-decl | files | padded |
|---|---|---|---|---|---|
| (a) body edit, same lines | dep, facade | dep, facade | dep, facade | dep, facade | dep, facade |
| (a′) line added in the last func | dep, facade | dep, facade | — | — | — |
| (a″) lines added in an early func | **fans out** | **fans out** | **fans out** | dep, facade | dep, facade |
| (b) unexported func appended | **fans out** | **fans out** | **fans out** | **fans out** | **fans out** |
| (b3) unexported var appended | **fans out** | **fans out** | **fans out** | **fans out** | **fans out** |
| (b″) unexported func in a new last file | **fans out** | **fans out** | **fans out** | **fans out** | **fans out** |
| (c) exported func appended | fans out | fans out | fans out | fans out | fans out |

"fans out" = dep, facade, u1..u20, main.

- `//line` changes nothing: pinned and per-decl match plain cell for cell.
  A directive starts a relative position base, and the base is recorded
  at the directive's own physical position, so lines added above it still
  move it.
- Physical positions are what count for body edits: one file per decl, or
  fixed-line padding, contains (a″).
- Unexported additions fan out in every layout, new file or not: adding
  any package-level func or var changes dep's export data whatever its
  position. Earlier runs that seemed to contain one were cache hits — the
  facade's output after an unexported addition is the same whichever
  addition it was, so the first case to reach it pays the fan-out and the
  later ones look contained.

Verdict: position pinning through `//line` contains nothing; physical
layout (a file or a padded slot per declaration) would contain body edits
that add or remove lines, but no layout contains an added unexported
func or var — and every save that adds or removes one, written or
generated, fans out. (§8 builds on the layouts that do contain body
edits.)

## 8. v3: padded layout behind a whole-package facade

§7.1 found the two layouts that keep a body edit's export data stable:
a file per declaration, or each declaration at a fixed line with blank
lines taking up growth. The transpiler owns the layout of the Go it
writes, so the watch can lay a shared package out that way, and a plain
alias facade over the whole package (no shards) then stops the cascade.

### 8.1 Gate in the toy module

Padded dep (each declaration at a fixed line, 100-line slots) behind the
facade, each case on a fresh module path:

| Edit to dep | Recompiled |
|---|---|
| (a) body edit, same lines | dep, facade |
| (a″) lines added in an early func | dep, facade |
| (a3) early func grows 40 lines, within its slot | dep, facade |
| (d) early func outgrows its slot (later decls move) | dep, facade, u1..u20 |
| (g) body edit inside a generic func | dep, facade, u1..u20 |
| (b) unexported func appended | dep, facade, u1..u20, main |
| (c) exported func appended | dep, facade, u1..u20, main |

Two more facts the prototype had to respect, measured the same way:

- A `//line` directive's *value* is export data when it positions a
  declaration (its header, a struct's fields): shifting those values by one
  fans out. The same shift in directives inside function bodies doesn't.
- What escape analysis concludes about an exported function's parameters
  is export data: a body edit changing whether a parameter escapes, or
  leaks to a result, fans out (TestFirewallContains/escape).

### 8.2 Design

Behind `VUKA_FIREWALL=1`, the watch's prepare step, after the module
overlay and devstatics (`cmd/vuka/firewall.go`):

- **Which packages:** module packages other than main imported by at least
  8 module packages (`VUKA_FIREWALL_IMPORTERS`), from the import graph
  discovery already has. A package with cgo, assembly or `go:embed`, or with
  an exported variable whose type the facade can't tell (§8.6), is left as
  it is; `VUKA_WATCH_TRACE` says which and why (`firewall: left <pkg>: …`).
- **Facade:** every Go file of the package is deleted in the overlay and
  moved to `<dir>/0vukafw/`; at the import path stands one file:
  `type T = vukafw_.T` (generic types with their type parameters),
  `const C = vukafw_.C`, and a forward per function (`func F(p0 A) R {
  return vukafw_.F(p0) }`, generic ones instantiating), which keeps
  `runtime.FuncForPC`'s name of the package; a function whose signature names
  what the facade can't (an unexported type) is bound as `var F = vukafw_.F`.
  An exported variable stays the core's own: `//go:linkname V <core>.V` over
  `var V T` (§8.6). Importers are untouched.
- **Layout:** the prologue (package clause and imports) and each top-level
  declaration take a slot: their length plus at least 10 lines, rounded up
  to 20. Each declaration starts with a `//line` giving its own position;
  the directives in its doc comment are dropped, and each function body
  opens with a directive giving its first line, so the lines inside bodies
  report where they are in the `.vuka` source as before.
- **Hysteresis:** the watch keeps each file's layout. While the file's
  declarations are the same and each still fits its slot, the slots stay and
  so do the values of the directives outside bodies — the pins. A
  declaration outgrowing its slot, or one added, removed or renamed, lays
  the file out afresh (and fans out, as it must).
- **Dev-only:** `vuka build`, `vuka test` and the language server never
  see it; unset, the overlay is today's.

What still recompiles the importers: an API change; any added or removed
package-level declaration; a declaration outgrowing its slot; an edit to a
generic function's body; an edit changing a parameter's escape analysis;
an added import beyond the prologue's slot.

### 8.3 Correctness

- Unit tests (`firewall_test.go`): every token keeps the position it
  reports through the layout; starts and pins survive a body growing in its
  slot and change when it outgrows it; the facade of a package with plain,
  generic and variadic functions, generic types and iota constants builds
  and runs an importer unchanged; and, compiled as the watch compiles,
  a body edit adding lines recompiles only the package and its facade while
  an escape change and an added function recompile the importers.
- Watch e2e (`TestFirewallWatch`, `-toolexec` counting compiles): with
  `VUKA_FIREWALL=1` a body edit adding lines compiles `shared` and
  `shared/0vukafw` only; an added function compiles the importers; the page
  is byte-identical (live tokens and toolbar ids aside) to the one the same
  app serves without the firewall, which runs no firewall step.
- On the bench's generated app, by hand: a body edit to `models` compiled
  `models` and `models/0vukafw`; to a component, `components` and its core;
  an unexported func or an API change, 17 packages. Six rendered pages were
  identical with the firewall on and off.

### 8.4 Numbers

`TestWatchBench`, scale 36 (50 packages), GOMAXPROCS=10, M1 Pro, both
sides run back to back on one build cache: before (`VUKA_FIREWALL` unset),
then after (`VUKA_WATCH_BENCH_ENV=VUKA_FIREWALL=1`). Go compile + link /
total to live:

| Save | Before | After |
|---|---|---|
| Go body edit in a shared component #1 | 1.53 s / 2.5 s | 0.57 s / 1.5 s |
| Go body edit in a shared component #2 | 1.44 s / 2.5 s | 0.53 s / 1.5 s |
| Go body edit, widely imported package #1 | 1.54 s / 2.5 s | 0.51 s / 1.5 s |
| Go body edit, widely imported package #2 | 1.55 s / 2.4 s | 0.50 s / 1.4 s |
| Go in one page #1 | 0.55 s / 1.5 s | 0.54 s / 1.5 s |
| text in a shared component #1 | 0.00 s / 0.4 s | 0.00 s / 0.4 s |
| new unused func, widely imported package #1 | 1.56 s / 1.9 s | 1.55 s / 1.9 s |
| new exported func, widely imported package #1 | 1.54 s / 2.0 s | 1.49 s / 2.4 s |
| exported API change, widely imported package #1 | 1.56 s / 2.5 s | 1.60 s / 2.5 s |

The body-edit saves compile as much as a one-page edit (the link and
two small packages); the rows that change export data are as before. The
synthetic app's importers are cheap packages, so the saving here is ~1 s,
not the 15–22 s of the 216-package app in §1, where the importers'
compile is most of the save; that app is the measure still to take.

### 8.5 Problems met and open

- **PkgPath**: types and methods report `<pkg>/0vukafw` in dev. Nothing in
  the bench app or the e2e app depended on it (the runtime's method registry
  keys on the type); an app keying a registry, a cache or a message on
  `reflect.Type.PkgPath` or a method's `FuncForPC` name would see it.
- **Init order**: the package's initialization runs in the core, before the
  facade; its dependencies are the same, so its order relative to them is.
  Go orders independent packages by import path, so relative to unrelated
  packages it can move; no case here noticed.
- **Pinned positions lie**: after lines added above a declaration, its
  header and fields report the line they had at the last layout until the
  next one; body lines are exact. A compile error in a signature then
  points a few lines off; the respeller could map pins back (not done).
- Exported variables kept a package out until §8.6.
- `go test ./...` in cmd/vuka passes with `VUKA_FIREWALL` unset; CI is
  green.

### 8.6 Exported variables: one variable, by linkname

On the 216-package app the trace showed every entity package left out:
the orm's generated `<M>Columns = <M>FieldsIn(…)` and `<M>Queries =
<M>Query{…}` are exported variables, and those are the packages with the
most importers (143, 95, 66). A facade `var V = vukafw_.V` is a copy, a
second variable: an importer's `&pkg.V`, or a write to it, would not reach
the core's. Keeping the variables at the import path and moving the rest
doesn't work either: the core's own code reads them (handwritten queries
through `<M>Queries`), and it can't import its facade.

Go has no variable alias, but the linker has one name per variable. The
variable stays declared and initialized in the core, and the facade
declares it without a value under the core's symbol:

```go
import _ "unsafe"

//go:linkname OrderQueries example.com/app/entities/0vukafw.OrderQueries
var OrderQueries OrderQuery

func _() { var _ *OrderQuery = &vukafw_.OrderQueries }
```

Importers compile against the facade's export data (`var OrderQueries
OrderQuery`, stable across body edits) and reference the core's symbol:
`&pkg.V` is the core's address, writes and reads are the core's, and its
initialization runs where it did, in the package's init, before any
importer's. A pull linkname to a non-std symbol passes `-checklinkname`;
the facade's declaration, content-free BSS, yields to the core's definition
(data or BSS) at link time.

The facade must spell the variable's type exactly, without type-checking
the package: the declared type, else what the initializer plainly is — a
composite literal's type (`&T{…}` its pointer), a call to a non-generic
package function with one result (that result type, read in the function's
file), a conversion to a package type, a basic literal's default type, or
`errors.New`/`fmt.Errorf`/`time.Date`/`time.Now`. Anything else (a function
value, a method call, another package's function, `[...]T{}`) leaves the
package out, as before. The blank function, type-checked and never run,
makes a wrong guess a compile error (`*T` only takes `&V` when the types are
identical), never a variable of the wrong size. A core path the linker
would escape (control characters, `%`, `"`, non-ASCII) leaves it out too.

Tests: TestFirewallFacade adds a handwritten file (int, error, struct,
slice and pointer variables, an unexported one that stays out) and an
ormgen-shaped generated file (`OrderColumns`, `OrderQueries`, an `init`
reassigning it); the importer writes through `pkg.V` and checks
`&pkg.V == core's &V` for each, and five initializers it can't tell keep a
package out. TestFirewallContains/vars: a body edit beside exported
variables compiles the package and its core only. TestFirewallWatch's
shared package exports the item its page shows.

With it, on the 216-package app, the entity package and each per-domain
entity package over the threshold go behind facades; what stays out are three
`flow.Of[…]()` variables, one function-typed variable, a package with
non-Go files and one generic signature.

`TestWatchBench`, the 216-package app, GOMAXPROCS=4, `-p=2`, M1 Pro, the
three runs back to back on one build cache. The entity edit renames an
index in `workflowdb`'s one handwritten method; that package has 7
importers, under the default 8, so the third run lowers the threshold. The
high-fan-in entity packages hold no handwritten function bodies: their
bodies are generated, and an edit to a model changes its fields, which is
API. Go compile + link, two saves each:

| Save | Off | On (8) | On (7) |
|---|---|---|---|
| component body (87 importers) | 14.97 s, 15.37 s | 1.90 s, 1.95 s | 2.00 s, 2.04 s |
| entity body (7 importers) | 2.92 s, 2.99 s | 3.00 s, 3.06 s | 1.89 s, 1.93 s |

Behind its facade the entity edit costs what the component edit does —
the package, its core and the link. Before this change the same edit
measured 2.60 s off and 2.68 s on; the entity packages were left out then,
whatever the threshold.
