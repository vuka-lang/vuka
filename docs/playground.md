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

## Running

When the site is built with a runner (`VITE_VUKA_RUNNER`), **Run** — or
Ctrl/⌘+Enter — builds the project on a server and shows the program in the
**Program** tab: its pages, its live events and what it prints. Each run
gets a fresh SQLite file (`DATABASE_URL`), imports std, ui, web and orm
only, and stops after two minutes unused or ten in all; running again, or
leaving the page, stops it. Without a runner the playground transpiles
only.

</div>
