# Examples

Examples follow one file type per job: rendering in `.vuka.html`, logic in
`.go` or `.vuka`, pages under `pages/` and service constructors registered on the app.

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

## Counter

Save as `pages/counter.vuka.html`:

```html
package pages

type Counter struct { n int }

func (p *Counter) Increment() { p.n++ }
<main><button onClick={Increment}>{n}</button></main>
```

## Browser state

```html
package components

import "github.com/vuka-lang/ui/v2"

type Disclosure struct { open ui.State[bool] }
<section>
  <button onClick={open = !open}>Details</button>
  <p hidden={!open}>More information</p>
</section>
```

## Routed application

See [Web getting started](/web/getting-started) for startup with
`web.New(pages.Routes(), …)`, [Folder routes](/web/folder-routes) for dynamic
segments and layouts, and [Dependency injection](/web/dependency-injection)
for services and typed hooks. See [Testing](/web/testing) before deploying.
