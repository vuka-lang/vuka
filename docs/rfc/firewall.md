# RFC: The compile firewall — editing a shared package without recompiling its importers

Status: rejected 2026-11-27 — experiment failed (§7). The facade stops
only edits that leave the edited package's export data unchanged; an
unexported addition, or a body edit that moves a later declaration's line,
changes it, and the change passes through the facade to every importer.
No shard writer was built.

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
generated, fans out. The status stays rejected.
