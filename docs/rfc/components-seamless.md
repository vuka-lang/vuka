# RFC: Seamless components

Status: draft, 2026-11-22. Everything here is additive or a deprecation with
a vet warning; no existing template changes meaning silently. Each numbered
section ships on its own (§13 sequences them).

## 1. Motivation

A component today is a `.vuka.html` file: exported fields are props, markup
between the tags fills `Children`, `<slot:name>` fills named holes, a func
field is a scoped slot, `ui.State[T]` crosses the boundary, and the compiler
checks all of it at the tag. The shape of a component is right. What isn't
seamless is the daily tissue around it — measured in a real app of ~56
`.vuka.html` files:

- Handler props are `any` (`On`, `OnClose`, `OnPage`…), checked at render,
  not compile time, and the kit's Button carries both `On any` and
  `OnClick ui.JSChain` with a comment saying which wins.
- `<design.Button on={Remove(o.ID)}>` **calls `Remove` during render** —
  the event-call sugar fires only on attributes spelled `on` + capital, so
  the same expression means two things on `onClick` and on `on`.
- Setting a plain HTML attribute on a component is a literal:
  `attrs={[]ui.Attr{{Name: "form", Value: "rule-" + r.Target}}}` — 25 of
  these in the app; the sugar for event calls doesn't reach inside them.
- No prop defaults: components write `typ()`, `ref()`, `first()` methods
  and negated bools (`Closed` meaning "not dismissible").
- Omitting an attribute needs a nil-returning helper; the app has 11
  `) any {` one-liners like `func inputAttr(s string) any { if s == "" { return nil }; return s }`.
- Class merging exists three times (`ui.MergeClasses`, the kit's private
  `withClass`, a vendored 230-line `cn.go`), and app components still
  concatenate: `class={"rounded-xl border … " + Class}`.
- Ids are threaded by hand (`"rule-kinds-" + r.Target`, `ID + "-title"`)
  and kit defaults collide ("modal", "tabs").
- A live child's `@ui.Mount` runs once, so state derived from a prop goes
  stale when the prop changes; there is no props-changed hook.

And five things components can't say at all:

- a fragment reused twice in one file needs its own file;
- a family (`Tabs`/`Tab`, `Form`/`Field`) can't share state without the
  page wiring every child;
- `bind:value` stops at elements, so a Combobox or money input loses the
  binding the bare `<input>` had;
- one failing component's render fails the whole page;
- a keyed list item appears and leaves abruptly — `transition` only
  animates `show`.

Part I (§2–§6) smooths what exists. Part II (§7–§12) adds the missing
tissue. §14 is a worked example using most of it.

---

## Part I — smoothing what exists

## 2. Handler props: typed, one meaning everywhere

**A component attribute whose field is `ui.Action` or `ui.JSChain` takes
everything an element's `on…` attribute takes, with the same meaning.**
The event-call sugar follows the *field's type*, not the attribute's
spelling, so this is one rule in place of today's four:

```vuka
<design.Button on={Remove(o.ID)}>Remove</design.Button>      <!-- a push, as on <button onClick=…> -->
<design.Button on={open = !open}>Toggle</design.Button>       <!-- browser commands -->
<design.Button on={Save}>Save</design.Button>                 <!-- the method as handler -->
<design.Chip onClick.stop={Select(id)} />                     <!-- modifiers ride along -->
```

- `ui.Action` remains "what an event does, in any form". `ui.JSChain`
  remains browser-only. Nothing changes inside components: they forward
  with `onClick={OnClick}` as today.
- An **`any` handler prop is deprecated**: a vet warning (W-series,
  "declare it ui.Action") on the declaration, and `ui.On(x)` becomes
  unnecessary once the field is typed. The kit and registry drop their
  `On any`/`OnClick` doubles for one `On ui.Action` per component
  (§13 migrates them).
- Payload-taking handlers get a typed form: `ui.ActionOf[T]` accepts a
  method or closure whose payload is `T`, checked at the tag like any
  prop — `OnSelect ui.ActionOf[Order]` ends the render-time
  `ui.CheckHandler` surprises for the DataTable family
  (`OnSelect func(T) any` → `OnSelect ui.ActionOf[T]`).

