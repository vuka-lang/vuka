# Testing

Components are functions and values: render one with `ui.String` and look
at the HTML.

```go
func TestPetRow(t *testing.T) {
	got, err := ui.String(context.Background(), PetRow(Pet{"Rex", 3}))
	if err != nil || got != "<tr><td>Rex</td><td>3</td></tr>" {
		t.Fatalf("%q %v", got, err)
	}
}
```

A `_test.vuka` file can use JSX in the test itself; `vuka test` runs it.

## Stateful components

Drive a [session](/ui/live) directly, or through the reference client:
`github.com/vuka-lang/ui/live/livetest` is a client of both protocol
versions in Go. It keeps what a browser keeps — the page's HTML; the
statics, kept strings and each instance's tree — and applies messages as a
browser runtime must; on a v2 patch it checks that morphing the updated
instances one by one gives the page the trees render.

```go
c, err := livetest.Join(live.NewSession(ctx, page), 2)
m, err := c.Event(ui.Event{Target: "c1:0", Type: "click"})
html := c.HTML()
```

`c.Handlers()` lists the page's handlers and `c.RandomEvent(r, words)`
picks one with a plausible value: ui's own tests drive the same random
events through a v1 and a v2 client and require both to hold the server's
page, byte for byte, after each. Sessions in tests verify skipped
components ([verification](/ui/live#verification)), so a `Render` reading
state that isn't an Assign fails the test that exercises it.

## JS commands

The reference client also runs [JS commands](/ui/js), on a model of the
page: the server's HTML plus what commands changed on its elements, kept
across patches by the [rule browsers follow](/ui/js#server-patches).

```go
c, _ := livetest.Join(live.NewSession(ctx, page), 2)
c.Click(`[data-vk-id="c1"] button`)             // runs the button's commands, sends its pushes
c.Fire("input[name=q]", ui.Event{Type: "keydown", Key: "Escape"})
c.HasClass(`[data-vk-ref="menu"]`, "hidden")    // false: the menu is open
c.Visible("#dialog"); c.Attr("#box", "checked"); c.Text("p")
```

`Click` and `Fire` find the first element matching a selector (tags, `#id`,
`.class`, `[attr]`, `[attr=v]`, descendant and `>`), run the commands or
send the event of the nearest element handling it, and apply the replies,
running the commands they push. Classes, attributes, `show`/`hide`/`toggle`,
`set_value`, conditions, `key`, `confirm` (`c.Confirm` answers; nil confirms),
`dispatch`, `exec`, `copy` (into `c.Clipboard`) and `push` are run;
`debounce` and `throttle` run at once; focus, scrolling and transitions do
nothing. Elements are known by their place in the page.

## Navigation

A click on a `ui.Link`, a `patch` or `navigate` command, `c.Navigate(href)`
and `c.Patch(href)` send the protocol's `navigate` message; the session
answers through its [router](/ui/live#navigation), and `c.URL()` is the
page's URL after it.

```go
s := live.NewSession(ctx, router("/books?page=1").Render)
s.SetRouter(router)
c, _ := livetest.Join(s, 2)
c.Click(".pager .next")                 // a patch: the shelf stays mounted
c.URL()                                 // "/books?page=2"
c.Navigate("/books/7")                  // another page
m, _ := c.Navigate("/books.json")       // m.Type == live.Redirect: a full page load
```

A redirect reply changes `c.URL()` and leaves the page the client holds as
it was: a browser would load a new document there. A navigate drops the optimistic
pushes still waiting, without their hooks.

