# Web

[`github.com/vuka-lang/web`](https://github.com/vuka-lang/web) is a web
framework for Vuka. Routes, views, services and controllers are declared with
decorators, handler parameters bind by name, and `web.New()` gathers
everything into an app.

**Go:** a handler is `func(http.ResponseWriter, *http.Request)`; reading a path
parameter, decoding a body, finding the database and writing JSON or HTML is
code in every handler, and the routes are registered by hand in `main`.

**Vuka adds** [declarers](/features/decorators#declaration-decorators) — a
library sees each decorated function with its parameter names — so a handler
says what it needs and returns what it made:

```vuka
@web.Get("/pets/{id}")
func ShowPet(id int, store *PetStore) Result[Pet] { return store.Find(id) }
```

`id` comes from the path, `store` from the container, and the `Result`
becomes JSON, or a 404 when it is an `Err(web.NotFound)`.

```sh
go get github.com/vuka-lang/web
```

::: info
Live components are [ui's](/ui/live) (`github.com/vuka-lang/ui/live`), served
over a WebSocket by web.
:::

## A first app

```vuka
package main

import (
	"log"
	"net/http"

	"github.com/vuka-lang/web"
)

type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

@web.Service
type PetStore struct {
	pets map[int]Pet `inject:"-"`
}

func (s *PetStore) Find(id int) Result[Pet] {
	p, ok := s.pets[id]
	if !ok {
		return Err(web.Status(404, "no pet %d", id))
	}
	return Ok(p)
}

@web.Get("/")
func Home(r *http.Request) ui.Node {
	return <html><body><h1>Pets</h1></body></html>
}

@web.Get("/pets/{id}")
func ShowPet(id int, store *PetStore) Result[Pet] { return store.Find(id) }

func main() {
	if err := web.New().Run(""); err != nil { // :$PORT, else :8080
		log.Fatal(err)
	}
}
```

```sh
vuka run .
curl localhost:8080/pets/1     # {"error":"no pet 1","status":404}
```

`web.New()` takes every route, service and controller declared at init.
`Run` checks the whole app first — each parameter of each route, each view,
the dependency graph — and refuses to serve a broken one, listing every
problem with its source position. It shuts down gracefully on SIGINT and
SIGTERM.

## What's in it

| | |
|---|---|
| [Routes](/web/routes) | `@web.Get`, `Post`, …, controllers, services, plain-Go registration |
| [Parameters and results](/web/parameters) | what binds where, what a result becomes, errors and redirects |
| [Views](/web/views) | `.templ` components, `html/template` files and JSX pages |
| [Dependency injection](/web/dependency-injection) | the container, lifecycles, Build errors |
| [Middleware](/web/middleware) | `@web.Use`, app-wide middleware, decorators that see the request |
| [Live components](/web/live) | stateful components served over a WebSocket |
| [Configuration and .env](/web/configuration) | env files, `web.Env`, required variables |
| [Routers and deployment](/web/deployment) | the router seam, `Handler`, `Run`, `PORT`, `VUKA_ENV` |
| [Testing](/web/testing) | `httptest`, `web.Resolve`, `web.Only` |

Complete apps are in the repository:
[`examples/pets`](https://github.com/vuka-lang/web/tree/main/examples/pets)
(pages, a JSON API, a controller, middleware) and
[`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live)
(a counter, a todo list, a chat) — see [Examples](/examples#pets).
For a database, the [ORM](/orm/) has a web integration:
[`ormweb`](/orm/databases#web-integration).
