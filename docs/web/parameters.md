# Parameters and results

<!-- go:context
type Pet struct {
	ID   int
	Name string
}

type NewPet struct{ Name string }

type PetStore struct{ pets map[int]Pet }

func (s *PetStore) Search(q string, page int) ([]Pet, error) { return nil, nil }
func (s *PetStore) Update(id int, in PetForm) (Pet, error)  { return Pet{ID: id}, nil }
func (s *PetStore) Add(in NewPet) (Pet, error)               { return Pet{Name: in.Name}, nil }

type Videos struct{}

func (v *Videos) Save(ctx context.Context, r io.Reader) error { return nil }
-->

A handler's parameters are filled by type: each says where its value comes
from — path, query, body, container — and its result says what to answer, so
none of them reads `r.PathValue("id")`, converts it, decodes the body or
encodes the response itself.

::: vuka
```vuka
@web.Get("/pets")
func SearchPets(q string, page int, store *PetStore) Result[[]Pet] { … }   // ?q=cat&page=2

@web.Put("/pets/{id}")
func UpdatePet(id int, in PetForm, store *PetStore) Result[Pet] { … }       // path, body, container
```
:::

::: go
```go
// Search is GET /pets's query: from Go, the query comes in a struct.
type Search struct {
	Q    string `form:"q"`
	Page int    `form:"page"`
}

func SearchPets(s Search, store *PetStore) ([]Pet, error) { // ?q=cat&page=2
	return store.Search(s.Q, s.Page)
}

func UpdatePet(id int, in PetForm, store *PetStore) (Pet, error) { // path, body, container
	return store.Update(id, in)
}

func init() {
	web.Routes.Get("/pets", SearchPets)
	web.Routes.Put("/pets/{id}", UpdatePet)
}
```
:::

## Parameters

