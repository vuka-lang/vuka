# Vuka for VS Code

Language support for [Vuka](https://github.com/vuka-lang/vuka): Go with Result,
Option, `?`, `match`, overloading and attributes.

- Completion, hover, signature help, go to definition, references, rename,
  outline, code actions and inlay hints, through gopls
- Diagnostics from Vuka (non-exhaustive matches, ambiguous overloads, misplaced
  `?`) and from Go's type checker, on the `.vuka` lines
- Syntax highlighting for Vuka's additions (`@attributes`, `decorator`, `match`, `?`, Result/Option) on top of Go's grammar,
  inside function bodies too
- JSX highlighting: tags and components (`<div>`, `<Card />`, `<card.Card>`), fragments, attributes
  (`data-id={x}`, `hx-get="/x"`), text, and the Go inside `{ … }` (expressions, `{for …}`, `{if …}` blocks)
- `.templ` files beside `.vuka` files are compiled by vuka itself (no `templ generate`); go to definition
  from Vuka code lands in the `.templ` file. Use templ's own extension for editing `.templ` files.

## Requirements

```
go install github.com/vuka-lang/vuka/cmd/vuka@latest
go install golang.org/x/tools/gopls@latest
```

The extension runs `vuka lsp`, which runs gopls and keeps the Go generated from
your `.vuka` files open in it, so nothing is written to your tree.

## Go files

The extension offers, once, to serve your Go files through Vuka as well: it
links `gopls` to `vuka` in its storage folder and sets the Go extension's
`go.alternateTools.gopls` to it. `.go` files in a package with `.vuka` files then
see the code those define. **Vuka: Serve Go Files Too** and **Vuka: Stop Serving
Go Files** switch it on and off; a `go.alternateTools.gopls` you set yourself is
left alone.

## templ files

`.templ` files get templ's icon. Install the
[templ extension](https://marketplace.visualstudio.com/items?itemName=a-h.templ)
for their highlighting and language server; Vuka only adds the icon, under the
same `templ` language, so the two work side by side. The icon is templ's logo,
by Adrian Hesketh, used under templ's MIT license (`images/TEMPL-LICENSE.txt`).

When the workspace has both `.vuka` and `.templ` files, the extension offers,
once, to let templ's language server use Vuka as its gopls, so `.templ` files
see code from `.vuka` files (a component from a `.vuka` file called as
`@Nav(current)` is no longer "undefined"; hover and go to definition reach it).
It writes a `templ` script (`templ.cmd` on Windows) beside the `gopls` link that
puts the link first on `PATH` and runs your templ, and sets
`templ.executablePath` to it; templ remains the only server for `.templ` files.
**Vuka: Let templ Use Vuka as Its gopls** and **Vuka: Stop templ Using Vuka**
switch it on and off, followed by a restart of templ's language server. A
`templ.executablePath` you set yourself is left alone.

## Settings

| Setting | Default | |
|---|---|---|
| `vuka.path` | `vuka` | the vuka binary |
| `vuka.goplsPath` | | gopls; empty finds it on PATH, `$GOBIN` or `$GOPATH/bin` |
| `vuka.trace.server` | `off` | log the LSP traffic |
| `vuka.sharedGopls` | `true` | one gopls daemon shared by every session (`-remote=auto`) |
| `vuka.offerGoDropIn` | `true` | offer to serve Go files through Vuka |
| `vuka.offerTemplDropIn` | `true` | offer to let templ's language server use Vuka as its gopls |

Run **Vuka: Restart Language Server** after reinstalling vuka.
