# Live components

A live page keeps its server instance while the browser is connected. Its
initial render is HTML; later events call exported methods and return patches.
Save this as `pages/counter.vuka.html`:

```html
package pages

import (
    "context"
    "github.com/vuka-lang/ui"
)

type Counter struct { n int }

@ui.Mount
func (p *Counter) mount(ctx context.Context) error { p.n = 0; return nil }

func (p *Counter) Increment() { p.n++ }
<main><button onClick={Increment}>Count: {n}</button></main>
```

The hook and server event make this live automatically; embedding `ui.Live`
also marks a live shape. `pages/route.go` is generated and the app receives
`pages.Routes()` at startup.

## Events

Handler parameters are filled by type. A form DTO binds submitted fields,
`context.Context` carries the session context, `*http.Request` provides the
request, `auth.User` the visitor and registered types the app services.
Validate and authorize each protected action. Public methods are browser-callable;
keep internal helpers and decorated hooks unexported.

## Patches

Unexported server fields are tracked automatically. Assign normally in handlers
and keep rendering free of mutation. Stable keys preserve child identity.
Use `ui.State[T]` for browser-owned values and assignments such as
`onClick={open = !open}` for a change without a round trip.

## Errors

Return safe typed errors for expected failures. Log private details server-side.
Do not assume transport serialization conceals an unsafe error message.

## Broadcasts

Subscriptions and typed update messages let sessions respond to shared changes.
Authorize topic membership and bound resource IDs. Never trust the browser's
choice of a topic or handler argument.

## Connections and reconnects

A reconnect creates or rejoins the transport's session according to its token
and lifecycle. Do not treat component memory as durable storage. Session limits,
expiry and proxy WebSocket support are deployment concerns.

## Security

Protect the initial request, socket join and subsequent actions. Middleware
around an HTTP request does not automatically authorize every socket event.
Refresh identity when permissions change, enforce CSRF for cookie-authenticated
forms, and avoid exposing secret fields as browser props.

## The wire

The web transport uses ui's [live protocol](/reference/live-protocol), including
render-tree patches. Applications write shape files; they do not construct the
wire representation. See [Testing](/web/testing) for browser and session checks.