The sugar also works **inside an attribute set**: wherever a `ui.Attr`
value is built from an event-shaped expression in a template hole, the
same lowering applies (so the escape hatch in §3 never forces
`ui.JS.Push(...)` back into app code).

### Events keep pace with the platform

The pipeline is already open-ended by construction, and this RFC makes
that a stated guarantee rather than an accident:

- the recognizer is `on` + a letter (`transpile/jsx.go isEvent`) — no
  event list;
- the name mapping is `lowercase(trim "on")` (`ui.EventName`) — no table;
- the runtime self-registers: `listenIn` scans rendered markup for
  `data-vk-on-<type>` and adds one capturing document listener per type
  it finds (`web live.js`) — no delegation allowlist, and capture catches
  non-bubbling events too.

So when the platform ships a new element event — as it shipped
`scrollend`, `beforetoggle`, `contentvisibilityautostatechange` — 
`onScrollend={…}` works in every tier with **zero framework changes**.
Three guarantees lock that in:

1. **A conformance test on a fictional event.** `onFroble={Save()}`
   transpiles, renders `data-vk-on-froble`, and a dispatched `froble`
   reaches the handler, in the live and HTTP paths both. Any future
   "known events" list breaks this test the day it is written — the test
   *is* the policy that no such list may sit on the hot path. Editor
   completion may know the DOM's names, but it is generated from the
   TypeScript DOM library at tool build time and is advisory only: an
   unknown event name completes to nothing and still compiles.

2. **The one semantic list shrinks to data and gets a per-site override.**
   `nobubble` (focus, mouseenter, …) decides whether an ancestor's
   handler owns a descendant's event. It stays — bubbling can't be
   feature-tested without dispatching — but a new non-bubbling event no
   longer waits for a runtime release: the `.self` modifier
   (`onNewthing.self={…}`) restricts any handler to its own element, the
   standard escape. The key-filter gate (E0173's key-event list) loosens
   the same way: key modifiers are allowed on any event whose payload
   carries a key at runtime, erroring only on the events known not to.

3. **Exact-name events for what camel case can't spell.** Lowercasing
   covers every built-in element event, but custom elements dispatch
   case-sensitive and punctuated names (`sl-change`, `MDCDialog:closed`),
   and HTML attribute names can't carry case. `on:` takes the name
   literally:

   ```vuka
   <sl-input on:sl-change={Choose(this.value)} />
   <my-widget on:stateChanged.self={Sync()} />
   ```

   The exact name travels beside the handler id (the attribute stays
   case-folded; the value carries the name), `listenIn` registers by it,
   and a `CustomEvent`'s JSON-serializable `detail` rides the payload the
   way an island emit's `Data` does — so third-party web components wire
   up like elements, with modifiers and all.

## 3. Plain attributes on component tags: no more literals

Two additions end the `[]ui.Attr{{Name: …, Value: …}}` ceremony:

1. **`attr:` sets any pass-through attribute by name**, on components and
   elements alike:

   ```vuka
   <design.Button type="submit" attr:form={"rule-" + r.Target}>Save changes</design.Button>
   <design.Button attr:data-tester-open={tester("handoff")} attr:data-payload={samplePayload(r)}>Open tester</design.Button>
   ```

   On a component it appends to `Attrs` (E0106 as today when there is no
   `Attrs` field); on an element it is the attribute, useful when the name
   collides with template syntax. `attr:` is explicit, so it bypasses the
   nearest-field typo check by declared intent.

2. **An attribute value may be omitted by its zeroness**: `?=` writes the
   attribute only when the value isn't the type's zero value:

   ```vuka
   <input id?={ID} placeholder?={Hint} />
   <Card id?={ID}>…</Card>
   ```

   This deletes the app's nil-returning helpers; `Option[T]` keeps working
   for "zero is a real value" cases. (`bool?=` is redundant with bare bool
   attributes and is an error pointing there.)

## 4. Prop defaults, enum literals and stable ids

**Enum literals, checked.** Quoted text on a typed prop already converts
(`<design.Button variant="outline" size="sm">` compiles today — untyped
constant conversion), but a typo compiles too and just styles wrong. For a
prop whose type is a defined string or int type with declared constants,
the compiler now checks the quoted literal against the constants' VALUES —
`variant="outlien"` is an error at the attribute with "did you mean
\"outline\"?" — and the editor completes the legal values after the quote.
The design vocabulary is the value (`"sm"`, `"outline"`), never the Go
constant's name; dynamic values keep the `{design.ButtonVariant(x)}` form,
which stays unchecked by design. A defined type with no declared constants
stays a plain string, uncheckable and unchanged.

**Defaults.** A prop declares its default in the tag that already carries
`required`:

```go
type Button struct {
    Type string `vuka:"default=button"`
    Tone Tone   `vuka:"default=ToneNeutral"`
}
```

The value is parsed as the field's type (string, number, bool, or a
same-package constant). It is applied when the tag leaves the prop out —
not when a caller passes the zero value explicitly, which keeps
`type=""` meaning what it says; a caller who needs "unset" back passes
nothing. Computed defaults stay methods, but the common constant case
stops costing one. (The struct-tag question in the roadmap §2.1 is hereby
settled for prop *metadata*: `required`, `default`, and §5's `class` are
the three the compiler reads; data-binding tags stay gone.)

