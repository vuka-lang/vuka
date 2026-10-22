# Routes

Declare pages through files under `pages/`, not registration calls. See
[Folder routes](/web/folder-routes) for brackets, catch-alls and layouts.

| File | URL |
|---|---|
| `pages/index.vuka.html` | `/` |
| `pages/pets/index.vuka.html` | `/pets` |
| `pages/pets/[id].vuka.html` | `/pets/{id}` |
| `pages/files/[path...].vuka.html` | `/files/{path...}` |
| `pages/_layout.vuka.html` | Wraps descendants; no URL |

`pages/route.go` is generated. Start with `web.New(pages.Routes(), …)`.
Exported page fields bind path values first, then query values. Their Go
types and validation determine acceptable input.

## APIs

Endpoint functions under `pages/api/` return JSON or typed errors. Input
structs bind form/JSON fields and services come from the container.
For an API outside the folder tree, ordinary `app.Get`, `app.Post` and the
other HTTP registration methods remain available in Go. Keep page rendering
in shape files and page routes in the tree.

## Forms that fail

Validation failures carry field errors and status 422. A handler should preserve
safe submitted values and render feedback through its page state. Never use an
unvalidated browser value as authority for an action.

## Services

Register constructors with `app.Service(NewX)` before Build or Run. Constructor
arguments are dependencies; hook and handler parameters are supplied by type.
An unfillable dependency or ambiguous body binding is a startup error with
source information. See [Parameters](/web/parameters).
