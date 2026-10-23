# JS Commands

[[toc]]

## Introduction

Browser commands perform DOM effects without a server round trip. You reach
for them to show a menu, toggle a class or move focus, where a trip to the
server would add nothing.

You write commands in the event attributes of a `.vuka.html` shape.

## Writing a Command

This menu button toggles the `#menu` element in the browser:

```html
package components

import "github.com/vuka-lang/ui/v2"

type Menu struct{}
<section>
  <button onClick={ui.JS.Toggle("#menu")}>Menu</button>
  <nav id="menu" hidden><a href="/pets">Pets</a></nav>
</section>
```

## Chaining Commands {#chains}

Commands can be chained in order. DOM visibility, classes, focus, attributes,
scrolling and dispatch operate on the selected elements.

Targets may be CSS selectors or component-local refs. Prefer refs when several
instances render the same component.

The commands are:

| Command | |
|---|---|
| `AddClass(t, names...)`, `RemoveClass`, `ToggleClass` | classes |
| `SetAttr(t, name, value)`, `RemoveAttr(t, name)`, `ToggleAttr(t, name, v1, v2)` | attributes; `ToggleAttr` sets `v1`, or `v2` when the attribute is `v1`. Not event handler attributes or `srcdoc`; URL values sanitized ([below](#urls-and-attributes)) |
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
| `When(cond, chain)`, `Else(chain)` | a branch as a block: no `End` ([conditions](#conditions)) |
| `Busy(t?, ui.Timeout(d)?)` | mark an element busy while a post or push is under way ([busy forms](#busy-forms)) |
| `Copy(t)`, `CopyText(s)` | the clipboard: an element's value or text, or a string |
| `ScrollTo(t)` | scroll the element into view |
| `SetValue(t, v)` | set a field's value (a checkbox's `"true"`), firing `input` and `change` |
| `SetText(t, text)` | set the element's text, replacing its content |
| `Call(name, args...)` | call a JavaScript function of the page ([scripts](/web/assets#scripts)) |
| `OnError(chain)`, `OnSuccess(chain)`, `Timeout(d)` | the last `Push`'s outcome ([below](#optimistic-updates)) |
| `Patch(href)`, `Navigate(href)` | move a live page to another URL over its connection: the same page with other parameters, or another page ([navigation](/web/navigation)); `href` a path or an `http`/`https` URL ([below](#urls-and-attributes)) |

### URLs and Attributes {#urls-and-attributes}

A command can't make the page run script it didn't ship with:

- `SetAttr`, `RemoveAttr` and `ToggleAttr` don't touch an event handler
  attribute (`onclick`, any `on…`, in any case) or `srcdoc`. The names are
  the code's own, so a chain naming one panics when it is built, as a chain
  built wrong does; use an `on…` attribute of the element instead.
- A URL attribute's value — `href`, `src`, `action`, `formaction`,
  `xlink:href`, `poster`, `data` — is sanitized as an `href` written in
  markup: a `javascript:` URL becomes
  `about:invalid#TemplFailedSanitizationURL`. Values often come from
  visitors, so they are cleaned, not refused.
- `Patch` and `Navigate` (and `PushPatch`, `PushNavigate`) take a path or an
  `http`/`https` URL; any other becomes that same placeholder, which the
  runtime won't load.

The runtime checks again what it runs, whatever built it — commands held in
an attribute for `Exec`, say: it skips an attribute command naming an event
handler attribute or `srcdoc`, or setting a URL attribute to a
`javascript:`, `data:` or `vbscript:` URL, and loads or navigates to
nothing but `http` and `https` URLs.

## Choosing Targets and Refs {#targets-and-refs}

A command's target picks the elements it acts on:

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

A ref belongs to one component instance; prefer refs to global IDs for repeated components.

## Conditions {#conditions}

Conditions limit a chain to the relevant key, form state or confirmation.

> [!WARNING]
> Commands are browser behavior, not permission checks. Authorize each server
> action.

## Pushing to the Server {#push}

A command chain can push a server handler. For ordinary browser state, use
assignment: `onClick={open = false; Save()}` closes first and then calls Save.
The handler receives typed form/request/service parameters, not browser authority.

## Optimistic Updates {#optimistic-updates}

A predicted UI change can run before the server replies. Keep the authoritative
write and validation on the server and provide failure behavior.

Busy forms
should prevent accidental repeat submission and restore their state when the
request finishes.

## Sending Commands From the Server {#from-the-server}

A live handler can send commands through its live instance. DOM-only effects
are transient. Put data that subsequent renders must preserve in tracked server
fields or browser-state fields. A fresh document navigation creates a new page.

## Server Patches {#server-patches}

A patch reconciles server output with the browser. Stable keys preserve logical
instances.

A command-only shape stays static; server handlers or decorated hooks make it
live. See [Client State](/ui/state) and [Testing Components](/ui/testing).

## Busy Forms {#busy-forms}

`Busy` marks an element while a push or post is in progress. Use a timeout
and failure handling so a failed request cannot leave the form disabled.

## Calling JavaScript

`Call` invokes a script function shipped by the page. Pass serializable values
and clean up instance resources on destroy; see [Scripts](/web/assets#scripts).
