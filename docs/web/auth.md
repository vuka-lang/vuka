# Authentication

Authentication identifies the visitor; authorization checks whether that visitor
may perform an action. Configure the auth provider on the app, register its
services explicitly and keep the page tree's guards separate from presentation.

## Users

The auth package exposes `auth.User`, the signed-in identity, and permission
checks. Supply your application's user store and password/session configuration.
Store password hashes, never plaintext passwords. Use the library's password
verification helpers rather than comparing hashes yourself.

## Guards

Use `guard.go` for protected page subtrees. It contains no markup. Keep public
login pages outside a protected subtree. A guard can obtain the current identity
through its context and return `web.Unauthorized` or `web.Forbidden`.
For scoped permissions, validate the path's resource before rendering it.

## In handlers

Declare `auth.User` as a typed handler or hook parameter. Services, request and
form data are supplied by their types too. Check permissions before writes,
including calls from a live page; hiding a button is not authorization.

```html
package pages

import (
    "github.com/vuka-lang/ui/v2"
    "context"
    "github.com/vuka-lang/web/v2/auth"
)

type Account struct { name string }

@ui.Mount
func (p *Account) mount(ctx context.Context, user auth.User) error {
    p.name = user.ID
    return nil
}
<main><p>Signed in as {name}</p></main>
```

Save as `pages/account.vuka.html` within the guarded tree. Keep the identity
server-side rather than exposing the whole user object as a prop.

## Signing in and out

Login form DTOs belong in logic files. A login handler validates input, checks
credentials through the auth store, issues the session cookie and redirects to
a safe local destination. Logout invalidates the session. Do not accept an
arbitrary return URL without validating its scheme and origin.

## Sessions and tokens

Set a strong production secret through the environment and configure secure,
HTTP-only cookies. Expiry and identity refresh belong to the auth/session layer.
Live applications must refresh or invalidate stale identities after permission
changes. Do not send credentials or session secrets to island props.

## CSRF

Cookie-authenticated form actions need CSRF protection. A WebSocket join and
its events also require the transport's origin and token checks. Preserve those
checks behind reverse proxies; do not replace them with a hidden form field
whose value the server never verifies.

## Testing

Test anonymous, authenticated and forbidden cases, including direct requests
to protected actions. Test socket joins and events after permission changes.
Auth test helpers can supply identities without using real production credentials.

## Development toolbar

The toolbar can expose the request's auth and gate diagnostics in development.
Keep it disabled in production; logs and failure messages must not reveal secrets.
