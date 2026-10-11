# Authentication

Most apps have pages only some people may see and actions only some may
take. `github.com/vuka-lang/web/auth` handles the whole of it — passwords,
sessions, API tokens, who may do what — so an app writes one interface:
where its users and their passwords live. Once it is installed, **every
route, live page and live event needs a sign-in** unless marked public, so
a page you forget to protect is protected anyway.

```vuka
import (
	"github.com/vuka-lang/web"
	"github.com/vuka-lang/web/auth"
)

@web.Get("/login")
@auth.Public
func Login(r *http.Request) ui.Node { return <auth.LoginPage request={r} /> }

@web.Get("/orders")
@auth.Requires("orders.read")
func Orders(me *auth.Identity, store *Store) Result[[]Order] { return store.For(me.ID) }

func main() {
	web.New(auth.Module(auth.Config{Endpoints: "/auth"})).Run("")
}
```

A visitor who isn't signed in and opens `/orders` is sent to
`/login?next=%2Forders`, signs in, and comes back. A script calling it gets
`401`; someone signed in without `orders.read` gets `403`. A complete app is
in [`examples/auth`](https://github.com/vuka-lang/web/tree/main/examples/auth).

## Users

The app's side is two methods: find a user by what they type to sign in,
and load a user by id.

```vuka
@web.Service
type Users struct{ db *DB }

// FindLogin returns the identity whose login (a username, an email) this is,
// with its encoded password; nil when there is none.
func (u *Users) FindLogin(ctx context.Context, login string) (*auth.Identity, string, error) {
	a, found, err := u.db.AccountByEmail(ctx, login)
	if err != nil || !found {
		return nil, "", err
	}
	return a.Identity(), a.PasswordHash, nil
}

// Load returns the identity of a user id, as a session or token names it;
// nil when the user is gone, which signs them out.
func (u *Users) Load(ctx context.Context, id string) (*auth.Identity, error) { … }

func (a Account) Identity() *auth.Identity {
	return &auth.Identity{ID: a.ID, Roles: a.Roles, User: a}
}
```

A `@web.Service` implementing `auth.Users` is found by itself; otherwise
give it in the config — `auth.Config{Users: NewUsers}` takes a value or a
constructor the container builds.

`auth.Identity` is who a request comes from:

| | |
|---|---|
| `ID` | the user's id, as `Load` takes it |
| `Perms` | what they may do: `"orders.write"`, or wildcards — `"orders.*"`, `"*"` |
| `Roles` | names of perm sets: `auth.Config{Roles: map[string][]string{"admin": {"orders.*"}}}` adds them to `Perms` |
| `User` | the app's own record, for handlers; auth never sends it anywhere |

Three more methods are optional:

| | |
|---|---|
| `SetPassword(ctx, id, encoded string) error` | stores a new hash: auth upgrades stale hashes at login, and `auth.SetPassword(ctx, id, plain)` uses it |
| `CheckLogin(ctx, *auth.Identity) error` | refuses a login whose password was right — an account disabled or unconfirmed; a `*web.Error` is shown as is |
| `Public(*auth.Identity) any` | what `GET /auth/me` shows of the user |

Passwords are stored as `auth.HashPassword(plain)` gives them:
Argon2id (`$argon2id$v=19$m=19456,t=2,p=1$…`). Hashes from an older system
in bcrypt (`$2a$`, `$2b$`) are checked too, and replaced at the user's next
login, as is an Argon2id hash weaker than `Config.Passwords`.

## Gates

| | |
|---|---|
| nothing | a sign-in (unless `Config.PublicByDefault`) |
| `@auth.Public` | everyone: the login page, a landing page, a health check |
| `@auth.Required` | a sign-in, said explicitly |
| `@auth.Requires(perms…)` | a sign-in with every one of these perms |
| `@auth.RequiresAny(perms…)` | a sign-in with at least one |

They go on routes, on `@web.Live` pages, on controllers and on live event
methods:

```vuka
@web.Controller("/admin")
@auth.Requires("admin")                    // every route of the controller
type Admin struct{ store *Store }

@web.Get("/")
func (a *Admin) Index() Result[[]Order] { … }

@web.Get("/status")
@auth.Public                               // this one is open
func (a *Admin) Status() string { return "ok" }

@web.Live("/board")
@auth.Requires("board.read")               // the page, and each of its events
type Board struct { ui.Live; … }

@auth.Requires("board.delete")             // this event, refused to anyone else
func (b *Board) Clear(ctx context.Context) error { … }
```

A route takes its controller's gate and its own, both checked;
`@auth.Public` on a route opens it alone. On a live event, the gate is
checked when the browser sends the event: a visitor without the perm gets
the event refused — the handler doesn't run, and the page receives the
error as it would a handler's (`vk:error`) — whatever the page shows or the
browser sends. Hide the button from those who can't use it with
`auth.Can(ctx, "board.delete")`, and keep the gate.

The gate covers what the event's handler runs: the gated method as the
handler, or called from one — a function literal such as
`onClick={() => b.Clear()}`, or another method's handler. It doesn't apply
to the render after the event (a gate on a component's type leaves its
`Render` alone) or to calls from Go outside an event. A gated method called
on a goroutine the handler starts isn't checked: gate the handler.

From Go, gates are attributes: `app.Get("/x", h, web.Attrs(auth.Requires("x")))`,
`app.Live("/board", &Board{}, web.Attrs(auth.Public()))`.

Who is refused, and how:

| | page visit (GET) | form post | script, API |
|---|---|---|---|
| not signed in | 302 to the login page, `?next=` the page | 303 to the login page | 401, `WWW-Authenticate: Bearer` |
| signed in, without the perm | 403 page | 403 page | 403 JSON |

A route that renders a page counts as a page, and so does any request
whose `Accept` names `text/html`. Errors take the app's usual shape
(`web.OnError`). Build fails when a route carries a gate and the app has no
`auth.Module`, and when the login page itself needs a sign-in.

**What isn't gated:** static files (`app.Static`) and web's own `/_vuka/`
assets are public; put a private file behind a route.

## In handlers

```vuka
@web.Get("/me")
func Me(me auth.Identity) string { return "hello " + me.ID }          // 401 when signed out

@web.Get("/")
@auth.Public
func Home(me *auth.Identity) ui.Node { … }                           // nil when signed out

func (s *Orders) Delete(ctx context.Context, id int) error {
	o := s.find(id)?
	auth.Check(ctx, "orders.delete", o)?                              // 401 / 403 as is
	…
}
```

| | |
|---|---|
| `auth.Current(ctx)` | the identity, or nil |
| `auth.Can(ctx, perm)` | whether it has the perm |
| `auth.Check(ctx, perm, obj)` | nil when it may do `perm` to `obj`; else `web.Unauthorized` or `web.Forbidden` |
| `*auth.Identity`, `auth.Identity` parameters | bound by themselves; the second answers 401 when signed out |

`auth.Check` asks the policy of the object's type when the config has one,
and otherwise the perms. A policy decides alone — it may let an owner edit
their own order without the perm:

```vuka
auth.Config{
	Policies: []auth.Policy{
		auth.PolicyFor(func(ctx context.Context, id *auth.Identity, perm string, o *Order) bool {
			return o.Owner == id.ID || id.Can(perm)
		}),
	},
}
```

## Live pages

A live page's identity is the one its connection joined with: the page's
route runs again for the WebSocket with the browser's cookie, through the
gates, so `Mount` and every event handler see it.

```vuka
@web.Live("/board")
type Board struct {
	ui.Live
	me *auth.Identity
}

func (b *Board) Mount(ctx context.Context) { b.me = auth.Current(ctx) }

func (b *Board) Render() ui.Node {
	return <p>Signed in as {b.me.ID}</p>
}
```

A page whose sign-in ends — `auth.RevokeUser`, a sign-out in another tab,
an expired session, a user `Load` no longer finds — is closed at its next
message, and reloads: its route sends it to the login page. The socket
only opens from the app's own origin.

## Signing in and out

`Config.Endpoints` mounts ready endpoints under a prefix:

| | |
|---|---|
| `POST /auth/login` | `login`, `password`, `next` — a form or JSON. A form goes on to `next` (or `Config.Home`) once signed in, and back to the login page with `?error=` otherwise; JSON gets the identity or the error |
| `POST /auth/logout` | ends the session (or revokes the bearer token the request carries) |
| `GET /auth/me` | `{"id", "roles", "perms", "user"}` |
| `POST /auth/token` | `login`, `password`, optional `name`: `{"access_token", "token_type": "Bearer", "expires_in"}`, shown once |

The login page is the app's: `auth.LoginPage` is a whole page, and
`auth.LoginForm` a form to put in your own layout:

```vuka
@web.Get("/login")
@auth.Public
func Login(r *http.Request) ui.Node {
	return <Shell title="Sign in">
		<auth.LoginForm next={r.URL.Query().Get("next")} error={auth.LoginError(r)} />
	</Shell>
}
```

Or write the form yourself: it posts `login`, `password` and `next` to
`/auth/login`. A sign-out is a POST:
`<form method="post" action="/auth/logout"><button>Sign out</button></form>`.

From your own handlers:

| | |
|---|---|
| `auth.Login(ctx, login, password)` | checks and signs in |
| `auth.SignIn(ctx, userID)` | signs in someone proven another way (a magic link, an invitation) |
| `auth.SignOut(ctx)` | signs out |
| `auth.IssueToken(ctx, userID, name, ttl)`, `auth.Revoke(ctx, token)` | bearer tokens |
| `auth.RevokeUser(ctx, userID)` | ends every session and token of a user, and closes their live pages |

Signing in and out set a cookie, so they belong to an HTTP request (a form
post), not to a live event. Outside a request — a job, a CLI — inject
`*auth.Auth`: it has `Issue`, `Revoke`, `RevokeUser`, `SetPassword` and
`Verify`.

**Login errors** are the same for an unknown login and a wrong password —
`invalid login or password`, after the same work — so the page never tells
which logins exist. Failed logins are throttled per login and per address
(5 per login and 50 per address in 15 minutes by default,
`Config.Throttle`): further attempts get `429` with `Retry-After`, the right
password included. Behind a proxy, set `Config.ClientIP` to read the
address it passes on, or every visitor shares the proxy's.

**`next`** is only followed when it is a path of this site: no scheme or
host, no `//` or `/\` a browser reads as another site, no backslash or
control character, before or after decoding. Anything else goes to
`Config.Home`.

## Sessions and tokens

**Pages** sign in with a session: a cookie holding a random 256-bit id —
`HttpOnly`, `SameSite=Lax`, `Secure` on HTTPS (or always, with
`Config.SecureCookie` when TLS ends at a proxy that doesn't say so). The
store keeps the SHA-256 of the id, never the id. Each sign-in starts a new
session under a new id and ends the one the browser had, so an id planted
before the sign-in is worthless. Sessions last `Config.SessionTTL` (14
days); `Config.SessionIdle` ends one unused for that long.

**Remember me.** With `Config.RememberMe`, staying signed in is the user's
choice: a sign-in whose form has the `remember` field checked (`"on"`,
`"1"`, `"true"`; JSON's `"remember": true`) keeps its persistent cookie for
`SessionTTL`; any other gets a cookie that ends with the browser session,
and a session of a day at most. `<auth.LoginForm remember />` draws the
checkbox; a form of your own names it `remember`.

**APIs** sign in with `Authorization: Bearer <token>`: tokens from
`POST /auth/token` or `auth.IssueToken`, random and opaque (`vka_…`),
stored hashed, lasting `Config.TokenTTL` (30 days). A token that doesn't
verify is `401`, not an anonymous request.

**JWTs** issued elsewhere — an identity provider, a gateway — are accepted
with `Config.JWT`: `Key` for HS256 or `PublicKey` for RS256, which also
fixes the algorithm a token must name; `exp` is required, `nbf`, `Issuer`
and `Audience` checked when present or set. The `sub` claim is the id
`Users.Load` takes.

::: warning Memory stores
Sessions and tokens are kept in memory unless the config gives stores. That
suits development, tests and a single process that can afford it, but:
every user is signed out when the process restarts or a new version
deploys, API tokens stop working, and replicas behind a load balancer
don't share sign-ins. For production, implement `auth.SessionStore` and
`auth.TokenStore` — `Get`, `Save`, `Delete` and `DeleteUser`, keyed by a
hash — over the app's database or cache. Throttling counts and the
revocation of JWTs are per process too.
:::

## CSRF

A signed-in browser sends its cookie with every request, even one another
site's page makes it send. `auth.Module` turns on `web.CSRF()`, which
answers `403` to a POST, PUT, PATCH or DELETE that a browser says comes from
another site (`Sec-Fetch-Site`, else `Origin`, as net/http's
`CrossOriginProtection` reads them). Forms need no token. Keep GET requests
free of side effects, since they are never refused. A request with a bearer
token passes, since no browser adds one by itself; other origins allowed to
post go in `Config.TrustedOrigins`.

## Testing

`github.com/vuka-lang/web/auth/authtest`:

```vuka
func TestOrders(t *testing.T) {
	admin := &auth.Identity{ID: "1", Perms: []string{"orders.*"}}
	srv := httptest.NewServer(web.New(auth.Module(auth.Config{}), authtest.As(admin)).Handler())
	defer srv.Close()
	// every request is signed in as admin: pages, APIs, live pages and their events
}

func TestLogin(t *testing.T) {
	users := authtest.NewUsers().Add("ada@example.com", "correct horse", auth.Identity{ID: "1"})
	app := web.New(auth.Module(auth.Config{Users: users, Passwords: authtest.Cheap, Endpoints: "/auth"}))
	// POST /auth/login …
}
```

| | |
|---|---|
| `authtest.As(id)` | every request of the app signed in as `id` (only in test binaries) |
| `authtest.Request(r, id)` | one request, for a handler called directly |
| `authtest.Context(ctx, id)` | a context signed in as `id`: a live session built in a test (`live.NewSession`, `livetest`), a service called directly |
| `authtest.NewUsers()` | `auth.Users` in memory: `Add(login, password, identity)`, `AddEncoded`, `Remove` |
| `authtest.Cheap` | a fast password cost for tests |

## The development toolbar

In development, the [toolbar](/web/toolbar)'s Request panel says who the
request came from (`7 (session)`, `anonymous`) and which gate let it through
or refused it (`Requires orders.write: refused, forbidden`); a refused live
event is a failed message in the Live panel. Cookies, `Authorization` headers and
parameters named like a password, token or secret are hidden.

## Configuration

| `auth.Config` | |
|---|---|
| `Users` | the app's `auth.Users`, or a constructor; nil: the provided type implementing it |
| `PublicByDefault` | routes without a gate are public |
| `LoginPath`, `Home` | the login page (`/login`) and where a sign-in goes without `next` (`/`) |
| `Endpoints` | the prefix of the endpoints (`"/auth"`); none when empty |
| `Roles` | role → perms |
| `Sessions`, `Tokens` | stores; in memory by default |
| `SessionTTL`, `SessionIdle`, `TokenTTL` | 14 days, off, 30 days |
| `RememberMe` | the login form's `remember` field decides whether a sign-in outlives the browser session |
| `Cookie`, `SecureCookie` | the cookie's name (`vk_session`); `Secure` always |
| `Passwords` | the Argon2id cost of new hashes (`auth.Argon2Defaults`) |
| `Throttle`, `ClientIP` | failed-login limits; the address they count |
| `JWT` | JWTs from elsewhere |
| `Policies` | `auth.PolicyFor` rules for `auth.Check` |
| `TrustedOrigins` | origins allowed to post |
