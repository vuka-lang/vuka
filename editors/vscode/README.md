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

## Settings

| Setting | Default | |
|---|---|---|
| `vuka.path` | `vuka` | the vuka binary |
| `vuka.goplsPath` | | gopls; empty finds it on PATH, `$GOBIN` or `$GOPATH/bin` |
| `vuka.trace.server` | `off` | log the LSP traffic |

Run **Vuka: Restart Language Server** after reinstalling vuka.
