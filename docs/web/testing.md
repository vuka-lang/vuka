# Testing Web Apps

[[toc]]

## Introduction

An app is an `http.Handler`, so the standard library's `httptest` tests it.
`web.New(pages.Routes(), …)` receives the generated page table, and each app
has its own container and singletons, so every test can build a fresh one.

This page covers request tests, testing services directly, mail and crons,
and pages driven as visitors use them.

<!-- go:context
type DB struct{}

func OpenDB() *DB { return &DB{} }

type NewPet struct{ Name string }

type Pet struct{ ID int }

type PetStore struct{}

func (s *PetStore) Add(in NewPet) (Pet, error) { return Pet{}, nil }
-->

## Testing Requests

This test serves the app with `httptest` and checks that an unknown pet
answers 404:

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

Test files are `_test.go`; `go test ./...` runs them
(`go tool vuka test ./...` when the package has `.vuka.html` templates).

## Building the App in a Test

These methods and options set up the app a test checks:

| | |
|---|---|
| `app.Build()` | every problem of the app — a test that the wiring is right, with no request |
| `app.Handler()` | the handler; panics when Build fails |
| `web.Resolve[T](app)` | the app's `T`, built with its dependencies: a service to call directly, or to seed |
| `app.Service(fakeConstructor)` | a ready value replaces a constructor: provide the fake instead of the real one |
| `web.Only(pkgs…)` | take only the declarations of these packages (import paths; `"main"` for the main package) |
| `web.NoDotEnv()`, `web.DotEnv(files…)` | which env files a test reads; under `go test`, `.env.test` and `.env` |

See [Dependency Injection](/web/dependency-injection) and
[Configuration](/web/configuration).

### Testing a Service Directly

`web.Resolve` hands the test a service built with its dependencies, to call
without a request:

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

### Limiting a Test to Some Packages

`web.Only` matters when a test binary links packages that declare routes of
their own: a library's tests, or an app whose packages each register a few.

## Testing Mail and Crons {#mail-and-crons}

