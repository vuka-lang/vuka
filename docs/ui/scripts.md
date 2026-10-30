# Component Scripts

[[toc]]

## Introduction

A component script is TypeScript that runs in the browser beside a shape's
markup, once per instance. You reach for one when the page needs code the
server can't run and markup can't say: a third-party library that draws a
chart or makes a list sortable, keyboard shortcuts, a timer, measuring the
DOM, or moving focus after an event.

The script is typed from the shape's Go struct. It calls the instance's Go
methods, reads and writes its browser state, finds the elements its markup
marks with `ref`, and hears the events its Go sends, and the editor
completes and checks each of those names.

## When to Write a Script {#when}

Most interactive UI needs no script. Reach for these first:

| You want | Use |
|---|---|
| a menu, a tab, a disclosure that opens and closes | [browser state](/ui/state): `open ui.State[bool]` and `onClick={open = !open}`, `<ui.Show when={open}>` |
| a class, an attribute or focus changed on a click or a key | [JS commands](/ui/js): `onClick={ui.JS.Toggle(ui.Ref("menu"))}`, `ui.JS.Key("Escape")` |
| something saved or loaded | an [event](/ui/stateful#events): `onClick={Save}` runs the Go method |
| a filter, a tab or a page number kept in the address | [URL state](/web/url-state): `Q string @ui.URL("q")` |

A script is for the rest: code that needs a browser API or an npm package —
a drag-and-drop or chart library, shortcuts that work anywhere on the page,
`setInterval`, `getBoundingClientRect`, focus after the server answers.

## Writing a Script {#writing}

A shape's script is a `<script lang="ts">` after its markup, one per shape.
Its default export is `component<T>`, with `T` the shape's struct, for a
page and a component alike:

```html
<script lang="go">
package components

type Clock struct {
	Zone string
}
</script>

<time ref="now">…</time>

<script lang="ts">
import { component, ref, onDestroy } from 'vuka:component'

export default component<Clock>((el, props, ctx) => {
  const now = ref('now')
  const tick = () => { now.textContent = new Date().toLocaleTimeString('en-GB', { timeZone: props.Zone }) }
  tick()
  let timer = setInterval(tick, 1000)
  onDestroy(() => clearInterval(timer))
  return {
    update(next) {
      clearInterval(timer)
      props = next
      tick()
      timer = setInterval(tick, 1000)
    },
  }
})
</script>
```

The function runs once for each instance, when it appears on the page:

- `el` is the instance's root element, the first element of its markup.
- `props` are the struct's exported data fields as JSON (a `ui.Node`, a
  func and unexported fields stay on the server). Secrets stay there too: a
  form (a field embedding `ui.FormState`, or a `ui.FormOf`) and any field
  tagged `input:"password"`, `"new-password"`, `"current-password"` or
  `"one-time-code"`, at any depth, are never props, so nothing a visitor
  types into a form is written into the page's HTML or a live patch. A
  script reads a form's values from its fields (`el.querySelector('[name=email]')`).
