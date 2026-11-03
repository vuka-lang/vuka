# Job Board

An example app built with [Vuka](https://vuka-lang.github.io/): employers
post jobs, job seekers search them and apply with a CV, and an admin closes
the ones that shouldn't be there. The
[Example Project](https://vuka-lang.github.io/guide/example-project) guide
walks through it.

## Run it

```sh
cp .env.example .env    # SQLite under data/, mail printed to the log
vuka run -watch .       # http://localhost:8080
vuka test ./...
```

Sign in as the admin `.env` names, or sign up as an employer or a job
seeker. The Tailwind CLI builds the stylesheet (`brew install tailwindcss`);
without it the pages run unstyled.

## What's where

```text
main.vuka              wiring: the options web.New takes
pages/                 the pages, each file's path its URL
  _layout.vuka.html    the document of every page
  (account)/           /login and /signup, outside the site's header
  (site)/              the site's header (_layout.vuka.html) and its pages:
                       /, /jobs/{job}, /jobs/{job}/apply, /applications,
                       /employer/… (guarded: employers), /admin (guarded: admins)
  _parts/              JobSearch, the live search on /
modules/jobs/          the jobs models, the Board service, the forms, the
                       nightly cron, the GraphQL query, the employer's email
modules/users/         users, sign-up, the session and token stores
migrations/            the schema, written by go tool orm makemigrations, and
                       the categories it starts with
layouts/, components/  the sign-in card, the user menu, shared markup
```
