# Vuka for Zed

A [Zed](https://zed.dev) extension for the [Vuka](https://vuka-lang.github.io/vuka/)
language — `.vuka` files.

Vuka is a **Go superset**: every `.vuka` file is Go plus a few additions —
`Result`/`Option`, the `?` operator, `match`/`case` pattern matching,
`@decorator` and typed `@T{…}` attributes (including field attributes after
struct fields), statics (`Type.X`), `enum` declarations, and JSX component
markup (`<div>…</div>`, `{expr}`).

This extension gives you:

- **Syntax highlighting** for `.vuka` files, via the **tree-sitter-go** grammar
  (see [the grammar limitation](#grammar-limitation) below).
- **Full language intelligence** through the **`vuka lsp`** language server:
  completion, hover, signature help, go-to-definition, references, rename,
  document/workspace symbols, code actions, inlay hints and diagnostics — the
  same features the VS Code extension provides. `vuka lsp` is
  [gopls](https://go.dev/gopls) behind a proxy that keeps the Go generated from
  each `.vuka` file open in gopls as an unsaved buffer and maps every request
  and response back onto your `.vuka` source. Overloads and statics show the way
  you wrote them (`area`, `User.New`), and Vuka's own errors plus Go's type
  errors land on the right `.vuka` lines.

## Prerequisites

The extension does **not** bundle a binary. Install the Vuka toolchain (which
provides `vuka lsp`) and gopls:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
go install golang.org/x/tools/gopls@latest
```

Make sure `"$(go env GOPATH)/bin"` is on your `PATH` so Zed can find `vuka`.

## Install as a dev extension

This extension lives inside the Vuka repository at `editors/zed`. To run it:

1. Open Zed.
2. Open the command palette and run **`zed: install dev extension`** (or:
   Extensions view → **Install Dev Extension**).
3. Pick this folder: `editors/zed`.

Zed compiles the Rust to WebAssembly and downloads/builds the tree-sitter-go
grammar for you — no Rust toolchain setup is required just to install it. Then
open a `.vuka` file: highlighting appears immediately, and the language server
starts once `vuka` is resolved.

## How the language server is wired

The extension's WASM shell (`src/lib.rs`) resolves and launches `vuka lsp` over
stdio. The `vuka` binary is resolved, in order:

1. an explicit path from your Zed settings
   (`lsp.vuka.binary.path`), otherwise
2. `vuka` on the worktree `PATH`.

Arguments default to `["lsp"]`. Both can be overridden in `settings.json`:

```jsonc
{
  "lsp": {
    "vuka": {
      "binary": {
        "path": "/absolute/path/to/vuka",
        "arguments": ["lsp", "--log", "/tmp/vuka-lsp.log"]
      }
    }
  }
}
```

If `vuka` can't be found, the server shows an error telling you to install it or
set `lsp.vuka.binary.path`.

### Go files and templ files

`vuka lsp` is a drop-in gopls, so Go files in a package that also has `.vuka`
files can be served through Vuka too (point the Go extension's gopls at a link
named `gopls` → `vuka`), and `.templ` files can use the same gopls. Zed wires
gopls through its built-in Go support rather than this extension; see the
[Vuka editors guide](https://vuka-lang.github.io/vuka/tools/editors) for the
cross-server setup. This extension focuses on `.vuka` files.

## Grammar limitation

Vuka has **no tree-sitter grammar of its own yet**, so this extension references
**tree-sitter-go** for the Go majority of each file. tree-sitter's error
recovery keeps Go highlighting working across the Vuka-only constructs, and the
queries in `languages/vuka/highlights.scm` add best-effort coloring for Vuka
keywords (`match`, `enum`, `static`, `decorator`), the `Result`/`Option` types
and `Ok`/`Err`/`Some`/`None`.

What the Go grammar can't model — and therefore isn't reliably highlighted
statically — is the genuinely non-Go syntax:

- JSX markup (`<div>…</div>`, `{expr}`, components, blocks),
- the `@decorator` / `@T{…}` sigil and field attributes,
- `match`-arm patterns and `enum` variant bodies.

These regions may highlight imperfectly (tree-sitter sees them as parse errors).
**The `vuka lsp` server provides the accurate intelligence** for all of them
regardless — completion, hover, navigation, rename and diagnostics are correct
even where the static coloring is not.

A dedicated **`tree-sitter-vuka`** grammar (covering JSX, decorators, match arms,
enums and field attributes) is the future enhancement; `extension.toml` and the
`languages/vuka/*.scm` queries would then point at it instead of tree-sitter-go,
and `injections.scm` could embed Go inside JSX braces. The VS Code extension's
TextMate grammar (`editors/vscode/syntaxes/vuka.tmLanguage.json`) is the
reference for what such a grammar should cover.

## Layout

```
editors/zed/
  extension.toml                 # Zed manifest: language server + grammar reference
  Cargo.toml, src/lib.rs         # WASM shell: locates & launches `vuka lsp`
  languages/vuka/
    config.toml                  # language config (suffix .vuka, comments, brackets)
    highlights.scm               # syntax highlighting (tree-sitter-go node names)
    outline.scm                  # document outline for Go declarations
    injections.scm               # (placeholder; needs tree-sitter-vuka)
```

## License

MIT.
