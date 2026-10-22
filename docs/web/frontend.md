# Frontends

Build the interface as a React, Vue or Svelte app with [Vite](https://vite.dev)
and ship it inside your binary, beside the JSON routes it calls. One process
serves both, on one address, so there is no CORS to configure, no second
deployment and no proxy in development.


```go
package main

import (
	"embed"
	"log"

	"github.com/vuka-lang/web/v2"
)

//go:embed all:frontend/dist
var dist embed.FS

type Pet struct{ Name string }

var pets = []Pet{{Name: "Rex"}}

func ListPets() []Pet { return pets }

func main() {
	app := web.New(web.Frontend(dist, "frontend/dist"))
	app.Get("/api/pets", ListPets)
	log.Fatal(app.Run(":8080"))
}
```

The Vite project lives in `frontend/`:

```
myapp/
  main.go
  frontend/
    package.json         vite, react, … (commit the lockfile)
    vite.config.js       plugins: [react(), vuka()]
    vuka/vite.js         the plugin; vuka run and vuka build keep it current
    index.html
    src/main.jsx
    dist/.gitkeep        the build's output; only .gitkeep is committed
```

```js
// frontend/vite.config.js
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import vuka from './vuka/vite.js'

export default defineConfig({ plugins: [react(), vuka()] })
```

Ignore `frontend/node_modules/` and `frontend/dist/*` in git, keeping
`frontend/dist/.gitkeep`: `//go:embed` needs the directory to hold a file
before the first build. The plugin puts `.gitkeep` back after each build.

## What is served

| request | answer |
|---|---|
| a path naming a file: `/assets/index-B3x9kQa1.js`, `/robots.txt` | that file, or 404 |
| any other GET path: `/`, `/pets/7`, `/settings/profile` | `index.html`: client-side routes survive a reload and work from a link |
| a route of the app, a live page, `/_vuka/…` | the app's, which always comes first |
| `/.vite/…` | 404: the build's manifest is for the server only |

So the frontend's router owns every page the app doesn't, and a JSON route
declared with `app.Get("/api/pets", …)` is never shadowed by it.

Pages load fast and update at once. A file Vite's manifest lists as build
output, with a content hash in its name, is cached by browsers for good;
every new build has new names. Everything else, `index.html` included, is
revalidated: the browser asks each time and gets `304 Not Modified` while
nothing changed.

### Under a prefix


<!-- go:context
//go:embed all:admin/dist
var admin embed.FS
-->

<!-- go:fragment -->
```go
opt := web.Frontend(admin, "admin/dist", web.FrontendAt("/admin"))
```

serves the app at `/admin` and below. Build it with the same base, in
`vite.config`: `base: '/admin/'`. An app can serve several frontends, each
under its own prefix.

## While you work

```sh
vuka run -watch .
```

starts your app and, beside it, the frontend's own Vite dev server. Open the
app's address, as always: its pages load their modules from Vite, so editing
a component updates the open page in place, keeping its state, while calls to
`/api/…` reach your Go code on the same origin. Save a `.vuka` file and the
app is rebuilt behind the page ([run -watch](/tools/cli#run-watch)).

- The first run installs the frontend's dependencies, with the package
  manager its lockfile is for (npm, pnpm, yarn or bun).
- Vite's output appears prefixed `vite:`; its own address is left out, since
  the page to open is the app's.
- Ctrl-C stops the app, then Vite.

How it fits together: while the dev server runs, the plugin writes
`frontend/dist/.vite/vuka-hot.json` with its address and process id. A
development app (`vuka run`, `web.Dev()`, `VUKA_ENV=development`) finds that
file beside the build it serves, fetches the page from Vite and points its
scripts at it. A file the page asks the app for, like `/logo.svg` from
`public/`, is redirected to Vite, which has the current one. When Vite stops
the file goes, and the app serves the build again; a file left behind by a
Vite that was killed names a process that is gone, and is ignored.

The same works without the `vuka` command: run `npm run dev` in `frontend/`
and your app with `VUKA_ENV=development`.

Before the first build, with no dev server running, the app's pages say how
to build the frontend, and its JSON routes work as usual.

The [development toolbar](/web/toolbar) appears on the frontend's pages too,
and lists each `fetch` the app makes with what the server did for it. The
app's [Tailwind stylesheet](/web/styling) is not added: the frontend brings
its own styles.

## Building

```sh
vuka build -o myapp .
```

runs `vite build` in `frontend/` first, then compiles the binary with
`frontend/dist` inside it. The binary needs no Node and no files beside it.

An app whose frontend wasn't built doesn't start outside development:

```
web: Frontend("frontend/dist"): the frontend is empty or missing: build it first (vuka build runs vite build; or npm run build in the frontend's directory), or run in development (vuka run, VUKA_ENV=development) for a placeholder page
```

A directory without a `package.json`, such as a `dist` built elsewhere and
copied in, is served as it is: nothing runs Vite.

`VUKA_FRONTEND=web` names another directory than `frontend/` (relative to
where you run `vuka`); `VUKA_FRONTEND=off` leaves the frontend alone.

## Configuration for the page

Settings the frontend needs, such as an analytics key or the name to show,
come from the app's environment and [`.env` files](/web/configuration), so
one build runs in every environment:


<!-- go:fragment -->
```go
opt := web.Frontend(dist, "frontend/dist", web.FrontendEnv("APP_NAME", "MAPS_KEY"))
```

```jsx
<h1>{import.meta.env.APP_NAME}</h1>
```

`index.html` gets the values when the app serves it, before any module runs,
and the plugin makes `import.meta.env.APP_NAME` read them. Only the keys
named reach the browser, and every visitor can read them: name public values
only, never a secret. Vite's own variables (`MODE`, `DEV`, `VITE_…`) keep
working as before. For TypeScript, declare them:

```ts
// src/vite-env.d.ts
interface ImportMetaEnv {
  readonly APP_NAME: string
  readonly MAPS_KEY?: string
}
```

## A complete example

[`examples/spa`](https://github.com/vuka-lang/web/tree/main/examples/spa) is
a React app with client-side routes, calling the app's `/api/pets` routes.
Its tests run without Node.
