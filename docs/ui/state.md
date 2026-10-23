# Client State

[[toc]]

## Introduction

A `ui.State[T]` field holds browser state. You reach for it when a value only
matters in the browser, such as an open panel or a selected tab, because
browser changes need no round trip to the server.

You read it as an ordinary variable in markup and write it by assignment in
an `on…` attribute.

## Declaring Browser State

This component declares three state fields and reads and writes them from its
buttons:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

type Tabs struct {
    open ui.State[bool]
    tab ui.State[string]
    count ui.State[int]
}
</script>

<section>
  <button onClick={open = !open}>Toggle</button>
  <button onClick={tab = "details"; open = true}>Details</button>
  <button onClick={count++}>Count: {count}</button>
  <div hidden={!open} class:selected={tab == "details"}>Details</div>
</section>
```

The zero value is the initial value: false, empty string or zero.

### Shared State

State belongs to each component instance. A package-level
`var theme ui.State[string]` is shared across that browser page. Put such
declarations in a logic file.

## Reading State {#reading}

Use state in children, attributes, conditions and expressions. A browser-state
expression remains reactive in the browser.

It cannot select a Go branch that requires a server execution. Use a reactive
attribute or `ui.Show` for visibility instead.

## Writing State {#writing}

Assignments in one event run in order. You can combine browser changes with a
server call: `onClick={open = false; Save()}`. Inside a live component method,
`p.open = false` sends the browser update.

> [!WARNING]
> Browser state is not durable storage or trusted authorization data.

## Passing State to a Child {#passing-to-a-child}

A child can accept a `ui.State[T]` field so both shapes observe the same
browser value. Ordinary exported fields pass props instead. For state and
function props crossing a frontend boundary, see [Islands](/web/islands).

## Handing a Click to a Child {#click-to-a-child}

An element's `onClick={open = !open}` is a chain of browser commands, a
`ui.JSChain`. A component's prop of that type takes the same statements, so
a page can decide what its child's button does and the child forwards it to
its element:

```html
<script lang="go">
// chip.vuka.html
package components

import "github.com/vuka-lang/ui/v2"

type Chip struct {
	Label   string
	OnClick ui.JSChain
}
</script>

<button class="chip" onClick={OnClick}>{Label}</button>
```

```html
<script lang="go">
// panel.vuka.html
package components

import "github.com/vuka-lang/ui/v2"

type Panel struct {
	manage ui.State[bool]
}
</script>

<section>
  <Chip label="Manage" onClick={manage = !manage}/>
  <ui.Show when={manage}><p>Managing</p></ui.Show>
</section>
```

A prop of type `any` takes them too.

> [!NOTE]
> A prop of a func type runs Go on the server and can't hold browser
> commands: statements there are [E0101](/errors/E0101). Declare it
> `ui.JSChain` to take them.
