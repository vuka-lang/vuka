# Client state

A `ui.State[T]` field holds browser state. Read it as an ordinary variable
in markup and write it by assignment in an `on…` attribute. Browser changes
need no round trip.

```html
package components

import "github.com/vuka-lang/ui"

type Tabs struct {
    open ui.State[bool]
    tab ui.State[string]
    count ui.State[int]
}
<section>
  <button onClick={open = !open}>Toggle</button>
  <button onClick={tab = "details"; open = true}>Details</button>
  <button onClick={count++}>Count: {count}</button>
  <div hidden={!open} class:selected={tab == "details"}>Details</div>
</section>
```

The zero value is the initial value: false, empty string or zero. State
belongs to each component instance; a package-level `var theme ui.State[string]`
is shared across that browser page. Put such declarations in a logic file.

## Reading

Use state in children, attributes, conditions and expressions. A browser-state
expression remains reactive in the browser. It cannot select a Go branch that
requires a server execution; use a reactive attribute or `ui.Show` for visibility.

## Writing

Assignments in one event run in order. Combine browser changes with a server
call: `onClick={open = false; Save()}`. Inside a live component method,
`p.open = false` sends the browser update. Browser state is not durable storage
or trusted authorization data.

## Passing to a child

A child can accept a `ui.State[T]` field so both shapes observe the same
browser value. Ordinary exported fields pass props instead. For state and
function props crossing a frontend boundary, see [Islands](/web/islands).
