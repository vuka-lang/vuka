# Kit Gallery

[[toc]]

## Introduction

`vuka ui gallery` serves the kit's components (`ui.Button`, `ui.Modal`,
`ui.DataTable` and the rest) on your machine, the way a component catalogue
reads:

- an index of the components, each with a small preview;
- a page per component: a sentence on what it is, its examples (each a
  preview with the `.vuka.html` source that writes it), its props with
  their types and defaults, related components, and the `vuka ui add`
  command when there is a [vendored copy](/ui/vendored) to write;
- **All states**: every component in every variant and state it takes
  (default, disabled, error, loading, with and without an icon) and every
  built-in widget, for comparing the kit before and after a change.

Every page has a light/dark toggle. It needs no app: run it anywhere.

```sh
vuka ui gallery
```

It serves the gallery at `http://localhost:7357`; `--addr` serves it on
another address. The [vendored components](/ui/components/) have pages of
their own in these docs.

## Previewing an app's theme

`--css` loads a stylesheet after the kit's, so its `--ui-*` overrides win the
way they do in the app. Give it the stylesheet the browser loads, the built
CSS, not a Tailwind source with `@import` lines:

```sh
vuka ui gallery --css static/app.css
```

The file is read again on every reload: change a token, reload, and every
component shows it. `--css` repeats; the files load in the order given, so a
copy of the kit's `preset-shadcn.css` goes before the app's own block:

```sh
vuka ui gallery --css preset-shadcn.css --css static/app.css
```

## Writing the pages

`--out` writes the pages to a directory instead of serving them: `index.html`,
a page per component under `components/`, `states.html`, and `light.html` and
`dark.html`, the states page fixed in one theme. Each page is one file with its
styles inside. Screenshots of `light.html` and `dark.html` taken before and
after a change show what the change did.

```sh
vuka ui gallery --out gallery --css static/app.css
```

## Where the examples come from

The gallery is package `github.com/vuka-lang/ui/v2/gallery`. Each example is
a `.vuka.html` file in its `examples` package, compiled like the kit's own
components, and the page shows that file's text: what renders and what is
shown come from one source. The props tables are generated from the kit's Go.
`gallery.Handler(opts)` serves the pages and `gallery.Write(dir, opts)` writes
them; an app can mount the handler on a development route of its own.
