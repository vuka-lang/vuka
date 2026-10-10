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
| `Exec(t, attr)` | run the commands held in the element's attribute `attr` |
| `Push(handler, opts...)` | call a handler on the server; `ui.Value(v)` passes `v` instead of the element's value |
| `Debounce(d)`, `Throttle(d)` | the steps after it run debounced or throttled |
| `Key(keys...)` | stop unless a key press is one of `keys` (`"Enter"`, `"Escape"`) |
| `Confirm(msg)` | stop unless the user confirms |
| `If(cond)`, `ElseIf(cond)`, `Else()`, `End()` | branches |
| `Copy(t)`, `CopyText(s)` | the clipboard: an element's value or text, or a string |
| `ScrollTo(t)` | scroll the element into view |
| `SetValue(t, v)` | set a field's value (a checkbox's `"true"`), firing `input` and `change` |

## Targets and refs

| Target | |
|---|---|
| `ui.This` | the element the attribute is on — the default |
| `ui.Ref("menu")` | the element with `ref="menu"` in the same component instance |
| `ui.Closest(".card")` | the closest ancestor (or itself) matching a selector |
| `ui.Inner(".item")` | its descendants matching a selector |
| `ui.Sel("#x")`, `"#x"` | every element of the document matching a selector |

`ref="menu"` names an element for commands; it renders as `data-vk-ref`.
A ref is the instance's own: a list of ten `<Dropdown>`s each opens its own
menu, though every one is `ref="menu"`. Prefer refs to selectors — they need
no ids and can't reach into another component. The compiler checks them: a
`ui.Ref("mnu")` in a `Render` whose JSX has no `ref="mnu"` is an error,
`no ref="mnu" in Dropdown's render; did you mean "menu"?` (a computed
`ref={…}` turns the check off for that function). A ref on an element passed
as children to a stateful component is inside that component's instance, so
the parent's commands don't reach it.

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

Branches nest; `ElseIf`, `Else` and `End` belong to the innermost open `If`,
and one in the wrong place panics as the chain is built. Without `End` a
branch runs to the end of the chain.

## Push

`Push` takes what `onClick` takes — a method value, a closure, a func prop
— and the handler gets its payload the same way ([events](/ui/stateful#events)):
the element's value, a key, a form's fields. `ui.Value(v)` replaces the
value, parsed by the handler's parameter: `Push(c.Pick, ui.Value(7))` calls
`func (c *List) Pick(id int)` with 7. The steps before a push run first; the
server's reply is applied when it comes.

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
component's root element and refs its own. Rendered without a session
`PushJS` does nothing.

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

The wire format is in the [live protocol](/reference/live-protocol#js-commands).
