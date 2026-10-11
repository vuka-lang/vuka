# Getting started

One command makes an app that runs: live pages styled with Tailwind, a
sign-in page wired to users in the database, sessions that survive
restarts, migrations applied at start-up, mail, and tests that pass. It is
plain Vuka code in plain folders, yours to change from the first minute.

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
vuka new shop
```

```text
? UI              › Live pages + Tailwind · recommended
? Database        › SQLite · a file, no server to run
? Mail            › Log in development, SMTP in production
? Authentication  › Yes, with a login page · users in the database
? Frontend app    › None
```

Enter takes each default; the arrow keys pick another. Every question has
a flag (`--db=postgres`, `--frontend=react`, …) and `--yes` takes the
defaults for the rest — see [the vuka command](/tools/cli#new).

```text
Created shop (module shop): live pages, SQLite, sign-in, mail.

The first admin, created when the app first starts (the password is kept in
.env, which git ignores):

	email     admin@example.com
	password  rDh9RkUcFh3tqV62mNCL
```

## Run it

```sh
cd shop
vuka run -watch .     # http://localhost:8080
```

The first start creates `data/app.db`, applies the migrations and creates
the admin. Open the app and you are sent to sign in:

![The sign-in page](/app-login.png)

A wrong email and a wrong password get the same message; the button shows
a spinner while it posts; "Remember me" keeps the sign-in for two weeks,
otherwise it ends with the browser session; light and dark follow the
system. After signing in you land where you were going:

![The dashboard, its user menu open](/app-dashboard.png)

The shell's links move between pages over the live connection, the
current one highlighted. The user menu opens in the browser — JS commands,
no server round trip — and signs you out.

The tests pass as they are:

```sh
vuka test ./...
```

They sign in with a wrong password and the right one, check that the
dashboard needs a sign-in, and restart the app on the same database to
check the session is still there.

## What's where

```text
shop/
  main.vuka              wiring only: the options web.New takes
  main_test.vuka         the app's tests
  .env, .env.example     settings and secrets; the first is never committed
  pages/
    home.vuka            the dashboard, a live page
    login.vuka           the sign-in page
  layouts/
    document.vuka        the HTML document, the favicon
    app.vuka             the signed-in shell: logo, navigation, user menu
    auth.vuka            the centred card of the sign-in page
    nav.vuka             the navigation, which modules add to
  components/            Button, Card, Input, Field, Flash, Icon, Logo
  modules/users/
    model.vuka           User, Session, Token: models of the ORM
    service.vuka         Users: what auth signs people in with
    stores.vuka          auth's sessions and tokens, in the database
    pages.vuka           the users page; admins add users there
    users_test.vuka
  migrations/
    0001_initial.go      the users, sessions and tokens tables
  static/logo.svg        the logo, also the favicon
  styles/app.css         Tailwind, and the theme's tokens
```

**`main.vuka`** is the app's configuration and nothing else:

```vuka
func options() []web.Option {
	db := orm.NewDatabases() // DATABASE_URL, read as the app starts
	return []web.Option{
		ormweb.Set(db).MigrateOnStart(true),
		mail.Provide(),
		auth.Module(auth.Config{
			Endpoints:  "/auth",
			Roles:      users.Roles,
			Sessions:   users.Sessions(db), // in the database: sign-ins survive restarts
			Tokens:     users.Tokens(db),
			RememberMe: true,
		}),
		users.Admin(), // ADMIN_EMAIL and ADMIN_PASSWORD: the first admin
		web.LiveLayout(layouts.Page("")),
		// … the static files
	}
}
```

Every route, page and live event needs a sign-in unless it is marked
`@auth.Public`; the users page asks for `users.read`, adding a user for
`users.write`. Roles grant them: an admin has every perm, a member
`users.read`.

**The look** is Tailwind classes over a few tokens in `styles/app.css`:
the brand colour (the logo's orange) and the font. Change them there and
every page follows; the shared pieces in `components/` keep focus rings,
labels and contrast consistent in both themes.

## Grow it

A module is the unit of growth:

```sh
vuka add module invoices
```

```text
wrote migrations/0002_invoices.go
wrote modules/invoices/invoices_test.vuka
wrote modules/invoices/model.vuka
wrote modules/invoices/pages.vuka
wrote modules/invoices/service.vuka
wrote main.vuka (imports the module)
```

The model is an [ORM](/orm/) model, the service holds what the page and
any API share, the page is a live page linked in the navigation, and the
migration makes the table, after the ones before it. Rename the fields,
add some, then let the ORM write the next migration from the models:

```sh
go tool orm makemigrations      # reads DATABASE_URL from .env
```

The app applies pending migrations when it starts; `go tool orm
showmigrations` lists them.

## Ship it

```sh
vuka build -o shop .
```

One binary: pages, styles, scripts and migrations inside it. Give it real
environment variables in place of `.env` — `DATABASE_URL`, `MAIL_DRIVER=smtp`
and the `MAIL_*` settings, `ADMIN_EMAIL` and `ADMIN_PASSWORD` for the first
start — and keep `data/` on a disk that persists if it is SQLite.

## Other kinds of app

| | |
|---|---|
| `--ui=templ` | the same app with [templ](/web/views#templ) views: a handler and its `.templ` file side by side |
| `--ui=api` | a JSON API: tokens from `POST /auth/token`, `/api/users`, no pages |
| `--db=postgres`, `--db=mysql` | that database's driver and a `DATABASE_URL` to fill in |
| `--auth=no` | no sign-in, no users module |
| `--frontend=react` (`vue`, `svelte`) | a Vite app in `frontend/`, served at `/app` ([Frontends](/web/frontend)) |