- `ctx` reaches the instance: [the table below](#ctx).

It may return hooks: `update(next)` runs when a re-render changes the
props, `destroy()` when the instance leaves the page. Use `listen` for
listeners and `onDestroy` for timers, observers and library instances;
they clean up when the instance leaves, including in-place navigation.
A returned `destroy()` keeps working too.

`page<T>((el, live, ctx) => …)` is the same as `component<T>`, with
`ctx.live` as its second argument; older page scripts written
`page<T>((el, live) => …)` keep working.

> [!NOTE]
> A page's props are its exported data fields, its `@ui.URL` fields among
> them. Its `update(next)` runs when a patch changes them and keeps the page
> mounted (the page has an `Update` taking `web.URLParams`); without one, a
> patch mounts the page again: `destroy`, then the function, with the new
> props.

## Setup Helpers {#setup}

Import the helpers from `vuka:component` and call them while the setup
function runs:

| Helper | Result |
|---|---|
| `ref('search')` | the element, typed from its tag; `null` only inside an `if`, `switch` branch or slot |
| `ref.all('category')` | a function returning the current loop elements; `.watch(fn)` runs when the set changes after a patch |
| `state('open')` | the state's `get`, `set` and `watch` handle |
| `listen(target, type, fn, options?)` | a listener removed on destruction; returns a function to remove it early |
| `onDestroy(fn)` | cleanup when the instance leaves |
| `on`, `emit`, `navigate`, `patch` | the corresponding `ctx` operation during setup |

Keep elements and handles in variables for callbacks. A helper called in
a timer or after `await` throws in development: `ref() called outside a
component's setup; call it at the top and keep the element`. The `ctx`
forms remain available for operations performed later.

A loop ref uses `ref.all`, because its elements can be added or removed:

```html
<script lang="go">
package components

type Alerts struct { Categories []string }
</script>

<form ref="form">
  for _, name := range Categories {
    <label><input ref="category" type="checkbox" value={name} />{name}</label>
  }
  <span ref="count">0 selected</span>
</form>

<script lang="ts">
import { component, ref, listen } from 'vuka:component'

export default component<Alerts>(() => {
  const checks = ref.all('category')
  const count = ref('count')
  const update = () => {
    count.textContent = `${checks().filter(c => c.checked).length} selected`
  }
  listen(ref('form'), 'change', update)
  checks.watch(update)
})
</script>
```

Here `category` belongs to an `<input ref="category" type="checkbox">`
inside a `for`, and `count` and `form` always render. Calling
`ref('category')` is a type error pointing to `ref.all('category')`.
Unknown names include a suggested spelling. Save the collection function,
not `checks()` at setup, to read the elements after later patches.

## The ctx Reference {#ctx}

| Member | |
|---|---|
| `ctx.live.reorder(ids)` | calls the Go method `Reorder` of the instance (exported, its first letter lowered); a `Promise` resolved once the server has run it and the page is patched, rejected with its error |
| `ctx.live.q` | the field `Q`'s handle: `get()` and `value`; `set(v)` for a field marked `@ui.Bind`, validated on the server |
| `ctx.state('open')` | the `ui.State` field `open`: `get()`, `set(v)` and `watch(fn)`, which returns a function that stops watching |
| `ctx.ref('box')` | the element of the instance's markup with `ref="box"`, typed by its tag (`HTMLInputElement` for an `<input>`), `null` while none renders |
| `ctx.on('saved', fn)` | runs `fn(payload)` for each `PushEvent("saved", payload)` the instance's Go sends |
| `ctx.emit('picked', v)` | components only: calls the handler the parent gave the root's `onPicked` attribute, with `v` |
| `ctx.navigate(href, { replace })` | moves to another page; completes the app's routes |
| `ctx.patch(href, { replace })`, `ctx.patch({ q: 'x' })` | stays on this page with a new URL: an href, or query parameters set on the current one (`null` removes one), a page's `@ui.URL` keys completed |
| `ctx.push(name, payload)` | calls a method by name, for a name built at run time; prefer `ctx.live` |

The names a script passes as strings — refs, states, events — come from
the shape, so the editor completes them and flags one the shape doesn't
have ([W0308](/errors/W0308)).

## Examples {#examples}

### Reordering With Drag and Drop {#drag-and-drop}

A sortable list with [SortableJS](https://github.com/SortableJS/Sortable),
`npm install sortablejs @types/sortablejs` at the module root. The script
lets the visitor drag the rows and sends the new order to the Go method,
which checks it before keeping it:

```html
<script lang="go">
package pages

import (
	"context"
	"errors"
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

// Index is a board whose cards the visitor reorders. Card, in card.go, is
// struct{ ID int; Title string }.
type Index struct {
	ui.Live
	cards []Card
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.cards = []Card{{1, "Write"}, {2, "Test"}, {3, "Ship"}}
	return nil
}

// Reorder keeps the cards in the order of ids, which must name each card once.
func (p *Index) Reorder(ids []int) error {
	byID := map[int]Card{}
	for _, c := range p.cards {
		byID[c.ID] = c
	}
	if len(ids) != len(p.cards) {
		return errors.New("the cards changed: reload the page")
	}
	next := make([]Card, 0, len(ids))
	for _, id := range ids {
		c, ok := byID[id]
		if !ok {
			return errors.New("no card " + strconv.Itoa(id))
		}
		delete(byID, id)
		next = append(next, c)
	}
	p.cards = next
	return nil
}
</script>

<ul ref="list">
  for _, c := range cards {
    <li key={c.ID} data-id={strconv.Itoa(c.ID)}>{c.Title}</li>
  }
</ul>

<script lang="ts">
import Sortable from 'sortablejs'
import { component, ref, onDestroy } from 'vuka:component'

export default component<Index>((el, props, ctx) => {
  const list = ref('list')
  const sortable = Sortable.create(list, {
    onEnd: () => {
      const ids = [...list.children].map((li) => Number((li as HTMLElement).dataset.id))
      ctx.live.reorder(ids).catch((err) => alert(err.message))
    },
  })
  onDestroy(() => sortable.destroy())
})
</script>
```

### Keyboard Shortcuts {#keyboard-shortcuts}

`/` focuses the search box and `n` opens a new draft, anywhere on the
page but in a field being typed in. The listener is on `document`, so
`destroy` removes it:

```html
<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

// Index is an inbox with shortcuts.
type Index struct {
	ui.Live
	drafts int
}

// Compose opens a new draft.
func (p *Index) Compose() { p.drafts++ }
</script>

<main>
  <input ref="search" type="search" placeholder="Search (/)" />
  <button onClick={Compose}>New (n)</button>
  <p>{drafts} drafts</p>
</main>

<script lang="ts">
import { component, ref, listen } from 'vuka:component'

export default component<Index>((el, props, ctx) => {
  const search = ref('search')
  const onKey = (e: KeyboardEvent) => {
    const t = e.target as HTMLElement
    if (e.metaKey || e.ctrlKey || e.altKey || t.closest('input, textarea, select, [contenteditable]')) return
    if (e.key === '/') {
      e.preventDefault()
      search.focus()
    } else if (e.key === 'n') {
      ctx.live.compose()
    }
  }
  listen(document, 'keydown', onKey)
})
</script>
```

### A Chart Fed by Props {#chart}

A component drawing its `Points` with [Chart.js](https://www.chartjs.org/)
(`npm install chart.js`). `update` gives the chart the new points when a
re-render changes them, and `destroy` frees its canvas:

```html
<script lang="go">
package components

// Sales charts a series. Point, in point.go, is
// struct{ Label string; Value float64 }.
type Sales struct {
	Title  string
	Points []Point
}
</script>

<figure>
  <figcaption>{Title}</figcaption>
  <canvas ref="canvas"></canvas>
</figure>

<script lang="ts">
import { Chart } from 'chart.js/auto'
import { component, ref, onDestroy } from 'vuka:component'

export default component<Sales>((el, props, ctx) => {
  const chart = new Chart(ref('canvas'), {
    type: 'line',
    data: {
      labels: props.Points.map((p) => p.Label),
      datasets: [{ label: props.Title, data: props.Points.map((p) => p.Value) }],
    },
  })
  onDestroy(() => chart.destroy())
  return {
    update(next) {
      chart.data.labels = next.Points.map((p) => p.Label)
      chart.data.datasets[0].data = next.Points.map((p) => p.Value)
      chart.update()
    },
  }
})
</script>
```

### Focusing an Input After an Event {#focus}

Adding a task clears the field and puts the cursor back in it, once the
server has kept the task. The Go method says so with `PushEvent`, and the
script hears it with `on`:

```html
<script lang="go">
package components

import (
	"strings"

	"github.com/vuka-lang/ui/v2"
)

// Tasks is a list the visitor adds to.
type Tasks struct {
	ui.Live
	Title string @ui.Bind
	tasks []string
}

// Add keeps the typed task, and tells the script.
func (t *Tasks) Add() {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		return
	}
	t.tasks = append(t.tasks, title)
	t.Title = ""
	t.PushEvent("added", map[string]int{"count": len(t.tasks)})
}
</script>

<form onSubmit={Add}>
  <input ref="title" bind:value={Title} />
  <button>Add</button>
  <ul>
    for i, task := range tasks {
      <li key={i}>{task}</li>
    }
  </ul>
</form>

<script lang="ts">
import { component, ref, on } from 'vuka:component'

export default component<Tasks>((el, props, ctx) => {
  const input = ref('title')
  on('added', (p) => {
    if (input) {
      input.value = ''
      input.focus()
    }
    document.title = `${p.count} tasks`
  })
})
</script>
```

The payload's type comes from the Go: `p.count` is a `number` here.

### Refreshing on a Timer {#timer}

A status panel asks the server for fresh numbers every thirty seconds
while the tab is visible:

```html
<script lang="go">
package components

import (
	"context"
	"time"

	"github.com/vuka-lang/ui/v2"
)

// Status shows the queue's depth.
type Status struct {
	ui.Live
	depth   int
	checked time.Time
}

// Refresh reads the queue again.
func (s *Status) Refresh(ctx context.Context) error {
	s.depth, s.checked = 42, time.Now()
	return nil
}
</script>

<p>{depth} queued, checked at {checked.Format("15:04:05")}</p>

<script lang="ts">
import { component, onDestroy } from 'vuka:component'

export default component<Status>((el, props, ctx) => {
  const timer = setInterval(() => {
    if (document.visibilityState === 'visible') ctx.live.refresh().catch(() => {})
  }, 30_000)
  onDestroy(() => clearInterval(timer))
})
</script>
```

A value the server learns about by itself — a new message, a finished
job — is better pushed from Go than polled.

### Reading State and Patching the URL {#url}

A page whose search box keeps the address in step as the visitor types,
debounced, replacing the history entry; its compact layout is browser
state the script follows:

```html
<script lang="go">
package pages

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
)

// Index searches the catalogue.
type Index struct {
	ui.Live
	Q       string @ui.URL("q")
	compact ui.State[bool]
}

// Update keeps the page mounted on a patch: Q follows the URL.
func (p *Index) Update(web.URLParams) {}
</script>

<main>
  <input ref="q" type="search" value={Q} />
  <button onClick={compact = !compact}>Compact</button>
  <p>Results for {Q}</p>
</main>

<script lang="ts">
import { component, ref, state, listen, onDestroy } from 'vuka:component'

export default component<Index>((el, props, ctx) => {
  const input = ref('q')
  let wait: ReturnType<typeof setTimeout> | undefined
  const onInput = () => {
    clearTimeout(wait)
    wait = setTimeout(() => ctx.patch({ q: input.value }, { replace: true }), 300)
  }
  listen(input, 'input', onInput)
  const stop = state('compact').watch((on) => el.classList.toggle('compact', on))
  onDestroy(() => { clearTimeout(wait); stop() })
  return {
    update(next) {
      if (document.activeElement !== input) input.value = next.Q // back and forward
    },
  }
})
</script>
```

## npm Packages and Bundling {#bundling}

`vuka run` and `vuka build` bundle each shape's script with esbuild into
one ES module per component, loaded the first time an instance renders:

- packages come from a `node_modules` at or above the shape's directory:
  `npm install sortablejs` at the module root;
- relative imports (`./drag.ts`) are bundled from beside the shape;
- `vuka:component` is vuka's own module.

A script imports JavaScript and TypeScript only: put its CSS in the shape's
`<style scoped>` or the app's stylesheet. Code split into chunks loaded later
(`import()` of a separate bundle) isn't supported; for a component built
with a framework (Vue, React, Svelte) use an [island](/web/islands).

## Types and Editor Support {#types}

The `vuka` command writes the scripts' types to
`.vuka/vuka-component.d.ts` from the structs: props, methods (with their Go
doc on hover), handles, states, and the names a script passes as strings —
refs with their elements' types, the events `PushEvent` sends with their
payloads, a component's emitted events, a page's `@ui.URL` keys and the
app's routes.

In VS Code it all works with nothing to install: the extension ships a
TypeScript language server and runs it with VS Code's own TypeScript (the
module's `typescript`, when it has one). `vuka lsp` gives each script to
it, so completion, hover, signature help and errors show in the
`.vuka.html`, and go to definition on `ctx.live.reorder` opens
`func (p *Index) Reorder`; the method's references list the scripts' calls.
Other editors point `vuka lsp` at a server ([Editors](/tools/editors#scripts)).

`vuka vet` checks the same names without an editor, so CI catches a
misspelt ref or event ([W0308](/errors/W0308)).

## Security {#security}

> [!WARNING]
> A script runs in the visitor's browser, and so does anything that calls
> the instance's methods: a visitor can call `Reorder` with any ids, or
> any exported method with any arguments. Check in the Go method that the
> visitor may do what it does, and validate its input, as the
> [drag-and-drop example](#drag-and-drop) does. Never put a secret in a
> prop: props are in the page.

## Troubleshooting {#troubleshooting}

- **`Argument of type '"serch"' is not assignable…`, or W0308 from
  `vuka vet`**: the script names a ref, state or event the shape doesn't
  have. The message names the closest one it does.
- **`live: no handler … takes no push named Reorder`**: the bundle allows
  the methods the script names as `live.reorder(…)` or `ctx.live.reorder(…)`.
  A method reached another way — `const l = ctx.live; l.reorder()`, or a
  name built at run time — isn't allowed; call it through `live.` or
  `ctx.live.`.
- **A listener or timer runs twice after navigating back**: the script
  didn't register it with `listen` or clean it up with `onDestroy` (or `destroy`).
- **`ctx.emit` is a type error in a page's script**: a page has no
  parent tag to emit to; call its own method instead.
- **`update` never runs on a page**: the patch mounts the page again;
  give the page an `Update(web.URLParams)` to keep it mounted.
- **No completion in the script in an editor other than VS Code**: `vuka
  lsp` found no TypeScript language server; see
  [Editors](/tools/editors#scripts).
