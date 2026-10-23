# Authentication

[[toc]]

## Introduction

Authentication identifies the visitor; authorization checks whether that
visitor may perform an action. You configure the auth provider on the app,
register its services explicitly, and keep the page tree's guards separate
from presentation.

This page covers the user store, guarding page subtrees, reading the
identity in handlers and permissions in templates, signing in and out,
sessions, CSRF and testing.

## Storing Users {#users}

The auth package exposes `auth.User`, the signed-in identity, and permission
checks. Supply your application's user store and password and session
configuration.

> [!WARNING]
> Store password hashes, never plaintext passwords. Use the library's
> password verification helpers rather than comparing hashes yourself.

## Guarding Pages {#guards}

Use `guard.go` for protected page subtrees; it contains no markup. Keep
public login pages outside a protected subtree. See
[Guards](/web/folder-routes#guards).

A guard can obtain the current identity through its context and return
`web.Unauthorized` or `web.Forbidden`. For scoped permissions, validate the
path's resource before rendering it.

## The Identity in Handlers {#in-handlers}

Declare `auth.User` as a typed handler or hook parameter. Services, request
and form data are supplied by their types too; see
[Parameters & Validation](/web/parameters).

Check permissions before writes, including calls from a live page.

> [!WARNING]
> Hiding a button is not authorization.

This page shows the signed-in user's ID:

```html
<script lang="go">
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
</script>

<main><p>Signed in as {name}</p></main>
```

Save it as `pages/account.vuka.html` within the guarded tree. Keep the
identity server-side rather than exposing the whole user object as a prop.

## Permissions in Templates {#permissions-in-templates}

With `auth.Context` in `main`'s
[context processors](/web/context-processors), every template reads two
variables: `user`, the signed-in identity (`nil` for an anonymous
visitor), and `perms`, what that visitor may do. `perms.Has` takes the
same patterns as the gates, `orders.*` and `*` included:

```html
<header>
  if user != nil {
    <span>{user.ID}</span>
  }
  if perms.Has("placements.approve") {
    <button onClick={PreviewSelectedPlacements}>Preview</button>
  }
</header>
```

When an identity's roles or permissions change, it signs out, or it is
impersonated or revoked, the components showing `user` or `perms` render
again at the visitor's next event, a layout that stays among them.

> [!WARNING]
> Showing is not protecting. `perms.Has` decides what the page displays;
> anyone can still send the event a hidden button sends, or request the
> page a hidden link names. Check the permission where the work happens:
> `@auth.Requires("placements.approve")` on the event method or the
> handler, or `auth.Can(ctx, "placements.approve")` in it or in a guard,
> answering `web.Forbidden` when it is false.
> `vuka vet` warns about a control shown behind `perms.Has("x")` whose
> event method checks no `x` ([W0403](/errors/W0403)).

A processor reading the signed-in user never reaches the browser unless
it is listed as `web.Browser(web.Personal(p))`, and the permission list
never does ([E0426](/errors/E0426)).

## Signing In and Out {#signing-in-and-out}

Login form DTOs belong in logic files. A login handler validates input,
checks credentials through the auth store, issues the session cookie and
redirects to a safe local destination. Logout invalidates the session.

> [!WARNING]
> Do not accept an arbitrary return URL without validating its scheme and
> origin.

## Sessions and Tokens

Set a strong production secret through the environment and configure
secure, HTTP-only cookies. Expiry and identity refresh belong to the auth
and session layer.

Do not send credentials or session secrets to island props.

### Live Pages {#live-pages}

Live applications must refresh or invalidate stale identities after
permission changes. See [Live Pages](/web/live).

## CSRF

Cookie-authenticated form actions need CSRF protection. A WebSocket join and
its events also require the transport's origin and token checks.

> [!WARNING]
> Preserve those checks behind reverse proxies; do not replace them with a
> hidden form field whose value the server never verifies.

## Testing Authentication {#testing}

Test anonymous, authenticated and forbidden cases, including direct requests
to protected actions. Test socket joins and events after permission changes.
Auth test helpers can supply identities without using real production
credentials.

## In the Development Toolbar {#development-toolbar}

The [Development Toolbar](/web/toolbar) can expose the request's auth and
gate diagnostics in development. Keep it disabled in production; logs and
failure messages must not reveal secrets.
