# Views

Every application view is a `.vuka.html` shape file: one struct in its Go
header and markup starting on the first line beginning with `<`.

```html
package components

import "github.com/vuka-lang/ui"

type PetCard struct {
    Name string
    Children ui.Node
}
<article>
  <h2>{Name}</h2>
  {Children}
</article>
```

Save as `components/pet_card.vuka.html`, import that package in a page shape,
and use `<components.PetCard name={Name}>…</components.PetCard>`.

## Pages and layouts

Page shapes live under `pages/`; the path declares the URL. A folder's
`_layout.vuka.html` wraps its descendants through `Children ui.Node`.
Start the app with `web.New(pages.Routes(), …)`. Route generation writes
`pages/route.go`, which should not be edited by hand.

## Data and context

Exported fields are props; unexported fields hold tracked server state or
provided dependencies. Fetch data in decorated hooks and handlers. Markup
can read implicit `ctx` for request-dependent presentation, but protected
actions must enforce permissions on the server.

## Interop

Existing templ libraries can be used as nodes or tags inside shapes; see
[templ interop](/ui/templ). This does not change the application authoring
form. Browser component libraries are integrated as [islands](/web/islands).
