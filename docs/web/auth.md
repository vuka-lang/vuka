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

### Passwords from another system

Users moved from another system keep the passwords they had there when
`Config.Hashers` reads that system's encoding. A `Hasher` checks one
encoding, and says whether the stored value is in it at all:

```vuka
// LegacySHA1 reads "sha1$<salt>$<hex>", the old system's encoding.
type LegacySHA1 struct{}

func (LegacySHA1) Check(encoded, password string) (ok, known bool) {
	salt, sum, found := strings.Cut(strings.TrimPrefix(encoded, "sha1$"), "$")
	if !strings.HasPrefix(encoded, "sha1$") || !found {
		return false, false                    // not this encoding
	}
	h := sha1.Sum([]byte(salt + password))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(h[:])), []byte(sum)) == 1, true
}

auth.Module(auth.Config{Hashers: []auth.Hasher{LegacySHA1{}}})
```

A login reads its own encodings (Argon2id, bcrypt) first, then asks each
hasher in turn; the first that knows the encoding decides, and a stored
value no one knows is a failed login (logged). A match is stale: with
`SetPassword` on `Users`, auth stores an Argon2id hash in its place, so
each user moves off the old encoding at their next sign-in. Compare in
constant time, as above.

A hasher may also have a `Dummy() string` method returning a value in its
encoding, of the cost its stored values have. An unknown login is checked
against a hash of the kind the logins found lately had — Argon2id or
bcrypt of the same cost, or that dummy — so it takes as long as a known one;
`Config.FailedLoginTime` evens out the rest.

## Gates

| | |
|---|---|
| nothing | a sign-in (unless `Config.PublicByDefault`) |
| `@auth.Public` | everyone: the login page, a landing page, a health check |
| `@auth.Required` | a sign-in, said explicitly |
| `@auth.Requires(perms…)` | a sign-in with every one of these perms |
| `@auth.RequiresAny(perms…)` | a sign-in with at least one |

They go on routes, on `@web.Live` pages, on controllers, on live event
methods and on any other method or function:

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
`@auth.Public` on a route opens it alone.

A live event needs a sign-in unless something marks it, as a route does:
the gate on its handler, on the handler's type, on the component it
belongs to — or, for the page's own events, on the page (its type, or
`web.Attrs` given to `app.Live`). `@auth.Public` on a page opens the page
and its own events; a component inside it whose type and handler carry no
gate needs a sign-in for its events. Mark that component `@auth.Public` too
when anonymous visitors should use it. On a live event, the gate is
checked when the browser sends the event: a visitor without the perm gets
the event refused — the handler doesn't run, and the page receives the
error as it would a handler's (`vk:error`) — whatever the page shows or the
browser sends. Hide the button from those who can't use it with
`auth.Can(ctx, "board.delete")`, and keep the gate.

### What a live event's gate covers

A gated method can't be run from the browser by someone the gate refuses,
whatever shape the handler takes. Two checks make sure of it:

- **Before the handler runs**, where web dispatches the event: the gates
  written on the handler's method, on its type and on the component the
  event targets (Vuka records every declaration's attributes, so a method
  value handed to `onClick` is enough to find them). This refuses a gated
  method handler, a push by name (`ctx.push("Clear")`), and every event of
  a component gated on its type — a function literal's
  (`onClick={func() { b.n++ }}`) included. `@auth.Public` on a method of a
  gated type opens that method.
- **When the gated method itself is called**, from inside the handler —
  a function literal (`onClick={func() { b.Clear() }}`), another method, a
  goroutine — by its decorator, against the identity found:

