# Navigation

Use `<ui.Link>` for navigation that can remain within the live connection.
Ordinary links still support full-document navigation.

```html
package components

import "github.com/vuka-lang/ui"

type Nav struct{}
<nav>
  <ui.Link href="/pets">Pets</ui.Link>
  <ui.Link href="/pets/7">Pet 7</ui.Link>
</nav>
```

## Patch or navigate

A navigation mounts the destination page. A patch updates compatible instances
while retaining state the router can safely keep. The server decides which
instances remain mounted, not the browser. Shared layouts come from
`_layout.vuka.html`; dynamic values come from `[id]` files or folders.

## From handlers and islands

Server handlers may return redirects or send navigation commands. An island's
component context exposes `navigate(href)` and `patch(href)`. Use typed URLs
from generated `pages/urls` where available rather than assembling unescaped paths.

## When it's a page load

A destination outside the live route tree, a file download or an incompatible
frontend needs a document load. Restrict redirects to permitted schemes and
origins; never pass arbitrary visitor input to a trusted redirect.

## Testing

Test both first-load HTML and connected navigation. Verify that a changed path
reruns the checks for that resource, and that retained state cannot expose a
previous visitor's or tenant's data. See [UI testing](/ui/testing).