**Ids.** A field of type `ui.AutoID` renders as a string that is unique on
the page and stable across renders (derived from the instance's key path,
as live ids are). A caller may still set it:

```go
type Dialog struct {
    ID ui.AutoID
}
```

```vuka
<dialog id={ID} aria-labelledby={ID.Suffix("title")}>
  <h2 id={ID.Suffix("title")}>Confirm</h2>
</dialog>
```

Kit and registry components move their colliding fixed defaults
("modal", "tabs") to `ui.AutoID`; `ID + "-title"` threads disappear.

## 5. Fallthrough — one marked element, and one class story

One principle, applied across the tag: **the common case declares
nothing; the compiler routes it to a deterministic element; a marker
relocates it; a declared field switches to explicit mode; a guard keeps
typos out of the implicitness.**

Every component has at most one **forward element**: the template's single
top-level element, or, when there are several roots or the right spot is
an inner element, the one carrying a bare `forward` attribute (two
markers, or several roots and no marker, is a compile error naming them).
What a caller writes on the component's tag and no prop claims goes
there:

- **`class` merges.** The caller's classes combine with the element's own
  through `ui.MergeClasses` (tailwind-merge semantics), so
  `class="border-red-200"` *replaces* the base `border` instead of
  fighting it on stylesheet order. Both sides write a plain `class`:

  ```vuka
  <div class="rounded-xl border bg-card shadow-xs">{Children}</div>
  ```

  ```vuka
  <design.Card class="border-red-200 mt-4">…</design.Card>
  ```

  The kit's private `withClass` and the vendored `cn.go` both dissolve;
  `vuka ui add` stops shipping `cn.go`. `ui.Variants` is untouched — the
  variant table says what a variant means, the merge says how a caller's
  classes compose with it. A component that *computes* with the caller's
  classes declares `Class string` with `vuka:"class"` instead: its one
  use in a `class` attribute lowers through the same merge, fallthrough
  turns off, and a tagged field never used in a class attribute hardens
  W0602 to an error.

