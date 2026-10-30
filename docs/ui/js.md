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
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Menu struct{}
</script>

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
| `SetValue(t, v)` | set a field's value — a string, a bool for a checkbox, or a read of another field (`ui.Sel("#x").Value()`) — firing `input` and `change` ([below](#set-value)) |
| `Options(t, method, args...)` | load a select's options from a method of the page over HTTP ([below](#options)) |
| `SetText(t, text)` | set the element's text, replacing its content |
| `Call(name, args...)` | call a JavaScript function of the page ([scripts](/web/assets#scripts)) |
| `OnError(chain)`, `OnSuccess(chain)`, `Timeout(d)` | the last `Push`'s or `Options`' outcome ([below](#optimistic-updates)) |
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

## Loading Options {#options}

`Options(target, method, args…)` fills a select from a method of the page,
or of a component on it — a city list following the country — with no
connection: the runtime asks the page's own URL over HTTP, so it works on
a page that isn't live as on one that is.

```html
<script lang="go">
package pages

import (
    "context"

    "example.com/app/geo"
    "github.com/vuka-lang/ui/v2"
)

type Checkout struct{}

func (p *Checkout) CitiesOf(ctx context.Context, country string, g *geo.Geo) ([]ui.Option, error) {
    return g.Cities(ctx, country)
}
</script>

<form method="post">
  <select id="country" name="country" onChange={ui.JS.Options(ui.Sel("#city"), CitiesOf, ui.This.Value())}>
    <option value="">Choose a country</option>
    <option value="tz">Tanzania</option>
    <option value="ke">Kenya</option>
  </select>
  <select id="city" name="city">
    <option value="">Choose a city</option>
  </select>
</form>
```

- **The method** returns `[]ui.Option`, or `[]ui.OptionGroup` for
  `<optgroup>`s, and maybe an error. It takes a `context.Context` first if
  it needs one, then the command's arguments — the parameters that aren't
  pointers or interfaces, in order — then its dependencies, pointers and
  interfaces the app injects. An argument is a Go value, or a read made
  when the step runs: an element's value, `ui.This.Value()` (a string,
  parsed for a number), or `ui.Sel("#gift").Checked()` (a bool).
- **No annotation.** The compiler registers each method an `Options`
  command names, and checks the command's arguments against it
  ([E0168](/errors/E0168)). Only those methods can be called this way, and
  only from the page that renders the command: the command carries the
  method's reference signed for the page's route.
- **What runs.** The call is a GET of the page's URL. The app's middleware
  and the route's run (who the visitor is), then the page's guards, then
  the method — not the page's `Load`, nor its layouts'. The method runs on
  a fresh page, its path and query fields bound, or a fresh component: not
  on what the page rendered, so it takes what it needs as arguments and
  dependencies. A component's method is called on the page that renders
  it, under that page's guards.
- **The reply** replaces the select's options; a leading option whose
  value is `""` (a placeholder) stays. The value stays when it is still
  offered, else it is cleared; then the select fires `change`, so a select
  loading another's options from its own chains on, and a script enhancing
  the select sees its new options. On a live page the options stay as the
  page re-renders, while the server renders the ones it did.
- **Failures** — the method's error, a guard refusing, no reply within
  `Timeout` (10s) — run the chain's `OnError`; without one the error is
  logged (and a `vk:error` event fired) and the options stay as they were.
  `OnSuccess` runs once they are in place. A later load of the same select
  wins over an earlier one still on its way.

```html
<select name="country" onChange={ui.JS.Debounce(100*time.Millisecond).Options(ui.Ref("city"), CitiesOf, ui.This.Value()).OnError(ui.JS.Show(ui.Ref("cities-error")))}>…</select>
<select ref="city" name="city"></select>
<p ref="cities-error" hidden>Couldn't load the cities.</p>
```

A struct form's dependent fields (`depends:` and `<Field>Choices`) need
no command: their choices follow on their own
([choices](/ui/forms#choices)).

## Setting Values {#set-value}

`SetValue(target, value)` sets a field's value and fires `input` and
`change` on it, so its own `on…` attributes and the form's checks run. The
value is a string, a bool for a checkbox, or a read of another field, made
when the step runs. A select takes the option of that value; a checkbox is
checked by `true`, or by a read of another's `Checked()`.

```html
<input id="same" type="checkbox" onChange={ui.JS.If(ui.This.Checked()).SetValue(ui.Sel("#ship-city"), ui.Sel("#bill-city").Value()).SetValue(ui.Sel("#ship-country"), ui.Sel("#bill-country").Value()).End()} />
```

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
