# RFC: The dev console — what `vuka run -watch` shows

Status: accepted 2026-11-27. Implementation lands in four steps (§6).

## 1. Problem

The watch reports itself in plain `●` lines on stderr: `● compiling… (3s so
far)`, `● compiling took 5.5s`, `● rebuilt in 4.9s`. They are honest but
flat: a step is named only once it has been slow for 3s, so a 5s rebuild is
silence and then an apology; a fast rebuild and a failed one read alike at a
glance; a cold build looks hung; the app's address scrolls by inside its own
log. The watch already knows it is on a terminal (`supervisor.tty`) and has
a spinner engine (`internal/progress`), and uses neither for the build loop.

What the established dev servers each do well:

| | worth taking |
|---|---|
| Vite | a ready banner with the URL; `dim-time [tag] verb file` events; repeats collapse into `(x2)`; keys (r restart, o open, c clear, q quit) |
| Next.js | symbol grammar — `○ Compiling /route…` then `✓ Compiled in 450ms`; request rows `GET / 200 in 32ms` |
| cargo | a progress counter on long builds (`112/216`), so a cold build never looks hung |
| Laravel | dotted-leader request rows, aligned and dim |
| Astro | fixed columns: dim timestamp, colored tag, message |
| Phoenix | restraint: the terminal stays quiet, detail lives elsewhere (vuka's browser toolbar already plays that role) |

## 2. What the terminal shows

Calm when fast, alive when slow. While a build runs, **one line pinned at
the bottom**, redrawn in place, naming the phase from its first moment:

```
⠹ compiling  3.1s   143/216 packages
```

Each rebuild ends as one persisted line: dim timestamp, colored symbol,
verb, time, dim detail.

```
12:04:01 ✓ rebuilt in 4.9s   posts/index.vuka — transpile 0.8s · compile 3.2s · link 0.9s
12:04:30 ✓ updated in 0.2s   posts/index.vuka.html (markup only — same binary)
12:05:02 ○ unchanged         the edit didn't change the binary
12:06:15 ✗ build failed      pages/orders/orders.vuka:981 — the last good build keeps running
```

- `✓ rebuilt` (green) carries the phase breakdown the plain output gives
  only when a phase was slow, and names the file(s) whose change the build
  answers — the watcher has them.
- `✓ updated` is the devstatics restart: its own verb, so markup-only edits
  visibly cost less.
- `○ unchanged` and `reusing the last build` are dim reassurance.
- `✗ build failed` (red) names the first error's file:line on the line
  itself; the diagnostics follow as today (`diag.Render`, already colored).
  The failure is **sticky**: the pinned line stays red through further
  edits — `✗ 3 errors  orders.vuka:981 — last good build serving` — until a
  build goes green, which prints `✓ fixed — rebuilt in 2.1s`. The screen is
  cleared when errors appear and when they go, as today; `-no-clear` opts
  out.
- The app exiting is red: `✗ the app exited (exit status 1) — waiting for a
  change`.
- `● linking N declarer packages (…)` becomes a dim `↻ N page packages
  linked` (the full list under `-v`).
- An identical consecutive event updates in place with `(x3)` instead of
  scrolling (Vite).

**Cold builds get a counter.** `go build -v` names each package as it
compiles, and the watch has the module's package list from the preload's
`go list -deps`: any compile past 3s shows `compiling 143/216 packages` on
the pinned line. First builds and dependency bumps stop looking hung.

## 3. The banner, and keys

The watch owns the app's stdout/stderr, so it catches web's
`web: listening on <url>` line the first time and renders a banner instead
of letting it scroll by:

```
  vuka v2.15.0  dev — 216 packages

  ➜  Local:   http://localhost:8080
  ➜  press h + enter for commands
```

Keys, when stdin is a terminal: `r` force a rebuild, `o` open the browser,
`c` clear, `q` quit, `h` help. Today the app inherits stdin; the watch
takes it for keys and a `-stdin` flag hands it back for the rare app that
reads it.

## 4. Request rows

web logs one structured line per request in dev only (gated on the watch's
env it already reads); the watch renders it dim and columnar, the status
colored, toolbar and asset traffic skipped:

```
12:04:12  GET  /orders ·················· 200  12ms
12:04:15  POST /orders/7/approve ········ 303  48ms
```

Per-request detail stays in the browser toolbar; `-quiet` silences the
rows. This is the one part of the design that touches web.

## 5. Mechanics

A `status` type in cmd/vuka with two renderers behind one API — the build
loop and `phases` report to it instead of printing:

- **plain** (pipes, CI, `TERM=dumb`, `NO_COLOR`): byte-for-byte today's `●`
  lines, slow-phase timing included. Every existing test
  (`waitLog("● rebuilt in ", …)`) and anything grepping a log is untouched.
- **terminal**: the pinned line, color, timestamps, the grammar above. It
  wraps the writers the app's output goes through, erasing the pinned line
  before a log write and redrawing after, so app logs interleave cleanly
  above it.

Phase durations are already measured (`phases.start`); the breakdown is
carrying them to the final line.

## 6. Steps

1. **`status` and the plain renderer.** A pure refactor: the watch's
   prints and `phases` go behind the interface, output byte-identical,
   proven by the existing tests plus a golden test of the plain renderer.
2. **The terminal renderer.** Pinned line, colors, timestamps, the final-
   line grammar with phase breakdown and file names, sticky failure,
   repeat collapse, `-no-clear`.
3. **Banner and counter.** The listen-line banner; the cold-build package
   counter from `go build -v` against the preload's package list.
4. **Keys and request rows.** `r`/`o`/`c`/`q`/`h` with `-stdin`; the
   dev-only request line in web and its columnar rendering (`-quiet`).

Steps 1–3 touch only cmd/vuka.

## 7. Risks

- The pinned line must never corrupt piped output: everything in-place is
  gated on the same terminal test the progress package uses, and the plain
  renderer is the default everywhere else.
- `go build -v` adds package names to stderr; the counter must consume
  them, not print them, and build errors must pass through untouched.
- Taking stdin changes what the app inherits; `-stdin` restores it, and
  apps started without a terminal see no change.
