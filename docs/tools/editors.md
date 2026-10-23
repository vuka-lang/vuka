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

### Emmet

VS Code's Emmet isn't turned on for `.vuka` files: Emmet can't tell the markup
from the Go around it, so it would offer abbreviations for every identifier
typed in Go code. The markup completion above covers elements and attributes.
To have Emmet anyway, add `"emmet.includeLanguages": {"vuka": "javascriptreact"}`
to your settings, and expand abbreviations with Tab.

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