Each parameter is filled from the one source its type names — the same rule
as a live page's hooks and event handlers
([Parameters, by type](/web/dependency-injection#parameters-by-type)):

| Parameter | From |
|---|---|
| `*http.Request`, `http.ResponseWriter`, `context.Context` | the request; the context is the request's |
| a type registered with `app.Service` or `web.Provide` | the [container](/web/dependency-injection) |
| a `web.ParamFrom` type, such as `*auth.Identity` | its function, for the request |
| a scalar: `string`, ints, floats, `bool`, an `encoding.TextUnmarshaler`; `*T` of one (nil when absent), `[]T` (repeated) | the path segment of the same name, else the query parameter `?name=`; absent → the zero value |
| one struct, or pointer to a struct, that isn't provided | the body (below) |
| `url.Values` | the form's fields, as sent (in place of a struct) |

<span class="vuka-only">The compiler records each handler's parameter
names (`vuka.RegisterParams`), so a scalar binds by its name.</span><span class="go-only">Reflection
can't see parameter names: a handler registered from Go binds its scalars to
the path's segments in order — the last scalar the last segment — and takes
the query in a struct, whose fields bind by their `form:` names.</span>

A malformed value answers 400 with a message: `id: "x" is not an integer`.

Problems are found when the app is built, not on the first request: a
scalar with no segment to bind, a second struct, a type that can't come from
a request (`chan int`, [E0405](/errors/E0405)), or a pointer nothing
provides ([E0407](/errors/E0407)) — each is a Build error at the handler's
file and line, naming the parameter and its type.

### The body

One struct parameter takes the body:

- `application/json` (or `+json`): decoded as JSON, up to 10 MB;
- otherwise the form — urlencoded, multipart, or the query string;
- then path segments fill the fields of the same name.

Every request body is bounded, uploads included: 32 MiB by default,
`web.MaxRequestBody(n)` for the app (negative for no limit), and
<span class="vuka-only">`@web.MaxBody(n)`</span><span class="go-only">the option `web.Attrs(web.MaxBody(n))`</span> for one route. A
longer body answers `413`, whether the binding reads it or a handler reads
`r.Body` itself; a multipart form keeps 10 MB in memory and the rest in
temporary files, never past the limit.

::: vuka
```vuka
@web.Post("/videos")
@web.MaxBody(512 << 20)
func Upload(r *http.Request, store *Videos) error { … }
```
:::

::: go
```go
func Upload(r *http.Request, store *Videos) error { return store.Save(r.Context(), r.Body) }

func init() {
	web.Routes.Post("/videos", Upload, web.Attrs(web.MaxBody(512<<20)))
}
```
:::

::: vuka
```vuka
type PetForm struct {
	Name string `form:"name" validate:"required"`
	Kind string `json:"kind"`
	Age  int
}
```
:::

::: go
```go
type PetForm struct {
	Name string `form:"name" validate:"required"`
	Kind string `json:"kind"`
	Age  int
}
```
:::

A form field binds by the `form` tag, else the `json` tag, else the field's
name, matched ignoring case (`-` leaves a field out).

### Validation

Once bound, the struct's `validate:` tags are checked — the struct's fields,
the structs they hold and slices or maps of them, at any depth — whatever
the request sent it as: JSON, a form, multipart, the query string or path
segments. The rules are those of [a page backed by a
component](/web/islands#validation):

| rule | |
|---|---|
| `required` | not the zero value (`""`, `0`, `false`, nil, an empty slice) |
| `omitempty` | the rest is skipped for the zero value |
| `min=n`, `max=n`, `len=n` | a string's characters, a number's value, a slice's or map's items |
| `oneof=a b c` | one of the values |
| `email` | an email address |

::: vuka
```vuka
type Address struct {
	City string `json:"city" validate:"required,min=2"`
}

type Signup struct {
	Email   string    `json:"email" validate:"required,email"`
	Plan    string    `json:"plan" validate:"omitempty,oneof=free pro"`
	Address Address   `json:"address"`
	Items   []Address `json:"items" validate:"max=5"`
}
```
:::

::: go
```go
type Address struct {
	City string `json:"city" validate:"required,min=2"`
}

type Signup struct {
	Email   string    `json:"email" validate:"required,email"`
	Plan    string    `json:"plan" validate:"omitempty,oneof=free pro"`
	Address Address   `json:"address"`
	Items   []Address `json:"items" validate:"max=5"`
}
```
:::

A failure doesn't call the handler: it answers 422 with each field's
message, keyed by its path — json names for a JSON body (`address.city`,
`items.1.city`), form names otherwise:

```json
{"error": "invalid: address.city: required; email: an email address",
 "fields": {"address.city": "required", "email": "an email address"},
 "status": 422}
```

An HTML route answers with its 422 error page. The error is a
`web.Invalid`, so `web.OnError` reads the fields with `errors.As(e,
&invalid)` — to render the form again with them, say. A rule that isn't one
of these, or a bad value (`min=x`), fails `Build` naming the route. Checks
the tags can't say are the handler's: it returns `web.Invalid{"email":
"already taken"}`, answered the same way.

## Results

A handler returns nothing, `error`, `T`, `(T, error)` or `vuka.Result[T]`:

| Result | Response |
|---|---|
| nothing, a nil error | 204 — unless the handler wrote the response itself through its `http.ResponseWriter` |
| `ui.Node` (<span class="vuka-only">JSX</span><span class="go-only">a component, `ui.HTML`</span>, a templ component) | HTML |
| `T` with <span class="vuka-only">`@web.Template`</span><span class="go-only">the `web.Template` option</span> | the [view](/web/views), HTML |
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
| `web.Invalid{"field": "message"}` | 422, with `fields` |
| `web.Status(code, format, args…)` | any |

::: vuka
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
:::

::: go
```go
func (s *PetStore) Find(id int) (Pet, error) {
	p, ok := s.pets[id]
	if !ok {
		return Pet{}, web.Status(404, "no pet %d", id)
	}
	return p, nil
}

func (s *PetStore) Remove(id int) error {
	if _, ok := s.pets[id]; !ok {
		return fmt.Errorf("removing pet %d: %w", id, web.NotFound) // still a 404
	}
	delete(s.pets, id)
	return nil
}
```
:::

`errors.Is(err, web.NotFound)` matches any `*web.Error` with that status.

An error is an HTML page on an HTML route — one with a view, a `ui.Node`
result or a redirect result — and `{"error": …, "status": …}` elsewhere,
with `"fields"` for a `web.Invalid`.
`web.OnError(func(w, r, e *web.Error, html bool))` replaces both. An error
that isn't a `*web.Error` shows "Internal Server Error" unless the app runs
in dev (`web.Dev()` or `VUKA_ENV=development`), where its message is shown.

## Redirects

`web.Redirect` is a string type that is an `error` and a `ui.Node`, so it
works as any of a handler's results:

::: vuka
```vuka
@web.Post("/pets")
func AddPet(in NewPet, store *PetStore) (web.Redirect, error) {
	p := store.Add(in)?
	return web.Redirect("/pets/" + strconv.Itoa(p.ID)), nil
}

@web.Get("/old")
func Old() ui.Node { return web.Redirect("/new") }
```
:::

::: go
```go
func AddPet(in NewPet, store *PetStore) (web.Redirect, error) {
	p, err := store.Add(in)
	if err != nil {
		return "", err
	}
	return web.Redirect("/pets/" + strconv.Itoa(p.ID)), nil
}

func Old() ui.Node { return web.Redirect("/new") }

func init() {
	web.Routes.Post("/pets", AddPet)
	web.Routes.Get("/old", Old)
}
```
:::

A POST answered with a redirect is a 303, so the browser follows it with a
GET — the post/redirect/get pattern with nothing more to write.
