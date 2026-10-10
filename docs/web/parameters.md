# Parameters and results

**Go:** a handler reads `r.PathValue("id")`, converts it, decodes the body,
writes the status and encodes the response — the same steps in every handler.

**Vuka adds** binding by name: a handler's parameters say where each value
comes from, and its result says what to answer.

```vuka
@web.Get("/pets")
func SearchPets(q string, page int, store *PetStore) Result[[]Pet] { … }   // ?q=cat&page=2

@web.Put("/pets/{id}")
func UpdatePet(id int, in PetForm, store *PetStore) Result[Pet] { … }       // path, body, container
```

## Parameters

Each parameter binds by its type and name:

| Parameter | From |
|---|---|
| `*http.Request`, `http.ResponseWriter`, `context.Context` | the request; the context is the request's |
| a provided type | the [container](/web/dependency-injection) |
| a scalar: `string`, ints, floats, `bool`, an `encoding.TextUnmarshaler`; `*T` of one (nil when absent), `[]T` (repeated) | the path segment of the same name, else the query parameter `?name=`; absent → the zero value |
| one struct, or pointer to a struct, that isn't provided | the body (below) |

A malformed value answers 400 with a message: `id: "x" is not an integer`.

Problems are found when the app is built, not on the first request: a
scalar with no name, a second struct, a type that can't come from a request
(`chan int`), or a pointer nothing provides — each is a Build error naming
the route, its source position and the parameter.

### The body

One struct parameter takes the body:

- `application/json` (or `+json`): decoded as JSON, up to 10 MB;
- otherwise the form — urlencoded, multipart, or the query string;
- then path segments fill the fields of the same name.

```vuka
type PetForm struct {
	Name string `form:"name" validate:"required"`
	Kind string `json:"kind"`
	Age  int
}
```

A form field binds by the `form` tag, else the `json` tag, else the field's
name, matched ignoring case (`-` leaves a field out). `validate:"required"`
rejects a zero value with 400 `name is required` — the only rule; anything
more is the handler's code.

## Results

A handler returns nothing, `error`, `T`, `(T, error)` or `Result[T]`:

| Result | Response |
|---|---|
| nothing, a nil error | 204 — unless the handler wrote the response itself through its `http.ResponseWriter` |
| `vuka.Node` (JSX, a templ component) | HTML |
| `T` with `@web.Template` | the [view](/web/views), HTML |
| `T` without | JSON, 200 |
| `web.Redirect(url)`, as a value or an error | 302 for GET and HEAD, else 303 |
| a `*web.Error`, wrapped or not | its status and message |
| any other error | 500; its message only in dev |
| a panic | 500 |

A handler that wrote the response and then returns an error can't change it
any more: the error is logged.

Success statuses are fixed (200 and 204); write through
`http.ResponseWriter` for anything else.

## Errors

| | Status |
|---|---|
| `web.NotFound`, `web.Unauthorized`, `web.Forbidden`, `web.Conflict` | 404, 401, 403, 409 |
| `web.BadRequest(msg)` | 400 |
| `web.Status(code, format, args…)` | any |

```vuka
func (s *PetStore) Find(id int) Result[Pet] {
	p, ok := s.pets[id]
	if !ok {
		return Err(web.Status(404, "no pet %d", id))
	}
	return Ok(p)
}

func (s *PetStore) Remove(id int) error {
	…
	return fmt.Errorf("removing pet %d: %w", id, web.NotFound)   // still a 404
}
```

`errors.Is(err, web.NotFound)` matches any `*web.Error` with that status.

An error is an HTML page on an HTML route — one with a view, a `vuka.Node`
result or a redirect result — and `{"error": …, "status": …}` elsewhere.
`web.OnError(func(w, r, e *web.Error, html bool))` replaces both. An error
that isn't a `*web.Error` shows "Internal Server Error" unless the app runs
in dev (`web.Dev()` or `VUKA_ENV=development`), where its message is shown.

## Redirects

`web.Redirect` is a string type that is an `error` and a `vuka.Node`, so it
works as any of a handler's results:

```vuka
@web.Post("/pets")
func AddPet(in NewPet, store *PetStore) (web.Redirect, error) {
	p := store.Add(in)?
	return web.Redirect("/pets/" + strconv.Itoa(p.ID)), nil
}

@web.Get("/old")
func Old() vuka.Node { return web.Redirect("/new") }
```

A POST answered with a redirect is a 303, so the browser follows it with a
GET — the post/redirect/get pattern with nothing more to write.
