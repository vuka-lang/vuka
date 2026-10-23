# Parameters & Validation

[[toc]]

## Introduction

Hook and handler arguments are supplied by type, so you declare only what
the call needs. Form DTOs and API data types live in `.go` or `.vuka` logic
files.

This page lists where each kind of parameter comes from, how submitted input
is validated, and how handlers answer with results, errors and redirects.

## Declaring Input

Declare the input as a struct in a logic file, with its binding and
validation tags:

```go
package models

type NewPet struct {
    Name string `form:"name" json:"name" validate:"required,max=80"`
}
```

A page handler can then declare `Create(ctx context.Context, in
models.NewPet, store *services.Pets) error`. The context comes from the
request or session, the struct from the submitted fields, and the registered
service from the container.

## Where Parameters Come From {#parameters}

Each parameter type has one source:

| Type | Binding |
|---|---|
| `context.Context` | Current context |
| Struct or pointer not provided as a service | Form or JSON body |
| `url.Values` or `map[string][]string` | Raw form fields |
| Scalar | Path segment or event value, parsed to its Go type |
| `*http.Request`, `http.ResponseWriter` | HTTP request and writer |
| `auth.User` | Authenticated visitor |
| Provided type | Service container |

The full rule, shared by every hook and handler, is in
[Dependency Injection](/web/dependency-injection#parameters-by-type).

### Page Fields

On a page, exported fields bind matching `[id]` path segments before query
parameters. For multiple path values the compiler has their source names.

A parameter with no source, or ambiguous body structs, is a startup error.

## Validation

Binding uses `form:` / `json:` tags, then field names. `validate:` rules run
on submitted input.

### The Body {#the-body}

Body validation failures answer 422 with field errors. An invalid page path
segment answers 404.

> [!WARNING]
> A value being well-formed does not imply authorization: check ownership
> and permissions in the server handler.

## Results

API endpoints return serializable values as JSON. An error stops normal
result encoding.

Page output comes from the page's shape. Handlers update tracked fields and
return errors rather than constructing markup strings.

## Errors and Redirects {#errors-and-redirects}

### Errors {#errors}

`web.NotFound`, `web.Unauthorized` and `web.Forbidden` carry their HTTP
statuses.

Public errors should contain safe messages; log private failure details on
the server.

> [!WARNING]
> Do not expose database errors or tokens to the visitor.

### Redirects {#redirects}

A `web.Redirect` redirects a GET with 302 and a submitted action with 303.
