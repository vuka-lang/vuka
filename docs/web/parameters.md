# Parameters and results

Hook and handler arguments are supplied by type. Declare only what the call
needs. Form DTOs and API data types live in `.go` or `.vuka` logic files.

```go
package models

type NewPet struct {
    Name string `form:"name" json:"name" validate:"required,max=80"`
}
```

A page handler can declare `Create(ctx context.Context, in models.NewPet,
store *services.Pets) error`. The context comes from the request/session,
the struct from the submitted fields, and the registered service from the container.

## Parameters

| Type | Binding |
|---|---|
| `context.Context` | Current context |
| Struct or pointer not provided as a service | Form or JSON body |
| `url.Values` or `map[string][]string` | Raw form fields |
| Scalar | Path segment or event value, parsed to its Go type |
| `*http.Request`, `http.ResponseWriter` | HTTP request and writer |
| `auth.User` | Authenticated visitor |
| Provided type | Service container |

On a page, exported fields bind matching `[id]` path segments before query
parameters. For multiple path values the compiler has their source names.
A parameter with no source, or ambiguous body structs, is a startup error.

## Validation

Binding uses `form:` / `json:` tags then field names; `validate:` rules run
on submitted input. Body validation failures answer 422 with field errors.
An invalid page path segment answers 404. A value being well-formed does not
imply authorization: check ownership and permissions in the server handler.

## Results

API endpoints return serializable values as JSON. An error stops normal result
encoding. Page output comes from the page's shape; handlers update tracked
fields and return errors rather than constructing markup strings.

## Errors and redirects

`web.NotFound`, `web.Unauthorized` and `web.Forbidden` carry their HTTP statuses.
A `web.Redirect` redirects a GET with 302 and a submitted action with 303.
Public errors should contain safe messages; log private failure details on the
server. Do not expose database errors or tokens to the visitor.