| the call | is checked against |
|---|---|
| with a `context.Context` from a request or a live page (what handlers, `Mount` and `Update` are given) | that context's identity, on any goroutine: `go b.Clear(ctx)` is checked; so is a gated service method a route calls with `r.Context()` |
| on the live connection while the event's handler runs, without such a context | the page's identity |
| on a goroutine the handler starts (`go b.Clear()`), or a decorator moves the call to | the page's identity: web knows which connection started the goroutine |
| on the live connection outside an event — a render, a broadcast | the page's identity |
| on a goroutine serving an HTTP request, without the request's context | the request's identity, once its route runs; refused before |
| `auth.WithIdentity(ctx, id)` | `id` — a job acting for a user |
| `auth.System(ctx)` | nothing: the app's own work passes — crons get it by themselves |
| none of these | refused — unless `LaxGates`, which runs it as the app's own work |

A refused call returns `web.Unauthorized` or `web.Forbidden` as its error
(a method without an error result just doesn't run); refused while the
event's handler runs, the event's reply carries it, and the page gets
`vk:error`. A gate on a component's type doesn't apply to the methods the
session calls itself — `Render`, `Mount`, `Update` — so a page rendering a
gated component for someone else renders; its events are refused.

**Strict by default.** A gated call nothing attributes to an identity is
refused. Code outside requests marks its context as the app's own work —
`ctx = auth.System(ctx)` in `main`, a job, a migration — and gated methods
called off the handler's own goroutine take a `context.Context`, passed
down from the handler. Web's crons run with `auth.System` already.

`auth.Config{LaxGates: true}` runs such a call as the app's own work
instead. That leaves unchecked, for a call that came from a live event:

- a goroutine started by a goroutine of the handler (or a pool's worker),
  calling a gated method without a request's context — web knows a
  goroutine's parent, not its grandparent;
- a goroutine still running after its page's connection closed;
- a gated method given `context.Background()` (or another context not from
  the request) on such a goroutine.

Whatever the setting, a call on a goroutine serving a request, or given a
request's context without auth's state, is never the app's own work: it is
checked against the request's identity, or refused. The setting holds for
the whole process once an app with it is built (`StrictGates: true` keeps
strict whatever another app says).

::: tip Moving from LaxGates being the default
Apps written when gated calls outside requests ran unchecked: give `main`'s
and jobs' contexts `auth.System(ctx)` where they call gated methods, pass a
`context.Context` to gated methods called from goroutines a handler starts,
or set `LaxGates: true` to keep the old behaviour while you do. Crons need
nothing. Strict is the default now: `StrictGates: true` only overrides
another app's `LaxGates` in the same process.
:::

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
| `auth.WithIdentity(ctx, id)` | a context signed in as `id`, for work outside a request (a job acting for a user): gates check it |
| `auth.System(ctx)` | a context marked as the app's own work: gated methods given it run, and `auth.Check` passes; never give it a request's work |

`auth.Check` asks the policy of the object's type when the config has one,
and otherwise the perms. A policy for `Order` decides for an `Order` and an
`*Order` alike, and so does one for `*Order`. A context without the app's
auth — `auth.WithIdentity` of a context from outside any request — can't
ask the app's policies, so `Check` refuses it any object a policy covers
rather than decide by the perms alone; a context marked `auth.System`
passes. A policy decides alone — it may let an owner edit their own order
without the perm:

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

A live page's sign-in is checked when the page mounts: its join, and each
navigation over its connection, run its route, which reads the session or
token once, through the route's gates. Its events then trust the identity
it mounted with, and read no session at all, so a busy page costs the
session store nothing per click.

A page whose sign-in ends is closed, and reloads: its route sends it to the
login page.

- An ending auth hears of closes the page at once, without waiting for a
  message from it: `auth.SignOut` (a sign-out in another tab), `auth.Revoke`,
  `auth.RevokeUser`, a new sign-in replacing the browser's session, and a
  request finding the session expired or its user gone.
- An ending auth doesn't hear of — a session row deleted by hand, a
  sign-out handled by another replica — is caught by the check that runs on
  every open page every `web.LiveCheckEvery` (30 seconds), so a page sitting
  open learns of it within that interval.

On that same periodic check, once `Config.LiveRecheck` (60 seconds) has
passed, the page also loads its identity again with `Users.Load`: a user
gone, or whose perms or roles changed, gets the page reloaded with what they
are now. The socket only opens from the app's own origin.

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
`/auth/login`.

`POST /auth/login` sends a failure back to the login page with
`?error=invalid` (or `throttled`, `refused`), which `auth.LoginError(r)`
turns into a message; what was typed is gone. To keep it, and to show a
refusal's own message, post the form to your own handler and re-render the
page — a form post the browser shows gets the whole page
([Forms that fail](/web/routes#forms-that-fail)):

```vuka
@web.Post("/login")
@auth.Public
func SignIn(ctx context.Context, r *http.Request) ui.Node {
	if _, err := auth.Login(ctx, r.PostFormValue("login"), r.PostFormValue("password")); err != nil {
		return <auth.LoginPage request={r} error={auth.LoginMessage(err)} />
	}
	return web.Redirect(auth.Next(r, "/"))
}
```

`auth.LoginMessage(err)` is the same message for an unknown login and a
wrong password, the throttle's, a `*web.Error` from `CheckLogin` as it is
(an account disabled, access that ended), and a general one for anything
else, which is yours to log. On a POST, `auth.LoginPage` fills the login
and `next` from the posted form; `auth.LoginForm` takes `login={…}`. A sign-out is a POST:
`<form method="post" action="/auth/logout"><button>Sign out</button></form>`.

From your own handlers:

| | |
|---|---|
| `auth.Login(ctx, login, password)` | checks and signs in |
| `auth.SignIn(ctx, userID)` | signs in someone proven another way (a magic link, an invitation) |
| `auth.SignOut(ctx)` | signs out |
| `auth.IssueToken(ctx, userID, name, ttl)`, `auth.Revoke(ctx, token)` | bearer tokens |
| `auth.RevokeUser(ctx, userID)` | ends every session and token of a user, and closes their live pages at once |

Signing in and out set a cookie, so they belong to an HTTP request (a form
post), not to a live event. Outside a request — a job, a CLI — inject
`*auth.Auth`: it has `Issue`, `Revoke`, `RevokeUser`, `SetPassword` and
`Verify`.

**Login errors** are the same for an unknown login and a wrong password —
`invalid login or password`, after the same work — so the page never tells
which logins exist. Every failed login, whatever failed — an unknown login,
a wrong password, a stored hash of another cost or encoding, a password too
long for bcrypt, a throttled attempt — takes at least
`Config.FailedLoginTime` (300ms; negative for none), so timing doesn't tell
either.

Failed logins are throttled per login, per account and per address (5 per
login or account and 50 per address in 15 minutes by default,
`Config.Throttle`): further attempts get `429` with `Retry-After`, the right
password included. Logins compare folded — case, accents and compatibility
forms aside, as a case- and accent-insensitive database collation finds the
same row for `Admin` and `ádmin` — and once `FindLogin` finds an account,
its own count applies whatever spelling or other login reached it. The
address is `web.ClientIP(r)`: behind a proxy, name it with
`web.TrustedProxies` ([deployment](/web/deployment#behind-a-proxy)), or
every visitor shares the proxy's; `Config.ClientIP` replaces it.

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
| `Throttle`, `ClientIP` | failed-login limits; the address they count (`web.ClientIP`) |
| `FailedLoginTime` | the least time a failed login takes (300ms; negative for none) |
| `LiveRecheck` | how often a live page loads its identity again (60s; negative for never) |
| `JWT` | JWTs from elsewhere |
| `Policies` | `auth.PolicyFor` rules for `auth.Check` |
| `TrustedOrigins` | origins allowed to post |
| `LaxGates` | a gated call nothing attributes to an identity runs as the app's own, rather than being refused |
| `StrictGates` | the default; keeps the process strict whatever another app's `LaxGates` says |
