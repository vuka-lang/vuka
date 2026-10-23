# Tailwind CSS

[[toc]]

## Introduction

Write [Tailwind](https://tailwindcss.com) classes in your views and they are
styled. There is no build config, no `<link>` to add and no CSS file to
commit.

The `vuka` command builds the stylesheet while you work and embeds it in the
binary for production. You add your own theme in a stylesheet of your own.

## Using Utilities

Use utilities directly in a shape:

```html
package pages

type Index struct{}
<main class="mx-auto max-w-xl p-8">
  <h1 class="text-3xl font-bold">Home</h1>
</main>
```

The Tailwind scanner reads the classes in shape files and the source files
of the components they use. Document markup belongs in `_layout.vuka.html`.

## Styling While You Work {#while-you-work}

`vuka run` keeps the stylesheet rebuilt beside your program. Add a class to
a `.vuka` file and save, and the open page restyles itself. The new
stylesheet replaces the old one without a reload, so a
[live page](/web/live) keeps its state and a half-filled form keeps what you
typed.

With `vuka run -watch` the program is rebuilt and swapped in when you change
its code as well. The stylesheet's watcher runs through the restarts, so a
change to a class restyles the page without a rebuild, and a change to code
gets the new build ([run -watch](/tools/cli#build-run-test-vet-install)).

The build goes to `.vuka/tailwind/` in the module, a directory that keeps
itself out of git.

> [!NOTE]
> Without the Tailwind CLI, `vuka run` says how to install it and serves the
> pages unstyled.

## Styling in Production {#in-production}

`vuka build` builds the stylesheet once, minified, and embeds it in the
binary, which needs nothing else to serve it. Pages link it as
`/_vuka/app.css?v=<hash>`, cached by browsers for good; a new build has a
new hash.

> [!WARNING]
> Without the Tailwind CLI, `vuka build` fails and says how to install it.

For plain `go build`, `vuka gen` writes the file that embeds the stylesheet
(`vuka_tailwind_gen.go` in the main package) with the rest of the generated
code.

## Adding Your Theme {#your-theme}

Colors, fonts, plugins and component classes go in your own stylesheet: an
`app.css` or `styles/app.css` at the module root that imports Tailwind.
Save this as `styles/app.css`:

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

`class="bg-brand font-display card"` then works anywhere.

### Keeping the Stylesheet Elsewhere

Keep it somewhere else and name it. Name it as a string literal, since the
`vuka` command reads it from your source:


<!-- go:fragment -->
```go
app := web.New(web.Tailwind("assets/site.css"))
```

### Classes in Component Libraries

Classes in component libraries you use (packages shipping `.vuka` or
`.templ` components, like [ui](/ui/)) are found too.

## Which Pages Get It

Every full page gets the stylesheet: one with `</head>` (or else
`</body>`), from shape markup, templ and `html/template` views and live
pages, unless it links `/_vuka/app.css` itself. A form post's page gets it
too, when the browser shows it as the document
([Forms That Fail](/web/routes#forms-that-fail)).

JSON and fragments (a script's post answered with HTML) don't, and neither
do live updates: the page already has it.

## Turning It Off

Pass `web.NoTailwind()` to the app:


<!-- go:fragment -->
```go
app := web.New(web.NoTailwind())
```

Then there is no link and no `/_vuka/app.css`, and the `vuka` command builds
nothing. `VUKA_TAILWIND=off` does the same for one `vuka run` or
`vuka build`.

## Serving Other Stylesheets {#other-stylesheets}

`web.RegisterCSS(name, css)` serves a stylesheet at `/_vuka/<name>` with the
same caching. The embedding file `vuka build` generates registers `app.css`
this way.

## Giving a Component Its Own Stylesheet {#a-page-s-own-stylesheet}

For rules that belong to one component, such as an animation or a `:hover`
on a row, give it a stylesheet of its own. It is scoped to its markup and
compiled into the same `app.css`: see [Styles & Scripts](/web/assets).
