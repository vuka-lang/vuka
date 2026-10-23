# Pages & Layouts

[[toc]]

## Introduction

Every application view is a `.vuka.html` shape file: one struct in its
`<script lang="go">`, and the markup below it.

You use the same form for a reusable component, a page under `pages/` and
the layout that wraps a folder of pages. This page shows each, and how they
get their data.

## Writing a Component

This component renders a pet's name and whatever its caller passes inside
it:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type PetCard struct {
    Name string
    Children ui.Node
}
</script>

<article>
  <h2>{Name}</h2>
  {Children}
</article>
```

Save it as `components/pet_card.vuka.html`, import that package in a page
shape, and use `<components.PetCard name={Name}>…</components.PetCard>`.

## Pages and Layouts

Page shapes live under `pages/`, and the path declares the URL. A folder's
`_layout.vuka.html` wraps its descendants through `Children ui.Node`. See
[Routing](/web/routes) and [Folder Routes](/web/folder-routes).

Start the app with `web.New(pages.Routes(), …)`. Route generation writes
`pages/route.go`.

> [!WARNING]
> `pages/route.go` should not be edited by hand.

## Data and Context

Exported fields are props; unexported fields hold tracked server state or
provided dependencies. Fetch data in decorated hooks and handlers.

Markup can read the implicit `ctx` for request-dependent presentation.

> [!WARNING]
> Protected actions must enforce permissions on the server, whatever the
> markup shows.

## Using templ and Browser Libraries {#interop}

Existing templ libraries can be used as nodes or tags inside shapes; see
[templ Interop](/ui/templ). This does not change the application authoring
form. Browser component libraries are integrated as [Islands](/web/islands).
