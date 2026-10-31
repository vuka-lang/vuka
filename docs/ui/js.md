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
| `Push(handler, args...)` | call a handler on the server — on a live page's instance over its socket, else on a fresh copy of the page over HTTP — with its arguments in order (Go values, element reads); options `ui.Read()`, `ui.Cache(d)`, `ui.Value(v)`, `ui.Timeout(d)`, `ui.KeepOnError()` ([below](#push)) |
| `Into(t, opts...)` | put the last `Push`'s result into `t` instead of the element itself; `ui.Append()`, `ui.Prepend()` add markup instead of replacing it ([below](#results)) |
| `Debounce(d)`, `Throttle(d)` | the steps after it run debounced or throttled |
| `Key(keys...)` | stop unless a key press is one of `keys` (`"Enter"`, `"Escape"`) |
| `Confirm(msg)` | stop unless the user confirms |
| `If(cond)`, `ElseIf(cond)`, `Else()`, `End()` | branches |
| `When(cond, chain)`, `Else(chain)` | a branch as a block: no `End` ([conditions](#conditions)) |
| `Busy(t?, ui.Timeout(d)?)` | mark an element busy while a post or push is under way ([busy forms](#busy-forms)) |
| `Copy(t)`, `CopyText(s)` | the clipboard: an element's value or text, or a string |
| `ScrollTo(t)` | scroll the element into view |
| `SetValue(t, v)` | set a field's value — a string, a bool for a checkbox, or a read of another field (`ui.Sel("#x").Value()`) — firing `input` and `change` ([below](#set-value)) |
| `Options(t, method, args...)` | load a select's options from a method of the page: `Push(method, args..., ui.Read()).Into(t)` ([below](#options)) |
| `SetText(t, text)` | set the element's text, replacing its content: a string, or a read (`ui.LoadError` in an `OnError`) |
| `Call(name, args...)` | call a JavaScript function of the page ([scripts](/web/assets#scripts)) |
| `OnError(chain)`, `OnSuccess(chain)`, `Timeout(d)` | the last `Push`'s outcome; `ui.LoadError` reads a failure's message ([below](#push-errors)) |
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

`Push(handler, args…)` calls a method of the page, or of a component on
it, on the server. For ordinary browser state, use assignment:
`onClick={open = false; Save()}` closes first and then calls Save. Where
the handler runs follows from the page:

| | A live page or stateful component | A page or component that isn't live |
|---|---|---|
| Runs on | its instance, over the page's socket | a fresh copy of the page, over HTTP to the page's own URL |
| State | reads and changes its fields; the page renders again and is patched | none kept: path and query fields bound, services injected |
| Request | the socket; with `ui.Read()`, an HTTP GET instead | a `POST` carrying the CSRF token; a `GET` with `ui.Read()` |
| Checks | the live session's | the app's middleware and the page's guards, not its `Load` |

Over HTTP, only methods a step names can be called, and only from the page
that renders the step: the compiler registers each one, with no
annotation, and the step carries its reference signed for the page's
route. A call a browser says is cross-site is refused. A reply is never
cached unless the step says so with `ui.Cache(d)`. A component without
state on a live page pushes over that page's socket, its props as rendered.

### Arguments {#push-arguments}

The handler takes a `context.Context` first if it needs one; its pointer
and interface parameters are dependencies the app injects. Every other
parameter takes the next argument, in order:

- a Go value from the render: `o.ID`, `item.SKU`;
- a read the browser makes when the event fires: `ui.This.Value()`,
  `ui.Sel("[name=coupon]").Value()`, `ui.This.Checked()`, `ui.Sel(…).Attr(n)`.
  A read's text converts to the parameter's type (`"3"` to an `int`,
  `"true"` to a `bool`); one that won't fails the push and runs its
  `OnError`;
- `ui.Value(x)`, as the first argument.

Options — `ui.Read()`, `ui.Cache(d)`, `ui.Timeout(d)`, `ui.KeepOnError()` —
are told apart by their type, so they may go anywhere among the arguments.
A handler taking one value, given none, gets the element's value.

```vuka
func (p *Shop) Price(ctx context.Context, sku string, qty int, coupon string, prices *Prices) (string, error)
```

```html
<input name="qty" onInput={ui.JS.Push(Price, item.SKU, ui.This.Value(), ui.Sel("[name=coupon]").Value(), ui.Read()).Into("#price")} />
```

**A struct parameter** — at most one, the handler's payload — given no
argument takes the fields of the form the element is in, bound by their
form names (a `form:` tag, else the field's name) and checked against its
`validate:` tags, as an `onSubmit` handler's form is. Arguments after it
fill the parameters that follow.

```vuka
type Quote struct{ SKU string; Qty int; Coupon string }

func (p *Shop) Quote(ctx context.Context, q Quote, prices *Prices) (string, error)
```

```html
<form>
  <input type="hidden" name="sku" value={item.SKU} />
  <input name="qty" onInput={ui.JS.Push(Quote, ui.Read()).Into("#price")} />
  <input name="coupon" />
</form>
```

The compiler checks the count, each Go value's type, and that each read
converts to its parameter ([E0168](/errors/E0168)).

### Results {#results}

What the handler returns besides an error goes `Into` a target — the
element the event is on, unless `.Into(target)` names another:

| The handler returns | The target |
|---|---|
| nothing, an `error` | nothing: an event (on a page that isn't live, it runs over HTTP) |
| `[]ui.Option`, `[]ui.OptionGroup` | a select's options are replaced; the value stays while still offered, else it is cleared; `change` fires |
| `ui.Node` | its content becomes the markup, morphed in place (focus and typing kept); `ui.Append()` / `ui.Prepend()` add it instead |
| a `string`, number, `bool`, `time.Time` | a field's value is set, firing `input` and `change`; any other element gets it as its text |
| a struct | each field fills the control of its form name inside the target; a field with no control is skipped, and an `Option[T]` left `None` leaves its control alone |
| `ui.Commands` | the commands run |

`Into` on a handler with no result is an error. A later push into a
target wins over an earlier one still on its way.

```vuka
func (p *Contact) RegionsOf(ctx context.Context, country int64, places *Places) ([]ui.Option, error)
func (p *Signup) Lookup(ctx context.Context, postcode string, geo *Geo) (Place, error) // Place{City, Region}
func (p *Orders) Details(ctx context.Context, id int64, orders *Store) (ui.Node, error)
func (p *Alerts) Unsubscribe(ctx context.Context, category int64, alerts *Store) (ui.Node, error)
func (p *Feed) Older(ctx context.Context, before int64, posts *Posts) (ui.Node, error)
func (p *Join) CheckName(ctx context.Context, name string, users *Users) (ui.Commands, error)
```

```html
<!-- dependent select -->
<select name="country" onChange={ui.JS.Push(RegionsOf, ui.This.Value(), ui.Read()).Into("#region")}>…</select>

<!-- a lookup filling several fields -->
<input name="postcode" onChange={ui.JS.Push(Lookup, ui.This.Value(), ui.Read()).Into(ui.Closest("form"))} />

<!-- a row's details, then a dialog -->
<button onClick={ui.JS.Push(Details, ui.Value(o.ID), ui.Read()).Into("#details").OnSuccess(design.OpenDialog("details"))}>View</button>

<!-- a write on a page that isn't live, rendering one list again -->
<button onClick={ui.JS.Push(Unsubscribe, ui.Value(a.CategoryID)).Into("#alerts")}>Remove</button>

<!-- load more -->
<button onClick={ui.JS.Push(Older, ui.Value(last.ID), ui.Read()).Into("#posts", ui.Append())}>Load more</button>

<!-- commands the server decides -->
<input name="username" onInput={ui.JS.Debounce(300*time.Millisecond).Push(CheckName, ui.This.Value(), ui.Read())} />
```

On a live page, what a push put in a place — markup, a value, text,
options — stays as the page renders again, until the server renders that
place differently.

### Reads {#push-reads}

`ui.Read()` makes a push a read: an HTTP GET to the page's URL, on a fresh
copy of the page. Without it a push on a page that isn't live is a `POST`
carrying the CSRF token, which the app checks as it checks a form post —
always safe for a handler that writes. On a live page a read goes over
HTTP instead of the socket, so a heavy lookup doesn't hold up the page's
events, and it leaves the page's state alone. `ui.Cache(d)` lets the
browser keep a read's reply for `d`, for the same call with the same
arguments.

A read must not write: the compiler warns when a read names a method
calling the ORM's `Save`, `Create` or `Delete` ([W0114](/errors/W0114)),
and when a handler on a page that isn't live assigns to the page's fields,
whose copy is thrown away ([W0113](/errors/W0113)).

### Failures {#push-errors}

A push fails when its handler returns an error, a guard refuses it, a read
doesn't convert, or no reply comes within `Timeout` (10s). Its predictions
are taken back and its `OnError` runs; `ui.LoadError` reads the failure's
message there. Without an `OnError` the error is logged and a `vk:error`
event fired. `OnSuccess` runs once the result is in place.

```html
<button onClick={ui.JS.Push(Details, ui.Value(o.ID), ui.Read()).Into("#details").OnError(ui.JS.SetText("#details-error", ui.LoadError))}>View</button>
<p id="details-error"></p>
```

## Loading Options {#options}

`Options(target, method, args…)` is shorthand for
`Push(method, args…, ui.Read()).Into(target)`: a select's options from a
method returning `[]ui.Option` or `[]ui.OptionGroup` (for `<optgroup>`s),
read over HTTP on a page that isn't live as on one that is.

```html
<select id="country" name="country" onChange={ui.JS.Options(ui.Sel("#city"), CitiesOf, ui.This.Value())}>
  <option value="">Choose a country</option>
  <option value="tz">Tanzania</option>
  <option value="ke">Kenya</option>
</select>
<select id="city" name="city">
  <option value="">Choose a city</option>
</select>
```

A leading option whose value is `""` (a placeholder) stays. A struct
form's dependent fields (`depends:` and `<Field>Choices`) need no command:
their choices follow on their own ([choices](/ui/forms#choices)).

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

A command-only shape stays static, as does one whose only server calls are
[`Push`](#push) steps of methods; other server handlers or decorated hooks make
it live. See [Client State](/ui/state) and [Testing Components](/ui/testing).

## Busy Forms {#busy-forms}

`Busy` marks an element while a push or post is in progress. Use a timeout
and failure handling so a failed request cannot leave the form disabled.

## Calling JavaScript

`Call` invokes a script function shipped by the page. Pass serializable values
and clean up instance resources on destroy; see [Scripts](/web/assets#scripts).
