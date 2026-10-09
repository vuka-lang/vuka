# Vuka for VS Code

Language support for [Vuka](https://github.com/vuka-lang/vuka): Go with Result,
Option, `?`, `match`, overloading and attributes.

- Completion, hover, signature help, go to definition, references, rename,
  outline, code actions and inlay hints, through gopls
- Diagnostics from Vuka (non-exhaustive matches, ambiguous overloads, misplaced
  `?`) and from Go's type checker, on the `.vuka` lines
- Syntax highlighting for Vuka's additions (`@attributes`, `decorator`, `match`, `?`, Result/Option) on top of Go's grammar

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

## Settings

| Setting | Default | |
|---|---|---|
| `vuka.path` | `vuka` | the vuka binary |
| `vuka.goplsPath` | | gopls; empty finds it on PATH, `$GOBIN` or `$GOPATH/bin` |
| `vuka.trace.server` | `off` | log the LSP traffic |
| `vuka.sharedGopls` | `true` | one gopls daemon shared by every session (`-remote=auto`) |
| `vuka.offerGoDropIn` | `true` | offer to serve Go files through Vuka |

Run **Vuka: Restart Language Server** after reinstalling vuka.
