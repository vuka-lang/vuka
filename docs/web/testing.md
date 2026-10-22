# Testing

An app is an `http.Handler`, so the standard library's `httptest` tests it.
`web.New(pages.Routes(), …)` receives the generated page table, and each app has its
own container and singletons — so every test can build a fresh one.

<!-- go:context
type DB struct{}

func OpenDB() *DB { return &DB{} }

type NewPet struct{ Name string }

type Pet struct{ ID int }

type PetStore struct{}

func (s *PetStore) Add(in NewPet) (Pet, error) { return Pet{}, nil }
-->


```go
func TestPets(t *testing.T) {
	app := web.New()
	app.Service(OpenDB)
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/pets/9")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("GET /pets/9: %d", res.StatusCode)
	}
}
```

Test files are `_test.go`; `go test ./...` runs them (`go tool vuka test ./...` when the package has `.vuka.html` templates).

| | |
|---|---|
| `app.Build()` | every problem of the app — a test that the wiring is right, with no request |
| `app.Handler()` | the handler; panics when Build fails |
| `web.Resolve[T](app)` | the app's `T`, built with its dependencies: a service to call directly, or to seed |
| `app.Service(fakeConstructor)` | a ready value replaces a constructor: provide the fake instead of the real one |
| `web.Only(pkgs…)` | take only the declarations of these packages (import paths; `"main"` for the main package) |
| `web.NoDotEnv()`, `web.DotEnv(files…)` | which env files a test reads; under `go test`, `.env.test` and `.env` |


```go
func TestStore(t *testing.T) {
	app := web.New()
	app.Service(OpenDB)
	store, err := web.Resolve[*PetStore](app)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Add(NewPet{Name: "Rex"})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("added", p.ID)
}
```

`web.Only` matters when a test binary links packages that declare routes of
their own: a library's tests, or an app whose packages each register a few.

## Mail and crons

`mailtest.New()` is a Mailer that keeps what the app sends instead of
sending it, and `mailtest.Last(t, m)` reads it back — see
[Mail](/web/mail#testing). `crontest.Run(app, name)` runs a cron with its
dependencies, and `crontest.NewClock` drives the schedule with a virtual
clock — see [Crons](/web/crons#testing).

## Live pages in a browser

[Live components](/web/live) are tested over HTTP for their first render (the
page carries `data-vk-token=`), and in a browser for their events. web's own
tests drive a headless Chrome over the DevTools protocol and skip when none
is found; `VUKA_CHROME` names the binary. See
[`examples/live/main_test.vuka`](https://github.com/vuka-lang/web/blob/main/examples/live/main_test.vuka) (its Go is what `vuka eject` writes).
