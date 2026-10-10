# Testing

An app is an `http.Handler`, so the standard library's `httptest` tests it.
`web.New()` takes a snapshot of what has been declared, and each app has its
own container and singletons — so every test can build a fresh one.

```vuka
func TestPets(t *testing.T) {
	app := web.New(web.Provide(OpenDB))
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

Test files are `.vuka` like the rest (`main_test.vuka`); run them with
`vuka test ./...`.

| | |
|---|---|
| `app.Build()` | every problem of the app — a test that the wiring is right, with no request |
| `app.Handler()` | the handler; panics when Build fails |
| `web.Resolve[T](app)` | the app's `T`, built with its dependencies: a service to call directly, or to seed |
| `web.Provide(fake)` | a ready value replaces a constructor: provide the fake instead of the real one |
| `web.Only(pkgs…)` | take only the declarations of these packages (import paths; `"main"` for the main package) |
| `web.NoDotEnv()`, `web.DotEnv(files…)` | which env files a test reads; under `go test`, `.env.test` and `.env` |

```vuka
func TestStore(t *testing.T) {
	app := web.New(web.Provide(OpenDB))
	store, err := web.Resolve[*PetStore](app)
	if err != nil {
		t.Fatal(err)
	}
	match store.Add(NewPet{Name: "Rex"}) {
	case Ok(p):
		t.Log("added", p.ID)
	case Err(e):
		t.Fatal(e)
	}
}
```

`web.Only` matters when a test binary links packages that declare routes of
their own: a library's tests, or an app whose packages each register a few.

## Live pages in a browser

[Live components](/web/live) are tested over HTTP for their first render (the
page carries `data-vk-token=`), and in a browser for their events. web's own
tests drive a headless Chrome over the DevTools protocol and skip when none
is found; `VUKA_CHROME` names the binary. See
[`examples/live/main_test.vuka`](https://github.com/vuka-lang/web/blob/main/examples/live/main_test.vuka).
