# Folder routes

The `pages/` tree is the page route declaration. Shape files render pages;
logic files implement APIs, guards and middleware. `pages/route.go` is
generated and `main.go` passes `pages.Routes()` to `web.New`.

## The tree

```text
pages/
  _layout.vuka.html          document for every descendant
  index.vuka.html            /
  pets/
    index.vuka.html          /pets
    new.vuka.html            /pets/new
    [id].vuka.html           /pets/{id}
  teams/
    [id]/
      _layout.vuka.html      shared team layout
      index.vuka.html        /teams/{id}
      members.vuka.html      /teams/{id}/members
      api/
        invites.go          /teams/{id}/api/invites
  files/
    [path...].vuka.html      /files/{path...}
  api/
    pets.go                 /api/pets
```

## Path parameters

The bracket names the segment. Its type and validation come from the
matching exported field in the shape:

```html
package pets

type Pet struct {
    ID int64 `validate:"min=1"`
}
<main><h1>Pet {ID}</h1></main>
```

Save as `pages/pets/[id].vuka.html`. A segment that cannot parse or validate
answers 404. A bracket folder groups a shared parameter, layout, sibling pages
and API subtree. Nested bracket folders add parameters. The catch-all
`[path...]` is terminal in its branch. Keep logic in `.go` or `.vuka` files.

## Layouts

`_layout.vuka.html` is an ordinary one-struct shape with a `Children ui.Node`
prop. Layouts nest outermost first and are not routes of their own. A layout
can declare shared path fields and decorated hooks to load what it displays.
Use `@ui.Mount`, `@ui.OnChange` or `@ui.Loads("group")` where appropriate.

## Endpoints

`pages/api/pets.go` exports endpoint functions such as `GET` and `POST`;
the compiler associates their HTTP methods with `/api/pets`. Parameters bind
by type and results are encoded as JSON. Put input structs in logic files.

## Guards

A folder's `guard.go` declares `Guard` and a `Load(ctx, deps…) error` method.
It renders nothing and checks access before pages and endpoints in that subtree.
Path fields are bound before the check. Return a typed error or redirect to
stop the request. Guards run again when a live socket joins; authorize protected
handler actions too.

## Middleware

`middleware.go` declares `Middleware(next http.Handler) http.Handler`, or a
constructor returning `web.Middleware` with provided dependencies. It wraps
requests in that folder and below. Root middleware runs outside child middleware;
guards and layout loading follow before binding, mount and handlers.

## Typed URLs

The generator writes `pages/urls` for links to known routes. Use `vuka routes`
to inspect the generated paths, methods, layouts and conflicts. Static paths
and dynamic segments must not declare the same method at the same URL.

## Startup

```text
app := web.New(pages.Routes(), options…)
app.Service(services.NewStore)
```

This is startup logic in `main.go`. Both the generated route table and service
registrations belong to this app; no side-effect page import is required.
