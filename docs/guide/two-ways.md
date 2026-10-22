# Source files

Use one file type for each job:

| File | Job |
|---|---|
| `.vuka.html` | Pages, layouts, components and email markup; one struct in a Go header, then markup |
| `.go` | Ordinary Go logic, services, models, API handlers and startup |
| `.vuka` | Logic using Result, Option, match, enums, overloading or decorators |

All three share package scope. Rendering lives in shape files. The first line
starting with `<` separates the Go header from markup. Exported fields are
props, unexported fields are server state or dependencies, and `ui.State[T]`
is browser state written by assignment in event attributes.

Page routes come from `pages/`, bracket names and `_layout.vuka.html`.
`pages/route.go` is generated; main constructs `web.New(pages.Routes(), …)`.
See [Templates](/ui/templates) and [Getting started](/web/getting-started).
