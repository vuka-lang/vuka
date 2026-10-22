# Islands

An island is a browser component inside server-rendered shape markup. Import
its `.tsx`, `.ts` or `.vue` file in the Go header and use it as a tag.

```html
package pages

import Chart "../frontend/chart.tsx"

type Index struct { Title string }
<main>
  <h1>{Title}</h1>
  <Chart title={Title} client:visible />
</main>
```

This is `pages/index.vuka.html`. It has one rendering struct; data and form
types belong in logic files. Imported frontend props are checked against the
component's declared props.

## Hydration strategies

Choose when browser code starts: immediate load, idle, visible or a media
condition. Use a client-only strategy for code that cannot render on the server,
with meaningful fallback content. Avoid hydrating a static fragment unnecessarily.

## Reactivity

Server data props update the island when the page changes. Function props call
server handlers; those handlers still validate and authorize their inputs.
A `ui.State[T]` prop can share a browser value between a shape and an island.
Use assignment in the shape's event attributes for browser state.

## Pages and layouts

A full frontend component still belongs inside a page shape under `pages/`.
Dynamic URLs come from `[id]` names and layouts from `_layout.vuka.html`.
The app starts with `web.New(pages.Routes(), …)`; the generated route table
handles page registration.

## Bound and deferred fields

Shape fields can use `@ui.Bind`, `@ui.URL("q")` and `@ui.Defer("group")`.
Use `@ui.OnChange` to react to bound fields and `@ui.Loads("group")` to load
deferred data. `ui.Changed` identifies changes. Put loading and access checks
in decorated header methods; constructor parameters are dependencies, not hooks.

## Build

Use the project's frontend package manifest and lockfile for React, Vue or
other island dependencies. Development serves frontend assets through the dev
pipeline; production builds and embeds the asset manifest. Use `vuka build`
and test a production build as well as the dev server.

## Limits

Only serializable exported data should cross into browser props. Services,
secrets and request contexts remain on the server. Client hydration can run
later than the first HTML render, so the initial markup must remain useful.
