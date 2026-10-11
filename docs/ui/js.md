# JS commands

Some of what a page does on a click needs no server: opening a menu,
showing a dialog, moving focus, copying a value. JS commands are those
changes, written in Go where the element is, and run by the browser at once
— no round trip, no hand-written script. A chain of them is an `on…`
attribute's value:

```vuka
<button onClick={ui.JS.RemoveClass(ui.Ref("menu"), "hidden").Push(c.MenuOpened)}>Open</button>
<div ref="menu" class="hidden">…</div>
```

The click removes `hidden` from the menu in the browser, then tells the
server with `Push` — a call of `c.MenuOpened`, as `onClick={c.MenuOpened}`
would make. A chain without `Push` never reaches the server.

## Chains

`ui.JS` is the empty chain. Each method returns a new chain with one more
step and leaves the one it is called on as it was, so a shared start is
safe to build on:

```vuka
closing := ui.JS.Hide(ui.Ref("dialog")).PopFocus()
<button onClick={closing}>Cancel</button>
<button onClick={closing.Push(c.Save)}>Save</button>
```

The target comes first. When several steps act on one element,
`On(target)` gives a chain whose commands take no target:

```vuka
<button onClick={ui.JS.On(ui.Ref("menu")).RemoveClass("hidden").AddClass("opacity-100").Push(c.MenuOpened)}>Open</button>
```

`On` again switches the target; `.Commands()` goes back to steps naming
their own.

