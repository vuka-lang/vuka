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
