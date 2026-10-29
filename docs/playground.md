---
layout: page
sidebar: false
aside: false
title: Playground
description: Edit Vuka in the browser and see the Go it becomes, with the diagnostics vuka build and vet report.
---

<ClientOnly>
  <Playground />
</ClientOnly>

<div class="pg-notes">

## Using the Playground

Pick a start from **Example** — a live counter, a form on a live page, a
component with props, a page with markup calls and `@ui.Defer`, an enum
with `match`, ORM models — or edit the files in place. **+** adds a
`.vuka`, `.vuka.html` or `.go` file; double-click a tab to rename it.

**Generated Go** shows the Go the open file becomes, as
[`vuka explain`](/tools/cli#explain) prints it; while the package has
errors it keeps the last Go that built. **Diagnostics** lists what `vuka
build` and `vuka vet` report, the same in the editor's gutter: a code opens
its [page](/errors/), a position moves the cursor there. **Format** runs
[`vuka fmt`](/tools/cli#fmt) on the open file. **Share** copies a link
holding every file of the project, so whoever opens it sees the same.

The compiler runs in the page, as WebAssembly, with the types of the
standard library, ui, web and orm; a project imports those only.

## Running

When the site is built with a runner (`VITE_VUKA_RUNNER`), **Run** — or
Ctrl/⌘+Enter — builds the project on a server and shows the program in the
**Program** tab: its pages, its live events over the page's connection and
what it prints, with the build's log as it runs. The bar above the program
takes a path (`/orders/7`) and opens the program in a tab of its own. Each
run gets a fresh SQLite file (`DATABASE_URL`), imports std, ui, web and orm
only, and stops after two minutes unused or ten in all; running again, or
leaving the page, stops it. Without a runner the playground transpiles
only.

The runner, [`playground/runner`](https://github.com/vuka-lang/vuka/tree/main/playground/runner),
builds and runs each project in nsjail: no network, its own SQLite file,
limits on time, memory, processes and disk, and each program served from
an origin of its own. Its README covers the container, its settings and
running it locally beside `vitepress dev`.

</div>