- **Plain attributes fall through.** `id`, `title`, `tabindex`, `data-*`,
  `aria-*` on a component tag land on the forward element — no `Attrs`
  field, no `attr:`. Only names that are real HTML/ARIA/data attributes
  qualify, and the nearest-field typo check runs first, so a misspelled
  prop is still a did-you-mean error, never a silent attribute (the
  disease Vue's `$attrs` has). `attr:` stays the explicit form that skips
  the check; a declared `Attrs` field takes everything and turns
  fallthrough off.

- **DOM events fall through.** `onClick` on a component with no matching
  handler prop attaches to the forward element. Typed props (§2) remain
  the contract for semantic events; fallthrough covers the DOM-level ones
  the author didn't anticipate, with the same near-miss guard.

- **`bind:value` reaches the control.** On a component whose forward
  element is a form control, `bind:value={p.Email}` wires it two-way with
  no declaration — the Input wrapper's author writes
  `<input class="h-10 rounded-md border" forward />` and nothing else.
  §9's declared form stays for components that mediate the value, and is
  to be re-read against this section before it is built.

- **`ref` and `disabled` follow.** `ref={x}` resolves to the forward
  element; `disabled` on a wrapper reaches the control, retiring the
  forwarded `Disabled bool` props half a kit declares.

- **`asChild` runs the routing in reverse.** A bare `asChild` on a
  component tag makes the caller's single element child the component's
  root: the forward element's tag is discarded and its attributes are
  donated to the child — `class` through the same merge (child wins per
  utility group), handlers compose (both fire), anything else the child
  keeps. A Button that is really a link stops being a prop
  (`Href`, `Patch`, `Target`, `Download`, …) and becomes an anchor:

  ```vuka
  <design.Button asChild variant="outline">
    <a href="/reports/42" target="_blank" download>Report</a>
  </design.Button>
  ```

  The component author changes nothing. Checked where Radix fails at
  runtime: `asChild` with zero, several or text-only children is a
  compile error at the tag, as is a component with no forward element to
  donate; a component child is an error in the first pass (chaining onto
  its forward element can come later). On live pages the substituted
  root keeps the donated element's identity for the morph — the problem
  Teleport already solves.

Where the principle does **not** go: data props (declared fields are the
component's API), semantic events with payloads (`ui.ActionOf[T]` needs
its type), `key`/`let`/slot syntax (compiler-owned), and §8's give/take
(deliberately declared shared state — a trust boundary, like
`ui.State`'s writability).

## 6. Live children: props that change, state that starts

- **`@ui.OnChange` fires for prop changes too.** `c ui.Changed` already
  names changed bound fields; it now also reports exported fields the
  parent set differently this render (`c.Has("Start")`). The
  Mount-derived-state staleness pattern gets its idiomatic fix:

  ```go
  @ui.OnChange
  func (c *Counter) changed(c ui.Changed) { if c.Has("Start") { c.n = c.Start } }
  ```

- **Browser state can start somewhere.** `@ui.Start(v)` on a `ui.State[T]`
  field gives it an initial value on any shape, static ones included —
  today a non-zero start needs a live `@ui.Mount` (E0149's workaround).

  ```go
  open ui.State[bool] `@ui.Start(true)`
  ```

- **Kept instances (a `keep` attribute).** An embedded live component
  leaving the markup drops its instance today. `keep` on its tag parks the
  instance instead — state, subscriptions — and re-renders it when the tag
  returns (tab panels, wizards): Vue's KeepAlive, scoped to the tag rather
  than matched by name, so nothing is silently unmatched. Parked instances
  are capped per page (least-recently-shown evicted, the live idle trim
  applying as usual) and dropped with the page. A kept component that must
  know (pause a ticker, re-check what it shows) takes the ordinary
  lifecycle: `@ui.OnChange` on return reports the away time; §12's states
  version as always, so a stale parked render reconciles like any other.

- **Method-value sameness is documented behavior, not folklore**: an
  inline closure prop re-renders the child every time; the docs and a vet
  hint (prefer `onCancel={p.Cancel}` over `onCancel={func(){ p.Cancel() }}`)
  make the cheap form the obvious one.

---

## Part II — the missing tissue

## 7. Markup func literals — markup declared where it is used

A func literal written as a template binding takes template markup as its
body. No new keyword: `x := expr` is already a template statement, and the
body is the template-block grammar the file already uses. The binding is an
ordinary Go value of type `func(params) ui.Node` — callable in a hole,
passable to any func prop or scoped slot.

```vuka
<script lang="go">
package pages

type Review struct {
    Submitted []Order
    Flagged   []Order
}
</script>

row := func(o Order) {
  <li class:flagged={o.Flagged}>
    <b>{o.Number}</b> — {o.Total}
    <button onClick={Approve(o.ID)}>Approve</button>
  </li>
}

<section>
  <h2>Submitted</h2>
  <ul>for _, o := range Submitted { {row(o)} }</ul>
  <h2>Flagged</h2>
  <ul>for _, o := range Flagged { {row(o)} }</ul>
</section>
```

Passed to a scoped slot, it replaces a `<slot:… let={…}>` block when the
caller wants to reuse one body — and the anonymous form serves a one-use
slot without naming anything:

```vuka
cell := func(p Person, i int) { <b>{i + 1}.</b> {p.Name} }

<components.List items={Open} row={cell} />
<components.List items={Open} row={func(o Order) { <li>{o.Number}</li> }} />
```

Rules:

- A func literal **bound at template level, or written in an attribute
  whose prop is a func returning `ui.Node`**, parses its body as a template
  block (the position decides, never the body's content); a func literal in
  an event attribute keeps today's meaning. The body has the file's full
  template grammar, its events are checked against the shape exactly as the
  main markup's, and it closes over props, state and methods.
- The type is inferred `func(params) ui.Node`; writing the result type
  (`func(o Order) ui.Node {`) is allowed and means the same. Passing it
  where that func type goes is just passing a value, checked as `let` is
  today.
- Go scoping applies unchanged: declare before use, shadowing is the
  ordinary Go error, and mutual recursion takes the standard
  `var a func(Order) ui.Node` pre-declaration. An unused binding is the
  ordinary unused-variable error.

This is also the answer to "pages full of `fieldClass` constants": a page
that keeps repeating nine identical alert `<div>`s can afford
`alert := func(msg string) { … }` even when promoting it to a shared
component isn't worth it yet.

## 8. Scope values — a family shares state without the page wiring it

A component gives a value to everything rendered inside it by tagging a
field `vuka:"give"`. Any descendant — through any depth of components,
slots and markup funcs — takes it by declaring a field of the same type tagged
`vuka:"take"`. Matching is by Go type; the nearest giver wins, so nested
`<Form>`s scope each field to its own form.

```vuka
<script lang="go">
// tabs.vuka.html
package design

import "github.com/vuka-lang/ui/v2"

type TabState struct{ Active ui.State[string] }

type Tabs struct {
    Value    ui.State[string]
    Children ui.Node
    shared   TabState `vuka:"give"`
}

func (t *Tabs) Rendering() { t.shared = TabState{Active: t.Value} }
</script>

<div class="tabs" role="tablist">{Children}</div>
```

```vuka
<script lang="go">
// tab.vuka.html
package design

type Tab struct {
    Name     string
    Children ui.Node
    tabs     TabState `vuka:"take"`
}
</script>

<button role="tab" class:active={tabs.Active == Name} onClick={tabs.Active = Name}>
  {Children}
</button>
```

The page writes what it means and nothing else:

```vuka
<design.Tabs value={tab}>
  <design.Tab name="pending">Pending</design.Tab>
  <design.Tab name="done">Done</design.Tab>
</design.Tabs>
```

Rules:

- `vuka:"give"` on any field, exported or not; the value rides the render
  context for that subtree and costs nothing when unused. Pointer and
  interface types stay shared; value types copy per render like props.
- A taker with no giver fails the render naming both ends and the real
  candidates from the registry: `design.Tab takes design.TabState: no
  ancestor gives it (design.Tabs does — is the tab outside its
  <design.Tabs>?)`. When the absence is visible in the same file, the
  editor flags the tag; render time is the contract.
- Give/take is a family's own tissue — a form's state, a group's
  selection, a table's column registration. Services keep coming from the
  injector; taking a type the injector provides is a vet warning.

`<Form>`/`<Field>`, `<RadioGroup>`/`<Radio>`, `<Accordion>`/`<Panel>` and
the DataTable's columns drop their prop chains this way, one family per
release.

## 9. `bind:` on a component tag

A component declares `Value ui.Bound[T]` and its tag accepts everything an
element's `bind:value` does — a server field, a `ui.State[T]`, the
`.debounce`/`.throttle` modifiers. Inside, the prop spreads onto the element
that carries the value, or reads and writes like state:

```vuka
<script lang="go">
// money.vuka.html — an input that formats cents on blur
package components

import "github.com/vuka-lang/ui/v2"

type Money struct {
    Value    ui.Bound[int64]
    Currency string `vuka:"default=TZS"`
}
</script>

<div class="money">
  <span>{Currency}</span>
  <input inputmode="decimal" {Value.Input()...} onBlur={Value = round(Value.Get())} />
</div>
```

```vuka
<components.Money bind:value.debounce.300ms={Amount} />
```

Rules:

- The attribute name after `bind:` is the prop's name lower-cased, so a
  component may expose several bindings (`bind:value`, `bind:open`).
- `Value.Input()` spreads as the element binding — value out, writes in,
  the tag's modifiers timing them; `Value.Get()`/`Value = x` are the
  programmatic forms, reaching the page's field or state exactly as the
  element's write would.
- `bind:` on a component without a matching `ui.Bound` prop errors at the
  attribute naming the props it does have. Value-plus-`onChange` keeps
  working; `ui.Bound` is for components that want element parity.

This also retires the exported-`InputClass` workaround: a control that
binds can be a component now, so the class constants stop leaking out of
the design package.

## 10. Boundaries — one region fails, the page stands

`<ui.Boundary>` renders its children; if their render panics or errors —
on first render or a live patch — the boundary renders its `failed` slot in
place, logs the error with page and node, and the rest of the page keeps
serving and patching. `retry` re-renders the children.

```vuka
<ui.Boundary>
  <components.UsageChart points={Points} />
  <slot:failed let={err, retry}>
    <p class="muted">The chart didn't load. <button onClick={retry}>Try again</button></p>
  </slot:failed>
</ui.Boundary>
```

- Without a `failed` slot the boundary renders nothing and logs — the
  fence alone is useful around data-driven markup.
- On a live page an event handler's error inside the boundary re-renders
  it failed instead of dropping the patch. `err` is the full error under
  `vuka dev`, a generic message otherwise — the rule every error surface
  follows.
- Boundaries nest; the nearest catches; outside any boundary, today's
  behavior.

## 11. Transitions on insert and remove

`transition={…}` on any **keyed** element or component makes the morph
animate its insertion and removal: enter when the key first appears, leave
awaited before the node is taken out.

```vuka
<ul>
  for _, n := range Notices {
    <li key={n.ID} transition={ui.Slide(200 * time.Millisecond)}>{n.Text}</li>
  }
</ul>
```

`ui.Fade`, `ui.Slide`, `ui.Scale`, `ui.Animate(…)` are the existing values.
Unkeyed elements morph as today.

The class timing is a contract, not an implementation detail — CSS
libraries depend on it (Vue's six-class lifecycle is the convention apps
know): the *from* classes go on before the node is live and come off one
frame after; the *active* classes hold from the start until the
transition's end event; the *to* classes hold from frame one to the end.
`ui.Animate(during, from, to)` names the same three moments, so the keyed
form and the JS-command form share one vocabulary. The end is the node's
own transition/animation end event; a transition on a nested element
instead needs an explicit duration on the value (`.For(d)`), since the
runtime listens on the keyed node only.

**Moves animate too.** A key reorder is a move, not remove-and-insert: the
runtime knows which keyed nodes were displaced by an insert, remove or
reorder, so it applies a *move* class and lets a CSS transition carry the
node from its old box to its new one (the FLIP pattern). The documented
rule rides along: a leaving node is taken out of layout flow for the move
math, so neighbors glide instead of snapping.

**No modes in the first pass.** Enter and leave run simultaneously; an
out-in sequence means the runtime holding a patch's apply until a leave
finishes, which v1 does not do — stated as a limit, revisited with
evidence. `appear` (animating the first render) is a value option
(`ui.Fade(d).Appear()`), not a default.

---

## 12. One state — merging `ui.State` and `ui.Assign`

Vuka has two state types because it started from the server: `ui.State[T]`
is a browser-owned signal (writes apply without a round trip, the browser's
copy wins on morphs), `ui.Assign[T]` is server-owned tracked state (`Set`
marks it changed, unchanged fields skip their spots) — and a plain
unexported field compiles to an Assign. Client frameworks have one state
concept and distinguish *usage*; the split is why a dialog costs a triplet
today: the payload wants server ownership, the open flag wants browser
ownership, and nothing joins them.

The two merge into one `ui.State[T]`: **one logical value, two replicas,
every write versioned** (Assign's counter, extended to both sides). The
type is the sync contract:

- **A server write** (`s.open = true` in a method, `Set`/`Update`) dirties
  it and rides the next render. The browser adopts it iff the server's
  version advanced past the browser's base — the rule form fields already
  follow ("keeps what the user typed until the server's value changes").
  Change tracking is untouched: guards compare versions, unchanged states
  skip their spots.
- **A browser write** (`onClick={open = !open}`, `ui.JS.Set`) applies
  instantly — today's signal feel — and marks the browser copy dirty.
  **Every event send carries the dirty states first**, so a handler reading
  the field sees the truth as of the event; the machinery is the one that
  already sends pending form changes before a page's other events.
- **Conflicts** resolve by version: a server write after the browser's base
  supersedes; a browser write in flight arrives with the event, so the
  handler reads it before writing over it.

No annotations pick a mode. What the code does decides, and the compiler
sees all of it — the same move as reads deciding reactivity:

1. **Never read by Go code** (rendered and written only in the browser):
   browser writes are never sent. The bare signal, inferred — a
   keystroke-hot draft costs no wire because no server code consumes it.
2. **Read by any live method**: synced. The sync set is minimal by
   construction — wire cost is paid exactly for the states the server
   consumes.
3. **A plain unexported field** stays a server-only tracked value, never
   browser-writable; it is not a signal.
4. **Derived values are expressions** — a template expression over states
   is browser-evaluated and tracked already; a Go method over synced
   states is the server form. The `Derived` option deprecates with
   `Assign`.

The contract is transport-neutral: a live page's dirty states ride its
socket events; a plain page's ride the HTTP channel its pushes and form
posts already use (§2's registered handlers), and the response's render
reconciles by version exactly as a patch does. A browser-local state
behaves identically on both page kinds — never sent.

Two rules keep the inference honest:

- **Escape means synced.** A state passed where the compiler cannot follow
  — an unknown function, an interface, an unanalyzed package — is
  classified synced. Over-syncing costs bytes; under-syncing is a
  correctness bug, so the conservative direction is fixed.
- **The classification is visible.** Hover and `vuka explain` say what a
  state is and why ("synced: read in `(s *Index).toggle`"); inference
  without that is archaeology.

`Notify` survives as pacing on the now-universal flow (run a handler on
sync, debounced or throttled). `ui.Assign[T]` becomes an alias of
`ui.State[T]` with a deprecation note and a W-series vet nudge; plain
fields compile as before, so apps change nothing to keep working.

Costs, stated plainly:

- **Event payloads grow** by the dirty synced states. Rule 1 keeps the
  chatty ones off the wire.
- **A behavior change**: server code that read a signal used to see the
  initial value; now it sees browser truth at event boundaries. Almost
  always the bug being fixed, but observable — it ships behind a release
  note, not silently.
- **Security**: a synced state is browser-writable by definition. Nothing
  authorization-relevant may trust one without a server-side check — true
  of form fields today, now true of every synced state, and the docs say
  it loudly.

What the merge retires — the split's fingerprints across the
vocabulary, each a bridge that stops needing to be learned:

- **Gone**: `Notify` as a channel (it was the hand-built sync; it survives
  only as pacing), `Derived` and memoized derived fields (rule 4: derived
  values are expressions), `Keep` (the version rule subsumes "the browser
  keeps this"), and the generated-code shims (`uigen.Assign`,
  `AssignDirty`, `AssignValue`) — one engine, no translation layer.
- **Reduced to sugar**: `ui.JS.Set` (a method writes the field and it
  propagates; the chain step remains for browser-side sequencing),
  `Shared` (a synced field handed to several components shares naturally),
  and §9's `ui.Bound` — under one state a synced prop is two-way by
  identity, so `Bound` shrinks to the element-spread helper; §9 is to be
  read against this section.
- **Re-platformed later**: the form engine's generation mechanism (loaded
  data under the browser's fields, adopted when the generation advances)
  is a hand-tuned, form-shaped instance of this section's reconciliation;
  forms keep their own machinery through the first pass and move onto
  state versions when the merge has settled.
- **Untouched, correctly**: `@ui.URL` fields (the URL is a third owner,
  not the split), `data-vk-ignore` (DOM ownership), `URLParams`/`Update`
  (navigation), and `ui.Value` in pushes (argument provenance; §2 already
  retired it at `ActionOf` sites).

What the build must pin with tests: a state read only inside another
state's event handler (ordering within one event); a state whose only Go
read is `Mount` (a read is a read: synced); the wire regression proving
rule 1 fires for a keystroke-bound field; and the dialog triplet rewritten
on merged state as the conformance example — with §2's `ui.ActionOf` and a
fresh-per-open overlay over two unified states, the ten-field page drops
to two.

## 13. Sequencing and the kits

Order, cheapest and highest-leverage first:

1. §2 typed handler props + one event-sugar rule (transpiler + vet; the
   footgun fix).
2. §3 `attr:` and `?=` (transpiler only).
3. §4 defaults + checked enum literals + `ui.AutoID`; §5 fallthrough in
   slices — class merge first, then attributes and events, then
   `bind:`/`ref`/`disabled` to the forward element, and `asChild` last —
   the same routing in reverse (ui + transpiler).
4. §7 markup func literals (transpiler + lsp; no runtime change).
5. §11 keyed transitions (runtime morph; attribute already parses).
6. §6 OnChange-for-props + `@ui.Start` (runtime).
7. §10 boundaries (runtime + live patch scoping).
8. §9 `ui.Bound` (ui + transpiler).
9. §8 give/take (render context + registry diagnostics).
10. §12 one state (runtime + wire protocol + the `Assign` deprecation) —
   last: it changes observed semantics, so it rides a release note.

Then the two component sets converge on the new vocabulary — one
`On ui.Action` per component, `ui.AutoID` ids, class fallthrough,
give/take families (Tabs, Dialog, Form, DataTable) — one family per
release, with the registry (`vuka ui add`) updated in the same release so
newly vendored components are born on the new forms. Existing vendored
copies belong to the app and are never rewritten behind its back; a
`vuka ui diff` note in the changelog shows what changed upstream.

Each step lands with its errors, editor diagnostics, docs section and
conformance tests; nothing removes or silently changes an existing surface.

## 14. Worked example — the pieces together

A billing page: tabs that wire themselves, a bound money input with a
default, a row markup func, fading alerts, and a fenced chart.

```vuka
<script lang="go">
package pages

import (
    "context"

    "example.com/app/components"
    "example.com/app/design"
    "github.com/vuka-lang/ui/v2"
)

type Billing struct {
    ui.Live
    tab    ui.State[string]
    Plan   Plan
    Budget int64
    Alerts []Alert
}

func (p *Billing) Save(ctx context.Context, store *Store) error { … }
func (p *Billing) Dismiss(id int64)                             { … }
</script>

alert := func(a Alert) {
  <li key={a.ID} transition={ui.Fade(150 * time.Millisecond)}>
    {a.Text} <button on={Dismiss(a.ID)}>×</button>
  </li>
}

<design.Tabs value={tab}>
  <design.Tab name="plan">Plan</design.Tab>
  <design.Tab name="usage">Usage</design.Tab>
</design.Tabs>

<design.TabPanel for="plan">
  <form onSubmit.prevent={Save()}>
    <components.Money bind:value={Budget} />
    <design.Button type="submit" attr:form?={formID}>Save</design.Button>
  </form>
  <ul>for _, a := range Alerts { {alert(a)} }</ul>
</design.TabPanel>

<design.TabPanel for="usage">
  <ui.Boundary>
    <components.UsageChart plan={Plan} />
    <slot:failed let={err, retry}>
      <p>Usage is unavailable. <button on={retry}>Retry</button></p>
    </slot:failed>
  </ui.Boundary>
</design.TabPanel>
```

No page-side tab plumbing, no change handler on the money input, no
`[]ui.Attr` literal, one list row written once, alerts that fade out, a
button whose `on` means the same as `onClick`, and a chart whose failure
is a sentence instead of a dead page.

## 15. What was considered and set aside

- **Dynamic tags** (`<X is={…}>`): a hole renders any `ui.Node`, so a func
  choosing a component covers it; sugar waits for evidence.
- **Give/take by name as well as type**: by-type with wrapper structs
  stays checkable and greppable; names reintroduce stringly wiring.
- **A `snippet` keyword** (Svelte 5's shape): it would be the only
  non-Go statement in templates, needs its own typing and hoisting rules,
  and has no anonymous form; the func-literal binding gives the same power
  through grammar the templates already have. Hoisting was its one edge,
  and declare-before-use reads fine for markup.
- **A tag form** (`<template:row let={…}>` used as `<row …/>`): two new
  tags, a second spelling of the slot family, and it still needs the
  "it's a func value" story to pass one to a prop.
- **Exported markup funcs**: markup shared across files is a component; one
  unit of cross-file reuse.
- **Calls in browser expressions** (E0124's limit): lifting it means
  shipping a Go subset to the browser; `@ui.From` derived values and §8
  (which removes the worst duplication, the registry Tabs' repeated
  `first()` expression) cover the observed cases.
- **Catching handler errors page-wide**: boundaries scope it; globally it
  is the existing error surface's job.
