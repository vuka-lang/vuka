# Editors

`vuka lsp` is a language server for `.vuka` files: [gopls](https://go.dev/gopls)
behind a proxy that keeps the Go generated from each `.vuka` file open in gopls
as an unsaved buffer — never written to disk — and maps every position both
ways.

Completion, hover, signature help, go to definition, references, rename,
outline, code actions, inlay hints and formatting (by
[`vuka fmt`](/tools/cli#fmt); the VS Code extension formats on save) work in
`.vuka` files. Vuka's own errors
and Go's type errors appear on the `.vuka` lines; an import of a folder of your
module written without the module path gets a hint with the right one. Overloads and statics show as
you wrote them (`area`, `User.New`), and typing `@` lists the project's
decorators.
A [`vuka.File`](/features/decorators#files) path such as `@Page("views/pets.templ")` is a link:
go to definition opens the file (a `.templ` file with one component, at that
component), and hover shows the path and the `.templ` file's components.

### Markup

JSX is served as markup, from the `.vuka` text itself, so it works while a tag
is half-typed:

- completion: after `<` every HTML element (and the SVG most pages draw with)
  beside the components in scope, each with a one-line description and its
  MDN link; in a tag the element's own attributes first (`href` on `<a>`,
  `colspan` on `<td>`, `type`, `value` and `placeholder` on `<input>`), then
  the global ones (`className`, `id`, `style`, `key`…), the event handlers
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
- go to definition, references and rename on the Go inside braces

Install gopls:

```sh
go install golang.org/x/tools/gopls@latest
```

## VS Code

The extension lives in
[`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode):
the language server, highlighting for Vuka's additions on top of Go's grammar
(field attributes, statics, `match` patterns, JSX with its blocks, event
attributes and components — the grammar these pages use), snippets for
components, live pages, routes, models and decorators, closing JSX tags as you
type (`>` adds the closing tag, `</` the name of the element still open, inside
blocks and fragments too), Enter between tags indenting, `{` and quotes pairing
in markup, and the Vuka icons.

VS Code's Emmet isn't turned on for `.vuka` files: Emmet can't tell the markup
from the Go around it, so it would offer abbreviations for every identifier
typed in Go code. The markup completion above covers elements and attributes.
To have Emmet anyway, add `"emmet.includeLanguages": {"vuka": "javascriptreact"}`
to your settings, and expand abbreviations with Tab. The language status shows `vuka version`; clicking
it restarts the server.

### Go files

The extension offers, once, to serve your Go files through Vuka as well. Run
under the name `gopls`, vuka is a drop-in gopls, so `.go` files in a package
with `.vuka` files see the code those define — instead of "undefined" errors.
The extension links `gopls` to `vuka` in its storage folder and sets the Go
extension's `go.alternateTools.gopls` to it; **Vuka: Serve Go Files Too** and
**Vuka: Stop Serving Go Files** switch it on and off.

### templ files

templ's own extension serves `.templ` files with `templ lsp`, which runs gopls.
When a workspace has both `.vuka` and `.templ` files, the Vuka extension offers,
once, to make that gopls Vuka: it writes a small `templ` wrapper next to the
`gopls` link that puts the link first on `PATH`, and sets `templ.executablePath`
to it. A `.templ` file then sees components defined in `.vuka` files —
`@Nav(current)` type-checks, and hover and go to definition reach the `.vuka`
file — while templ stays the only server for `.templ` files. **Vuka: Let templ
Use Vuka as Its gopls** and **Vuka: Stop templ Using Vuka** switch it on and
off; a `templ.executablePath` you set yourself is left alone.

Both servers use gopls's shared daemon (`-remote=auto`), so the type-checking is
done once for the whole editor.

## Other editors

Run `vuka lsp` over stdio for the `vuka` file type. For Go files, point the
editor's gopls at a link named `gopls` to `vuka`; for `.templ` files, run
`templ lsp` with that link first on `PATH`.