`mailtest.New()` is a Mailer that keeps what the app sends instead of
sending it, and `mailtest.Last(t, m)` reads it back; see
[Mail](/web/mail#testing). `crontest.Run(app, name)` runs a cron with its
dependencies, and `crontest.NewClock` drives the schedule with a virtual
clock; see [Crons](/web/crons#testing).

## Testing Pages as Visitors Use Them {#viewtest}

Package `viewtest` drives a page the app serves as a visitor does: click,
type, submit, follow links, then check what the page shows. Each handle
serves the app on a test server of its own, closed as the test ends.

<!-- go:context
type Todos struct{ ui.Live }

func (p *Todos) Render() ui.Node { return ui.El("main", nil) }

func todosApp() *web.App {
	app := web.New(web.Only("none"), web.NoDotEnv())
	app.Live("/todos", func() *Todos { return &Todos{} })
	return app
}
-->

```go
package todos_test

import (
	"testing"

	"github.com/vuka-lang/web/v2/viewtest"
)

func TestAddTodo(t *testing.T) {
	p := viewtest.Open(t, todosApp(), "/todos")
	p.Fill("title", "Buy milk").Click("Add")
	p.Expect("#todos li").Text("Buy milk")
	p.Expect(".empty").Absent()
}
```

`viewtest.Open` drives the page over its live connection without a
browser: each action sends the message the runtime would — a click to its
handler, a form's fields on submit, a navigation — and waits for its reply,
and the page the test reads is the markup the server sent, patched as it
arrives. It is fast and needs nothing installed. A page that isn't live is
held as served: its links and forms load the next page, as a browser
without script does.

`viewtest.Browse` opens the same page in a headless Chrome, the real
runtime running, for what only a browser does: [JS commands](/ui/js),
[islands](/web/islands), hooks, focus, what the runtime keeps across
patches (a [`ui.ShowOnce`](/web/live#show-once) value). Its actions are a
person's: a click is the mouse's, `Fill` types key by key. A test without
Chrome is skipped (failed when `CI` is set); `VUKA_CHROME` names the
binary.

```go
func TestAddTodoInChrome(t *testing.T) {
	p := viewtest.Browse(t, todosApp(), "/todos")
	p.Fill("title", "Buy milk").Click("Add")
	p.Expect("#todos li").Text("Buy milk")
}
```

### Locators {#locators}

A locator names an element: a CSS selector (a tag, `#id`, `.class`,
`[attr]`, descendant and `>` combinators), a field's name, or the text of
a button, link or label. `Fill`, `Check` and `Select` read it as a field's
name first; the other actions as a selector first. So `Click("Add")`
clicks the button reading "Add", and `Fill("title", …)` the field named
`title`.

### Actions {#actions}

| | |
|---|---|
| `Click(loc)` | its click handler gets the event; a link moves the page, over the connection when it is a `ui.Link`; a submit button submits its form. A click nothing answers fails the test, as does one only a JS command answers under `Open` |
| `Fill(loc, value)` | sets a field, as typing it and moving on does |
| `Check(loc, on)` | sets a checkbox, or chooses a radio button |
| `Select(loc, option)` | chooses the option whose value, or else text, is `option` |
| `Submit(loc)` | submits the form: the form, a button in it, or a field of it |
| `Navigate(url)` | moves the page as a link does |
| `Hover(loc)` | on a `ui.Link` to another page, renders it [ahead](/web/navigation#prefetch-on-a-live-page), so the click that follows takes it |
| `ScrollTo(loc)` | brings it into view: the [groups loading when seen](/ui/stateful#when-seen) and the [`ui.Lazy` regions with `seen`](/ui/state#lazy) inside it start loading |
| `Press(key, mods…)` | `Browse` only: presses `"Enter"`, `"Escape"`… in the element with the focus |
| `Eval(js, &out)` | `Browse` only: evaluates an expression in the page, awaiting a promise |
| `NewTab(path)` | `Browse` only: opens another tab sharing the cookies, for what one tab's actions do to another's page |

Actions return the page, so they chain. Under `Open` each waits for its
reply; under `Browse` they don't wait for the server, and an `Expect` does.

### Reading the Page {#reading-the-page}

`Text(loc)` is an element's text now, `HTML()` the document as it stands
and `URL()` its path and query, where navigations and redirects moved it.
`Expect(loc)` checks an element, waiting until the page shows what it
should — a reply, a deferred group's patch, a broadcast — and failing with
what it showed last once `viewtest.Timeout` (5 seconds) passes:

| | |
|---|---|
| `Text(want)` | its text, white space squashed, is `want` |
| `Contains(sub)` | its text contains `sub` |
| `Value(want)` | a field's value is `want` |
| `Visible()` | it is on the page and shown |
| `Absent()` | no element matches |

`Wait(what, cond)` waits for any condition the same way:

```go
func TestTwoTodos(t *testing.T) {
	p := viewtest.Open(t, todosApp(), "/todos")
	p.Fill("title", "Buy milk").Click("Add")
	p.Fill("title", "Bake bread").Click("Add")
	p.Wait("two rows", func() bool { return strings.Count(p.HTML(), "<li") == 2 })
}
```

To test as a signed-in user, build the app with
[`authtest.As`](/web/auth#testing). `viewtest.EventStream()`, an option of
`Browse`, runs the page with no WebSocket, over the
[event stream](/web/live#the-event-stream-fallback).
`viewtest.Mount` drives a page's [form](/ui/forms#testing) field by field.

## Testing Live Pages in a Browser {#live-pages-in-a-browser}

[Live components](/web/live) are tested over HTTP for their first render
(the page carries `data-vk-token=`), and with
[`viewtest`](#viewtest) for their events. web's own tests drive a headless
Chrome over the DevTools protocol the same way; see
[`examples/live/main_test.vuka`](https://github.com/vuka-lang/web/blob/main/examples/live/main_test.vuka)
(its Go is what `vuka eject` writes).
