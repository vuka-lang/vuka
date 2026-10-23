# Editors

[[toc]]

## Introduction

`vuka lsp` is a language server for `.vuka` files: [gopls](https://go.dev/gopls)
behind a proxy that keeps the Go generated from each `.vuka` file open in gopls
as an unsaved buffer — never written to disk — and maps every position both
ways.

You get the editor features you know from Go in `.vuka` and `.vuka.html`
files, through extensions for VS Code and GoLand or any editor that speaks LSP.

## What the Language Server Does {#what-the-language-server-does}

Completion, hover, signature help, go to definition, references, rename,
outline, code actions, inlay hints and formatting (by
[`vuka fmt`](/tools/cli#fmt); the VS Code extension formats on save) work in
`.vuka` files.

Vuka's own errors
and Go's type errors appear on the `.vuka` lines; an import of a folder of your
module written without the module path gets a hint with the right one. Overloads and statics show as
you wrote them (`area`, `User.New`), and typing `@` lists the project's
decorators.

### Attributes and Decorators

After a struct field, `@orm.` lists what the package has for a
field attribute — its types (`Char{…}`, `Column("…")`, `PK`) and the
functions returning one (`DefaultTo(…)`), types declaring
[`vuka.OnField`](/features/attributes#targets) first and types whose targets
leave fields out omitted.

A [`vuka.File`](/features/decorators#files) path such as `@Page("views/pets.templ")` is a link:
go to definition opens the file (a `.templ` file with one component, at that
component), and hover shows the path and the `.templ` file's components.

Hover on an [`@ui.Island`](/web/islands#declare-the-props-in-go) shows the
component file it names or infers (relative to the module root) and its
framework; go to definition opens it; a missing or ambiguous file is an error
on the decorator as you type, the build's message.

Hover on a method
named `Mount`, `Changed` or `Load<Group>` that no hook attribute marks says
it isn't a hook (a hook is declared with `@ui.Mount`, `@ui.OnChange` or
`@ui.Loads`), and names the attribute.

### Shape Files {#templates}

`.vuka.html` files are served like `.vuka` files: hover, go to definition,
completion and rename on the Go header, holes and statements go
through the Go they compile to (a bare `{Title}` is the receiver's field, so
go to definition lands on it in the shape header), and their errors show at
their lines.

VS Code highlights them as HTML with Go in braces and at line
starts, TypeScript in `<script lang="ts">` and CSS in `<style>`, Zed reads the markup as HTML.

A template's `<script lang="ts">` is TypeScript with its component's
types (`component<Chart>`: `props`, `ctx.live`, `ctx.state`): completion,
hover, signature help, go to definition and errors come from
[typescript-language-server](https://github.com/typescript-language-server/typescript-language-server),
which `vuka lsp` starts when one is on `PATH`, in the module's or its
frontend's `node_modules/.bin`, or named by `$VUKA_TSLS`
(`npm i -D typescript typescript-language-server`).

### Editing Markup {#markup}

Shape markup is served from the `.vuka.html` text itself, so it works while a tag
is half-typed. It offers:

- completion: after `<` every HTML element (and the SVG most pages draw with)
  beside the components in scope, each with a one-line description and its
  MDN link; in a tag the element's own attributes first (`href` on `<a>`,
  `colspan` on `<td>`, `type`, `value` and `placeholder` on `<input>`), then
  the global ones (`class`, `id`, `style`, `key`…), the event handlers
  (`onClick`, `onInput`…), `aria-*` and `data-`; inside the quotes of an
  enumerated attribute its values (`type="checkbox"`, `target="_blank"`,
  `rel="noopener"`); after `</` the element still open; after the `{` of a
  child, `for`, `if` and `match` complete to whole blocks; a component's
  attributes are its parameters or props, and the Go in braces completes as
  Go, the surrounding tags unfinished or not
- hover on an element or an attribute: its description and MDN link
- linked editing: renaming an opening tag renames its closing tag as you type
  (`editor.linkedEditing`, on for `.vuka` files in VS Code); highlights pair
  the two
- folding of elements and blocks, beside gopls's folding of the Go
- one diagnostic at an unclosed tag while it is typed, not a flood
- each diagnostic with its code, linked to its page in the
  [catalogue](/errors/), and its help; what `vuka vet` reports of an island
  page (a deferred field without a loader, a bad `bind:` tag) shows as you
  type
- go to definition, references and rename on the Go inside braces

### Completing Attributes, Events and Refs {#attributes-events-refs}

In a tag, completion offers every ARIA attribute (`aria-pressed`,
`aria-current`, `aria-level`, `aria-relevant`…) and inside its quotes the
values it takes (`true` `false` `mixed` for `aria-pressed`; `page` `step`
`location` `date` `time` for `aria-current`), and the WAI-ARIA roles for
`role`.

Event handlers are the ones the element fires: `onClick`, `onInput`,
`onKeyDown` and the pointer, focus and drag events on any element, `onSubmit`
and `onReset` on a `<form>`, `onClose` and `onCancel` on a `<dialog>`,
`onToggle` on `<details>`, `<dialog>` and popovers, the playback events on
`<audio>` and `<video>`. Each item says the handlers Vuka takes: a method
(`{Save}`), a call (`{Add(2)}`), browser state (`{open = !open}`), a func or
a `ui.JS(…)` command. The event's name can be written in any case
(`onkeydown`).

`ref` names an element for [`ui.Ref`](/ui/js#targets-and-refs); inside the
quotes of `ui.Ref("…")`, completion lists the refs the same file declares.

### Keying a Loop's Items {#keying-loops}

A loop's items need a `key` ([W0102](/errors/W0102)). Typing `for` on a line
of markup offers a loop whose first element is keyed:

```html
for _, item := range items {
  <li key={item.ID}></li>
}
```

In a loop you wrote, the first element completes with the key: when the
item's type has an `ID` field (or `Id`, `Key`, `UUID`, `Slug`, `Code`, or a
field ending in `ID`), `<l` completes to `<li key={t.ID}`.

### Keeping the Struct in View {#outline}

The outline of a shape is its struct holding its fields, its methods and its
markup's elements (`section#todos.list`, its children inside). The struct spans
the file down to the end of its markup, so with sticky scroll
(`editor.stickyScroll.enabled`) the `type … struct` line stays pinned above the
markup you scroll through, with the elements around the cursor below it.

The Go header folds as one, from the package clause to the line before the
markup, beside the folds of its declarations and of the markup's elements and
blocks.

### Formatting a Selection {#format-selection}

Format Selection (`textDocument/rangeFormatting`) formats only the selected
lines of a `.vuka` or `.vuka.html` file by [`vuka fmt`](/tools/cli#fmt)'s
rules; the rest of the file stays as it is. A file that doesn't parse is left
alone.

## Installing gopls {#installing-gopls}

The language server needs gopls. Install it:

```sh
go install golang.org/x/tools/gopls@latest
```

## VS Code {#vs-code}

The extension lives in
[`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode):
the language server, highlighting for Vuka's additions on top of Go's grammar
(field attributes, statics, `match` patterns, shape markup with its blocks, event
attributes and components — the grammar these pages use), snippets for
components, live pages, routes, models and decorators, closing markup tags as you
type (`>` adds the closing tag, `</` the name of the element still open, inside
blocks and fragments too), Enter between tags indenting, `{` and quotes pairing
in markup, and the Vuka icons.

### Tailwind CSS {#tailwind}

With [Tailwind CSS IntelliSense](https://marketplace.visualstudio.com/items?itemName=bradlc.vscode-tailwindcss)
installed, class completion, hover, color swatches and linting work in
`.vuka.html` and `.vuka` files. The Vuka extension sets its defaults:
`tailwindCSS.includeLanguages` reads both as HTML, and
`tailwindCSS.experimental.classRegex` finds classes in:

- `class="…"`
- the strings of `class={…}`: `class={"mt-2 " + extra + " text-sm"}`
- the strings of a Go function whose name ends in `Class` or `Classes`, where
  it is called (`tabClass(on, "px-5")`) and in its body:

```go
func tabClass(on bool, extra string) string {
    if on {
        return "bg-blue-600 text-white " + extra
    }
    return "text-gray-700"
}
```

> [!NOTE]
> A `tailwindCSS.includeLanguages` or `tailwindCSS.experimental.classRegex`
> of your own replaces the extension's. Copy the Vuka entries into it from
> the extension's `package.json` (`contributes.configurationDefaults`).

### Emmet

Emmet abbreviations expand in `.vuka.html` files: the extension sets
`"emmet.includeLanguages": {"vuka-html": "html"}`. `.vuka` files are left
out, where markup sits inside Go functions.

> [!WARNING]
> Emmet reads the whole file as HTML; it can't tell the Go header from the
> markup. In the header it can suggest an abbreviation for a word that is also
> a tag name (`map`, `select`) or has a dot (`p.Q`). The header is still Go to
> the editor — its comments, brackets and snippets are Go's. To turn Emmet off
> for shapes, set `"emmet.excludeLanguages": ["vuka-html"]`.

The language status shows `vuka version`; clicking
it restarts the server.

### Serving Go Files {#go-files}

The extension offers, once, to serve your Go files through Vuka as well. Run
under the name `gopls`, vuka is a drop-in gopls, so `.go` files in a package
with `.vuka` files see the code those define — instead of "undefined" errors.

The extension links `gopls` to `vuka` in its storage folder and sets the Go
extension's `go.alternateTools.gopls` to it; **Vuka: Serve Go Files Too** and
**Vuka: Stop Serving Go Files** switch it on and off.

### Serving templ Files {#templ-files}

templ's own extension serves `.templ` files with `templ lsp`, which runs gopls.
When a workspace has both `.vuka` and `.templ` files, the Vuka extension offers,
once, to make that gopls Vuka: it writes a small `templ` wrapper next to the
`gopls` link that puts the link first on `PATH`, and sets `templ.executablePath`
to it. A `.templ` file then sees components defined in `.vuka` files —
`@Nav(current)` type-checks, and hover and go to definition reach the `.vuka`
file — while templ stays the only server for `.templ` files.

**Vuka: Let templ
Use Vuka as Its gopls** and **Vuka: Stop templ Using Vuka** switch it on and
off; a `templ.executablePath` you set yourself is left alone.

> [!NOTE]
> Both servers use gopls's shared daemon (`-remote=auto`), so the type-checking is
> done once for the whole editor.

## GoLand {#goland}

The plugin lives in
[`editors/goland`](https://github.com/vuka-lang/vuka/tree/main/editors/goland),
for GoLand and the other JetBrains IDEs with LSP support (2025.1 and later):
the language server for `.vuka` and `.vuka.html` files, highlighting of
`.vuka` files by the VS Code extension's grammar, and its snippets. It runs
`go tool vuka lsp` when `go.mod` declares vuka as a tool, else `vuka lsp`;
**Settings | Languages & Frameworks | Vuka** sets the binaries.

`.vuka.html` files open as HTML. GoLand serves `.go` files itself, not through gopls, so
`.go` files don't see code declared in `.vuka` files.

## Other Editors {#other-editors}

Run `vuka lsp` over stdio for the `vuka` file type. For Go files, point the
editor's gopls at a link named `gopls` to `vuka`; for `.templ` files, run
`templ lsp` with that link first on `PATH`.
