# Stateful components

Unexported fields hold tracked server state. Change them with ordinary Go
assignment; the compiler tracks reads and writes. A provided dependency is
injected rather than tracked. Exported fields are props.

Save as `components/counter.vuka.html`:

```html
package components

import (
    "context"
    "github.com/vuka-lang/ui"
)

type Counter struct {
    Start int
    n int
}

@ui.Mount
func (c *Counter) mount(ctx context.Context) error {
    c.n = c.Start
    return nil
}

func (c *Counter) Increment() { c.n++ }
<button onClick={Increment}>{n}</button>
```

The hook and server handler make this live. A shape may also embed `ui.Live`
explicitly. Each mounted instance owns its state; give repeated instances
stable keys. Keep shared application data in a service rather than a page field.

## Hooks

| Decorator | When |
|---|---|
| `@ui.Mount` | Before the instance's first render |
| `@ui.OnChange` | After bound fields change; accepts `ui.Changed` |
| `@ui.Loads("group")` | After initial render to load a deferred group |

Hooks are marked where declared and can be unexported. Handler and hook
parameters are filled by type: context, form data, request, signed-in user,
provided services, and the hook's change set. Unfillable parameters are startup errors.

Field attributes include `@ui.Bind`, `@ui.URL("q")` and `@ui.Defer("group")`.
Use `vuka:"-"` for fields excluded from tracking. Do not mutate server state
while rendering. Keep authorization in handlers and guards.

## Events

Exported methods wired to `on…` run on the server. A form struct binds the
submitted fields and checks its validation tags. Put that struct in a logic file.
A scalar argument can come from the event value; captured IDs can be passed
in a closure: `onClick={func() { Remove(row.ID) }}`.

## Messages

Live components can subscribe to topics and handle typed updates. Topic
membership belongs to the live instance and must be authorized. See
[Live sessions](/ui/live) for session routing and protocol behavior.
