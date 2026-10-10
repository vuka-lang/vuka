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
type, and the Vuka icons. The language status shows `vuka version`; clicking
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
