# Styling with Tailwind

Write [Tailwind](https://tailwindcss.com) classes in your views and they are
styled — no build config, no `<link>` to add, no CSS file to commit:

```vuka
@web.Get("/")
func Home() ui.Node {
	return <Layout title="Home">
		<button class="rounded bg-emerald-500 px-4 py-2 text-white"
			onClick={ui.JS.AddClass(ui.Ref("note"), "opacity-50")}>Save</button>
		<p ref="note" class="mt-2 text-sm text-gray-500">Unsaved changes</p>
	</Layout>
}
```

`vuka run` and `vuka build` build the stylesheet from every class your module
uses — in JSX, [templ](/web/views) and `.html` views, Go code, and the names a
[JS command](/ui/js) adds, like `opacity-50` above — and every full page the
app serves links it.

The one thing to install is the Tailwind CLI:

```sh
brew install tailwindcss        # or: npm i -D @tailwindcss/cli
```

## While you work

`vuka run` keeps the stylesheet rebuilt beside your program. Add a class to a
`.vuka` file, save, and the open page restyles itself: the new stylesheet
replaces the old one without a reload, so a [live page](/web/live) keeps its
state and a half-filled form keeps what you typed.

With `vuka run -watch` the program is rebuilt and swapped in when you change
its code as well; the stylesheet's watcher runs through the restarts, so a
change to a class restyles the page without a rebuild, and a change to code
gets the new build ([run -watch](/tools/cli#run-watch)).

The build goes to `.vuka/tailwind/` in the module, a directory that keeps
itself out of git. Without the Tailwind CLI, `vuka run` says how to install it
and serves the pages unstyled.

## In production

`vuka build` builds the stylesheet once, minified, and embeds it in the
binary, which needs nothing else to serve it. Pages link it as
`/_vuka/app.css?v=<hash>`, cached by browsers for good; a new build has a new
hash. Without the Tailwind CLI, `vuka build` fails and says how to install
it.

For plain `go build`, `vuka gen` writes the file that embeds the stylesheet
(`vuka_tailwind_gen.go` in the main package) with the rest of the generated
code.

## Your theme

Colors, fonts, plugins and component classes go in your own stylesheet: an
`app.css` or `styles/app.css` at the module root that imports Tailwind.

```css
/* styles/app.css */
@import "tailwindcss";

@theme {
	--color-brand: #0f766e;
	--font-display: "Inter", sans-serif;
}

@layer components {
	.card { @apply rounded-lg border p-4 shadow-sm; }
}
```

`class="bg-brand font-display card"` works anywhere then. Keep it somewhere
else and name it — as a string literal, since the `vuka` command reads it from
your source:

```vuka
app := web.New(web.Tailwind("assets/site.css"))
```

Classes in component libraries you use (packages shipping `.vuka` or
`.templ` components, like [ui](/ui/)) are found too.

## Which pages get it

Every full page — one with `</head>` (or else `</body>`) — from JSX, templ
and `html/template` views and live pages, unless it links `/_vuka/app.css`
itself — a form post's page too, when the browser shows it as the document
([Forms that fail](/web/routes#forms-that-fail)). JSON, fragments (a
script's post answered with HTML) don't, and neither do live updates: the
page already has it.

## Turning it off

```vuka
app := web.New(web.NoTailwind())
```

No link, no `/_vuka/app.css`, and the `vuka` command builds nothing.
`VUKA_TAILWIND=off` does the same for one `vuka run` or `vuka build`.

## Other stylesheets

`web.RegisterCSS(name, css)` serves a stylesheet at `/_vuka/<name>` with the
same caching; the embedding file `vuka build` generates registers `app.css`
this way.

## A page's own stylesheet

For rules that belong to one component — an animation, a `:hover` on a row —
give it a stylesheet of its own, scoped to its markup and compiled into the
same `app.css`: see [Styles and scripts](/web/assets).
