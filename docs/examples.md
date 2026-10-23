# Examples

[[toc]]

## Introduction

Examples follow one file type per job: rendering in `.vuka.html`, logic in
`.go` or `.vuka`, pages under `pages/` and service constructors registered on
the app. The small ones below show the shape; the repository holds whole apps.

## Whole Apps {#apps}

Whole apps written this way are in the repository:
[`examples/folder-routes`](https://github.com/vuka-lang/vuka/tree/main/examples/folder-routes)
(pages, layouts, guards and a JSON API as folders, with a `main.go` passing
`pages.Routes()` to `web.New`),
[`examples/islands-go`](https://github.com/vuka-lang/vuka/tree/main/examples/islands-go)
(a Vue island in a live page and a page backed by its component, built
with `go generate` and run with `go run .`) and the apps of
[`examples/conformance`](https://github.com/vuka-lang/vuka/tree/main/examples/conformance)
— `library-users`, `crud`, `live`, `components`, `islands`, `islands-vue`.
Each conformance app is written once, in Vuka, and CI checks that it
answers alike built by `vuka build` and ejected to plain Go.

## A Counter {#counter}

A live page that counts clicks on the server. Save it as
`pages/counter.vuka.html`:

```html
<script lang="go">
package pages

type Counter struct { n int }

func (p *Counter) Increment() { p.n++ }
</script>

<main><button onClick={Increment}>{n}</button></main>
```

## Browser State {#browser-state}

A component that opens and closes a section in the browser, with no server
round trip:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Disclosure struct { open ui.State[bool] }
</script>

<section>
  <button onClick={open = !open}>Details</button>
  <p hidden={!open}>More information</p>
</section>
```

## A Routed Application {#routed-application}

See [Directory Structure](/web/getting-started) for startup with
`web.New(pages.Routes(), …)`, [Folder Routes](/web/folder-routes) for dynamic
segments and layouts, and [Dependency Injection](/web/dependency-injection)
for services and typed hooks. See [Testing Web Apps](/web/testing) before
deploying.
