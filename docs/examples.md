# Examples

Examples follow one file type per job: rendering in `.vuka.html`, logic in
`.go` or `.vuka`, pages under `pages/` and service constructors registered on the app.

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

import "github.com/vuka-lang/ui"

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
