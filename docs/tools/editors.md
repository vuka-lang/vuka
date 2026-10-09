# Editors

`vuka lsp` is a language server for `.vuka` files: [gopls](https://go.dev/gopls)
behind a proxy that keeps the Go generated from each `.vuka` file open in gopls
as an unsaved buffer — never written to disk — and maps every position both
ways.

Completion, hover, signature help, go to definition, references, rename,
outline, code actions and inlay hints work in `.vuka` files. Vuka's own errors
and Go's type errors appear on the `.vuka` lines. Overloads and statics show as
you wrote them (`area`, `User.New`), and typing `@` lists the project's
decorators.

Install gopls:

```sh
go install golang.org/x/tools/gopls@latest
```

## VS Code

The extension lives in
[`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode):
the language server, highlighting for Vuka's additions on top of Go's grammar,
and the Vuka icons.

### Go files

The extension offers, once, to serve your Go files through Vuka as well. Run
under the name `gopls`, vuka is a drop-in gopls, so `.go` files in a package
with `.vuka` files see the code those define — instead of "undefined" errors.
The extension links `gopls` to `vuka` in its storage folder and sets the Go
extension's `go.alternateTools.gopls` to it; **Vuka: Serve Go Files Too** and
**Vuka: Stop Serving Go Files** switch it on and off.

Both servers use gopls's shared daemon (`-remote=auto`), so the type-checking is
done once for the whole editor.

## Other editors

Run `vuka lsp` over stdio for the `vuka` file type. For Go files, point the
editor's gopls at a link named `gopls` to `vuka`.