| Command | |
|---|---|
| `AddClass(t, names...)`, `RemoveClass`, `ToggleClass` | classes |
| `SetAttr(t, name, value)`, `RemoveAttr(t, name)`, `ToggleAttr(t, name, v1, v2)` | attributes; `ToggleAttr` sets `v1`, or `v2` when the attribute is `v1` |
| `Show(t, opts...)`, `Hide`, `Toggle` | the style's `display`, and a `hidden` attribute: `ui.Display("flex")` is what `Show` sets (else the declaration is removed, `block` when the element would still be hidden); `ui.Transition(during, from, to)` and `ui.Time(d)` animate it |
| `Transition(t, during, from, to, opts...)` | the transition alone: `during` and `from` added, `from` swapped for `to` on the next frame, all removed after `ui.Time` (200ms) |
| `Focus(t)`, `FocusFirst(t)` | focus the element, or its first focusable descendant |
| `PushFocus(t?)`, `PopFocus()` | remember an element (default: the focused one); focus it again later |
| `Dispatch(t, event, opts...)` | a `CustomEvent`; `ui.Detail(v)`, `ui.NoBubble()` |
| `Exec(t, attr)` | run the commands held in the element's attribute `attr`; point it only at attributes your own markup fills with commands, never at one holding user data |
| `Push(handler, opts...)` | call a handler on the server; `ui.Value(v)` passes `v` instead of the element's value; `ui.Timeout(d)`, `ui.KeepOnError()` |
| `Debounce(d)`, `Throttle(d)` | the steps after it run debounced or throttled |
| `Key(keys...)` | stop unless a key press is one of `keys` (`"Enter"`, `"Escape"`) |
| `Confirm(msg)` | stop unless the user confirms |
| `If(cond)`, `ElseIf(cond)`, `Else()`, `End()` | branches |
| `When(cond, chain)`, `Else(chain)` | a branch as a block: no `End` ([below](#when)) |
| `Busy(t?, ui.Timeout(d)?)` | mark an element busy while a post or push is under way ([below](#busy-forms)) |
| `Copy(t)`, `CopyText(s)` | the clipboard: an element's value or text, or a string |
| `ScrollTo(t)` | scroll the element into view |
| `SetValue(t, v)` | set a field's value (a checkbox's `"true"`), firing `input` and `change` |
| `SetText(t, text)` | set the element's text, replacing its content |
| `Call(name, args...)` | call a JavaScript function of the page ([below](#calling-javascript)) |
| `OnError(chain)`, `OnSuccess(chain)`, `Timeout(d)` | the last `Push`'s outcome ([below](#optimistic-updates)) |
| `Patch(href)`, `Navigate(href)` | move a live page to another URL over its connection: the same page with other parameters, or another page ([navigation](/web/navigation)) |

## Targets and refs

| Target | |
|---|---|
| `ui.This` | the element the attribute is on — the default |
| `ui.Ref("menu")` | the element with `ref="menu"` in the same component instance |
| `ui.Ref("username", "password")` | the elements with either ref |
| `ui.GetAttr("name", "username", "password")` | the instance's elements whose `name` is one of the values |
| `ui.GetAttr("data-step")` | the instance's elements that have the attribute |
| `ui.Closest(".card")` | the closest ancestor (or itself) matching a selector |
| `ui.Inner(".item")` | its descendants matching a selector |
| `ui.Sel("#x")`, `"#x"`, `ui.Sel("#x", ".y")` | every element of the document matching a selector, or any of several |

A target of several parts finds every element any part finds, in page
order, each once. A command acting on one element — `Focus`, `ScrollTo`,
`Copy`, and a read passed to `Call` — takes the first.

`ref="menu"` names an element for commands; it renders as `data-vk-ref`.
A ref is the instance's own: a list of ten `<Dropdown>`s each opens its own
menu, though every one is `ref="menu"`. Prefer refs to selectors — they need
no ids and can't reach into another component. The compiler checks them: a
`ui.Ref("mnu")` in a `Render` whose JSX has no `ref="mnu"` is an error,
`no ref="mnu" in Dropdown's render; did you mean "menu"?`, and each name of
`ui.Ref("username", "pasword")` is checked the same way (a computed
`ref={…}` turns the check off for that function). A ref on an element passed
as children to a stateful component is inside that component's instance, so
the parent's commands don't reach it.

**By attribute.** `ui.GetAttr` picks elements by an attribute they already
have, so a form's fields need no refs:

```vuka
fields := ui.GetAttr("name", "username", "password")
<button onClick={ui.JS.SetAttr(fields, "readonly", "")}>Lock</button>
```

A plain value matches exactly; `ui.Prefix("step-")`, `ui.Suffix(".pdf")`,
`ui.Contains("draft")` and `ui.Word("old")` (one word of a space-separated
list, as classes are) match part of one. Like a ref it stays in the
component instance; `.Page()` widens it to the whole page:
`ui.GetAttr("role", "alert").Page()`. Values are matched as data — a value
holding `"]` or any other selector syntax is never read as a selector.
When the function's JSX writes the attribute literally, the compiler checks
each literal value: `ui.GetAttr("name", "usrname"): no element in Login has
name="usrname"; did you mean "username"?`. Values computed at run time, and
attributes some element sets with an expression, aren't checked.

`GetAttr` picks elements; `t.Attr(name)` (below) reads an attribute of a
target.

## Conditions

`If` and `ElseIf` read a target when they are reached:

```vuka
<input type="checkbox" ref="all" onChange={ui.JS.
	If(ui.Ref("all").Checked()).RemoveClass(ui.Ref("bulk"), "hidden").
	Else().AddClass(ui.Ref("bulk"), "hidden").
	End()} />
```

| | holds when |
|---|---|
| `t.Checked()` | a checked checkbox or radio |
| `t.Value()` | its value isn't empty |
| `t.Value().Eq("a")` | its value is `"a"` |
| `t.Attr("open")` | it has the attribute |
| `t.Is(".open")` | it matches the selector |
| `t.Exists()` | there is such an element |

A read of a target finding several elements holds when every one passes;
`.Some()` makes it hold when at least one does. A target finding nothing
fails every read but `Exists`:

```vuka
fields := ui.GetAttr("name", "username", "password")
ui.JS.If(fields.Value())        // both filled in
ui.JS.If(fields.Value().Some()) // either one
```

**Joining.** `a.And(b, …)`, `a.Or(b, …)`, `a.Not()`, `ui.All(…)` and
`ui.Any(…)` build a condition from others. They group as written:
`a.And(b).Or(c)` holds when a and b do, or c does; nest a call for the
other grouping, `a.And(b.Or(c))`.

```vuka
<input ref="email" onInput={ui.JS.
	When(ui.Ref("email").Value().And(ui.Ref("terms").Checked()), ui.JS.RemoveAttr(ui.Ref("send"), "disabled")).
	Else(ui.JS.SetAttr(ui.Ref("send"), "disabled", ""))} />
```

Branches nest; `ElseIf`, `Else` and `End` belong to the innermost open `If`,
and one in the wrong place panics as the chain is built. Without `End` a
branch runs to the end of the chain.

### When

`When(cond, chain)` is a branch as a block: `chain` runs when `cond`
holds, and the steps after it run either way, with no `End` to write.
`Else(chain)` right after it is its other branch:

```vuka
<input type="checkbox" ref="all" onChange={ui.JS.
	When(ui.Ref("all").Checked(), ui.JS.Show(ui.Ref("bulk"))).
	Else(ui.JS.Hide(ui.Ref("bulk"))).
	Push(c.SelectAll)} />
```

It encodes to the same `if`, `else` and `end` steps as `If`; an `If` left
open inside `chain` ends with it.

## Push

`Push` takes what `onClick` takes — a method value, a closure, a func prop
— and the handler gets its payload the same way ([events](/ui/stateful#events)):
the element's value, a key, a form's fields. `ui.Value(v)` replaces the
value, parsed by the handler's parameter: `Push(c.Pick, ui.Value(7))` calls
`func (c *List) Pick(id int)` with 7. The steps before a push run first; the
server's reply is applied when it comes.

## Optimistic updates

A click that the server will almost always accept — a like, a checked
todo, a deleted row — can show its result at once, before the round trip,
and take it back in the rare case the server refuses. The steps before a
`Push` are its **predictions**:

```vuka
<button onClick={ui.JS.
	AddClass(ui.This, "liked").
	SetText(ui.Ref("likes"), strconv.Itoa(c.likes.Get()+1)).
	Push(c.Like).
	OnError(ui.JS.Show(ui.Ref("like-error"))).
	OnSuccess(ui.JS.Dispatch(ui.This, "liked"))}>
	♥ <span ref="likes">{c.likes.Get()}</span>
</button>
<p ref="like-error" style="display: none">Couldn't like</p>
```

The button turns `liked` and the count goes up the moment it is clicked.
Then the push settles:

- **Success** — the reply carries no error. Nothing is taken back; the
  server's render follows as usual, and where it renders something other
  than the prediction (another visitor's like counted too), the server's
  value wins. Then `OnSuccess`'s chain runs.
- **Failure** — the handler returns an error, the server refuses the event,
  or no reply comes within the push's timeout. Every change the predictions
  made is reverted to what it was before them — except where the server
  has rendered a new value since, which stays. Then `OnError`'s chain runs.

A chain's predictions for a push are the steps since the chain's start, or
since the push before it. So one chain can make two optimistic calls, each
taking back only its own:

```vuka
ui.JS.
	SetText(ui.Ref("status"), "Saved").Push(c.Save).OnError(ui.JS.SetText(ui.Ref("status"), "Not saved")).
	AddClass(ui.Ref("badge"), "synced").Push(c.Sync)
```

`OnError`, `OnSuccess` and `Timeout` belong to the last `Push` before
them; with none, building the chain panics. The steps after the last push
are no prediction: they stay whatever happens.

Deleting a row, put back if the delete fails:

```vuka
<li key={it.ID}>
	{it.Name}
	<button onClick={ui.JS.
		Hide(ui.Closest("li")).
		Push(func() error { return l.Delete(it.ID) }).
		OnError(ui.JS.Show(ui.Ref("delete-error")))}>Delete</button>
</li>
```

When the server removes the row, it is gone for good; when `Delete` returns
an error, the row reappears and the error shows.

**Keeping a prediction.** `Push(c.Save, ui.KeepOnError())` reverts nothing
when the push fails — for a change the user should see even then (a typed
draft, a "not sent" mark) — and still runs `OnError`.

**Timeouts.** A push waits 10 seconds for its reply before it counts as
failed; `Push(c.Like, ui.Timeout(3*time.Second))` or `.Timeout(3*time.Second)`
after it sets another. A reply that comes after its push timed out is
applied as usual — the server's render is still the truth — but its push's
`OnError` already ran, and `OnSuccess` doesn't.

What the server owns is what to predict: the count it will render, the row
it will delete. A prediction the server's render doesn't confirm is replaced
by it; one it never renders differently stays until the next render
changes it.

## Busy forms

`Busy` marks an element busy while what its event started is under way —
a form posting, a push waiting for its reply — so it can't be sent twice
and shows that it is working:

```vuka
<form method="post" class="group" onSubmit={ui.JS.Busy()}>
	<input name="username" required />
	<input name="password" type="password" />
	<button class="group-aria-busy:opacity-50 group-aria-busy:cursor-wait">Sign in</button>
</form>
```

On submit the form gets `aria-busy="true"`; its inputs, textareas and
selects become `readonly` (never `disabled`, so their values are still
posted); its buttons and links get `aria-disabled="true"` and
`tabindex="-1"`; and further clicks and submits on it are ignored. The
browser posts the form as usual — a chain without a `Push` never stops a
submit — and only once the form is valid: an empty required field keeps
it from submitting, so from going busy. The next page replaces it.

Everything `Busy` changed is put back exactly as it was — an attribute it
found already set stays — when:

- a `Push` of the same chain is answered, success or failure: the element
  is usable again before `OnSuccess` or `OnError` runs;
- the timeout given with `ui.Timeout(d)` passes;
- the browser shows the page again from its back/forward cache, as the
  back button may.

A plain post that loads the next page needs none of them. Style it with
Tailwind's `aria-busy:` variants on the element itself, or
`group-aria-busy:` on elements inside one marked `class="group"`.

With a live handler:

```vuka
<form ref="form" onSubmit={ui.JS.Busy().Push(c.Save).OnError(ui.JS.Focus(ui.GetAttr("name", "title")))}>…</form>
```

`Busy(ui.Ref("form"))` marks another element; with several targets each is
marked. `JS.On(t).Busy()` marks `On`'s target.

Marking fields as part of a chain works with the targets above too: the
login form can lock exactly its two fields once both are filled in.

```vuka
fields := ui.GetAttr("name", "username", "password")
ui.JS.If(fields.Value()).SetAttr(ui.Ref("form"), "aria-busy", "true").SetAttr(fields, "readonly", "")
```

## Calling JavaScript

`Call` runs a function of the page — a chart's refresh, a date picker, a
confetti burst — as a step of a chain:

```vuka
<button onClick={ui.JS.Call("confetti", 120)}>Celebrate</button>
<input ref="q" onInput={ui.JS.Debounce(200*time.Millisecond).Call("Search.preview", ui.Ref("q").Value())} />
```

The name is a function registered with the page's runtime, else a dotted
path on `window`:

```js
vuka.register("confetti", (ctx, count) => burst(ctx.el, count))
```

The function gets a context — `ctx.el`, the element the call is on (or
`JS.On(t).Call(…)`'s target), `ctx.event`, `ctx.root`, the component's root
element, and `ctx.target(spec)` — then the arguments: JSON values, or reads
of an element made when the step is reached, `t.Value()`, `t.Checked()`,
`t.Attr(name)` (the attribute's value, or `null`). A name that isn't an
identifier or a dotted path of them panics as the chain is built; nothing
is ever evaluated as code. Write the name as a constant: a name built from
user data could reach any function on `window`.

The chain waits for a promise the function returns. Returning `false` stops
the chain, like a declined `Confirm`. A throw, a rejected promise or an
unknown function stops it and fires a `vk:error` event on the element; when
the call is among a push's predictions, that push fails without reaching
the server — its predictions are taken back and its `OnError` runs:

```vuka
ui.JS.AddClass(ui.This, "busy").Call("Upload.prepare").Push(c.Upload).OnError(ui.JS.Show(ui.Ref("upload-error")))
```

## From the server

A stateful component pushes commands to its own page from a handler, an
`Update`, or `Mount` under a session:

```vuka
func (c *Form) Save(ctx context.Context, f Fields) error {
	if err := c.store.Save(ctx, f); err != nil {
		c.PushJS(ui.JS.Focus(ui.Ref("name")))
		return err
	}
	c.PushJS(ui.JS.Hide(ui.Ref("dialog")).PopFocus())
	return nil
}
```

They run once the reply's changes are on the page, with `ui.This` the
component's root element and refs its own. Pushed chains may be
optimistic too: their pushes settle as a click's do. Rendered without a
session `PushJS` does nothing. `c.PushPatch(href)` and
`c.PushNavigate(href)` push a chain of one `Patch` or `Navigate`: the page
moves once the reply is applied ([navigation](/web/navigation)).

## Server patches

What commands change stays as the browser set it while the server keeps
rendering that attribute as before: the menu stays open when the server
re-renders the dropdown for another reason. When the server renders the
attribute differently — a new `class`, say — its value wins, and the
browser's change is dropped. So an element that the server and commands
both change should have the server decide the state it owns, and leave the
rest to commands.

## Without a session

A chain renders the same with or without a [live session](/ui/live): a page
rendered by `ui.Handler` runs its commands too. Only `Push` needs a session;
without one it does nothing.

## Examples

A dropdown, closing on Escape:

```vuka
type Dropdown struct {
	ui.Live
	Label string
	Items []string
}

func (d *Dropdown) Render() ui.Node {
	return <div class="relative" onKeyDown={ui.JS.Key("Escape").AddClass(ui.Ref("menu"), "hidden")}>
		<button onClick={ui.JS.ToggleClass(ui.Ref("menu"), "hidden")}>{d.Label}</button>
		<ul ref="menu" class="hidden absolute">
			{for _, it := range d.Items { <li>{it}</li> }}
		</ul>
	</div>
}
```

A modal that takes focus and gives it back:

```vuka
func (m *Modal) Render() ui.Node {
	open := ui.JS.PushFocus().Show(ui.Ref("dialog"), ui.Display("flex")).FocusFirst(ui.Ref("dialog"))
	close := ui.JS.Hide(ui.Ref("dialog")).PopFocus()
	return <div>
		<button onClick={open}>Edit</button>
		<div ref="dialog" style="display: none" role="dialog" onKeyDown={ui.JS.Key("Escape").Hide(ui.Ref("dialog")).PopFocus()}>
			<input name="title" />
			<button onClick={close}>Cancel</button>
			<button onClick={close.Push(m.Save)}>Save</button>
		</div>
	</div>
}
```

`Key` stops the chain unless the key matches, so it comes first: a
chain's steps before it run on every key.

A copy button:

```vuka
<input ref="token" value={k.Token} readonly />
<button onClick={ui.JS.Copy(ui.Ref("token")).AddClass(ui.This, "copied")}>Copy</button>
```

Delete, after asking:

```vuka
<button onClick={ui.JS.Confirm("Delete " + it.Name + "?").Push(func() { l.Delete(it.ID) })}>Delete</button>
```

## Testing

The reference client runs commands on its model of the page
([testing](/ui/testing#js-commands)): `Click` and `Fire` run an element's
commands and send its pushes, and `HasClass`, `Attr`, `Visible` read the
result.

`c.Busy(sel)` reports whether a `Busy` mark is on an element, clicks and
submits on it are ignored as in a browser, and `c.PageShow()` stands for
the page coming back from the back/forward cache.

For optimistic pushes, `c.Hold()` keeps the events a test's clicks send
until `c.Deliver()`, so the predicted page can be checked before the
server answers; `c.Pending()` counts the pushes not yet settled,
`c.Advance(d)` moves the client's clock (a push whose timeout passes
fails), and `c.Info(topic, msg)` delivers a broadcast in between. A handler
that returns an error makes the failure path. `c.RegisterFunc(name, fn)`
stands in for a JavaScript function `Call` names: it gets the context and
the arguments, element reads resolved, and returns a result (`false` stops
the chain) or an error, as a throw.

```go
c.Hold()
c.Click("button")
if c.Text("span") != "8" { t.Fatal("not predicted") }
c.Deliver()
if c.Text("span") != "7" || !c.Visible(`[data-vk-ref="like-error"]`) { t.Fatal("not reverted") }
```

The wire format is in the [live protocol](/reference/live-protocol#js-commands).
