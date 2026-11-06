# Vuka for Zed

A [Zed](https://zed.dev) extension for [Vuka](https://vuka-lang.github.io/):
`.vuka` files and `.vuka.html` shapes, highlighted by Vuka's own grammars and
served by `vuka lsp`, the language server the VS Code extension runs.

## Installing

The extension bundles no `vuka`. Install it, and gopls:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
go install golang.org/x/tools/gopls@latest
```

or add it to your module, where the extension runs it with `go tool vuka`:

```sh
go get -tool github.com/vuka-lang/vuka/cmd/vuka/v2
```

Then, until the extension is in Zed's registry, install it from a checkout of
this repository: **zed: install dev extension** in the command palette, and
pick `editors/zed`. Zed builds the extension and fetches the grammars from the
revision `extension.toml` names. Once it is published, **zed: extensions**,
search for Vuka, **Install**.

## What It Does

| | VS Code | Zed |
|---|---|---|
| `.vuka` highlighting | TextMate grammar | tree-sitter grammar: `?`, `match`/`case` with guards, `enum`, `decorator`, `@attributes` (and `@T{…}`, field and parameter attributes), statics, markup with its holes, `{if}`/`{for}`/`{match}` blocks and `<script>`/`<style>` |
| `.vuka.html` highlighting | TextMate grammar | tree-sitter grammar: the Go header or `<script lang="go">`, markup, holes in text, attributes and strings, statement lines, `<script lang="ts">` as TypeScript, `<style>` as CSS |
| Shape names coloured by the server (props, state, events, components) | semantic tokens | semantic tokens, with `"semantic_tokens": "combined"` (see [Settings](#settings)) |
| Completion, hover, signature help, definition, references, rename, symbols, diagnostics, inlay hints, quick fixes | `vuka lsp` | `vuka lsp` |
| TypeScript in `<script lang="ts">` | bundled server | installed by the extension with Zed's Node, unless one is on `PATH` |
| Format on save | `vuka fmt`, through the server | `vuka fmt`, through the server (Zed's default formatter) |
| Organize imports on save | on by default | one setting (see [Settings](#settings)) |
| Closing tags as you type | `>` and `</` | `>` adds the closing tag |
| Linked editing of a tag's name | yes | yes |
| Brackets, quotes, comments, indentation | `language-configuration.json` | `config.toml`, `brackets.scm`, `indents.scm`; `{/* */}` comments in markup |
| Outline, text objects, rainbow brackets | outline | outline, function and class text objects, brackets |
| Snippets | yes | the same snippets |
| Tests and tasks | — | run buttons on `Test…` functions and packages; `vuka run -watch`, `vuka test`, `vuka routes` tasks |
| Finding `vuka` | setting, login shell `PATH`, `$GOBIN`, `$GOPATH/bin`, `~/go/bin` | setting, `go tool vuka`, `PATH`, `$GOBIN`, `$GOPATH/bin`, `~/go/bin` |
| Restart the server | **Vuka: Restart Language Server** | **editor: restart language server** |
| `vuka version` in the status bar | yes | no: Zed has no place for it |
| **Vuka: Show Routes** | yes | the `vuka routes` task |
| **Vuka: Extract Component** | asks the new component's name | no: Zed can't ask for the name the server needs |
| Serving `.go` and `.templ` files through Vuka | offered, set up for you | by hand (see [Go and templ files](#go-and-templ-files)) |
| Tailwind CSS | set up for you | by hand (see [Settings](#settings)) |
| Emmet | in `.vuka.html` | no: the Emmet extension's server serves the languages it lists |

## Settings

In Zed's `settings.json`:

```jsonc
{
  "languages": {
    "Vuka": {
      "semantic_tokens": "combined",
      "code_actions_on_format": { "source.organizeImports": true }
    },
    "Vuka HTML": {
      "semantic_tokens": "combined",
      "code_actions_on_format": { "source.organizeImports": true }
    }
  },
  "lsp": {
    "vuka": {
      "binary": {
        "path": "/absolute/path/to/vuka",
        "arguments": ["lsp", "-log", "/tmp/vuka-lsp.log"]
      },
      "settings": {
        "goplsPath": "/absolute/path/to/gopls",
        "sharedGopls": true
      },
      "initialization_options": {
        "vuka": { "implicitContextHints": true }
      }
    }
  }
}
```

- `semantic_tokens`: `"combined"` lays the server's colours over the
  grammar's: a shape's props, state, browser state, events and components
  get their own (`languages/*/semantic_token_rules.json` maps them to your
  theme). Zed leaves it off by default.
- `code_actions_on_format`: organizes imports before each format, as VS
  Code does on save — removes the unused ones, adds the missing ones, groups
  them.
- `lsp.vuka.binary.path`: the `vuka` to run; `arguments` replaces `["lsp"]`.
- `lsp.vuka.settings`: `goplsPath` is the gopls vuka runs (default: gopls on
  `PATH`, `$GOBIN`, `$GOPATH/bin`); `sharedGopls: false` gives this server a
  gopls of its own instead of the shared daemon.
- `initialization_options`: `implicitContextHints: false` hides the `ctx`
  inlay hints in markup.

**Tailwind CSS.** Add the server to the languages and tell it they are HTML.
Zed names them `vuka html` and `vuka` to that server, and an
`includeLanguages` of your own replaces Zed's, so keep the languages you use
it for:

```jsonc
{
  "languages": {
    "Vuka": { "language_servers": ["vuka", "tailwindcss-language-server", "..."] },
    "Vuka HTML": { "language_servers": ["vuka", "tailwindcss-language-server", "..."] }
  },
  "lsp": {
    "tailwindcss-language-server": {
      "settings": {
        "includeLanguages": { "vuka html": "html", "vuka": "html", "html": "html", "css": "css" }
      }
    }
  }
}
```

Class completion then works in strings (`class="…"`, and the strings of
`class={…}`).

**Tasks.** Run **task: spawn** for `vuka run -watch`, `vuka test` and
`vuka routes`; the run button beside a `Test…` function runs that test. To
bind one to a key, or to run the watcher in a module below the worktree root,
add it to `.zed/tasks.json`:

```json
[
  {
    "label": "vuka run -watch",
    "command": "vuka",
    "args": ["run", "-watch", "."],
    "cwd": "$ZED_WORKTREE_ROOT/app"
  }
]
```

## File Icons

The extension ships an icon theme, **Vuka Icons**, that gives `.vuka` and
`.vuka.html` files a V icon in the project panel and tabs. Zed applies only
the icon theme you select, so pick it: **icon theme selector: toggle** in the
command palette, then **Vuka Icons**, or in `settings.json`:

```json
{ "icon_theme": "Vuka Icons" }
```

Every other file keeps its standard icon. Zed doesn't merge an icon theme's
file associations with its default theme's, so this theme carries a copy of
the default's (`gen_icon_theme.go` writes them from Zed's source), and uses
Zed's own icons for them.

## Go and templ Files

Run under the name `gopls`, `vuka` is a drop-in gopls, so Go files in a
package with `.vuka` files see the code those define. Link it and give Zed's
Go support the link:

```sh
mkdir -p ~/.vuka/bin && ln -sf "$(command -v vuka)" ~/.vuka/bin/gopls
```

```json
{ "lsp": { "gopls": { "binary": { "path": "/Users/you/.vuka/bin/gopls" } } } }
```

For `.templ` files, run templ with that directory first on `PATH`: a script
such as `~/.vuka/bin/templ`

```sh
#!/bin/sh
PATH="$HOME/.vuka/bin:$PATH" exec "$(go env GOPATH)/bin/templ" "$@"
```

and `{ "lsp": { "templ": { "binary": { "path": "/Users/you/.vuka/bin/templ" } } } }`.

## Troubleshooting

- **Stale answers after upgrading vuka.** The running server is the old
  binary: run **editor: restart language server** (or reopen the project).
  The same after `go get -tool` moves the module's vuka.
- **"vuka not found".** The message lists where the extension looked. Zed
  reads `PATH` from your login shell; put `$(go env GOPATH)/bin` on it, or set
  `lsp.vuka.binary.path`.
- **No colours from the server in shapes.** Set `semantic_tokens` (above),
  then restart the server.
- **What the server is doing.** Add `"-log", "/tmp/vuka-lsp.log"` to
  `lsp.vuka.binary.arguments`, or open **dev: open language server logs**.
- **Highlighting looks wrong after editing the grammar.** Zed builds the
  grammars from the revision in `extension.toml`, not the working tree; see
  below.

## Developing

```
editors/zed/
  extension.toml        manifest: the grammars, the server, capabilities
  src/lib.rs            finds vuka and starts vuka lsp
  languages/vuka/       config.toml and the queries for .vuka files
  languages/vuka-html/  the same for .vuka.html files
  snippets/             the VS Code extension's snippets
  icon_themes/vuka.json the Vuka Icons theme, written by gen_icon_theme.go
  icons/vuka.svg        the .vuka and .vuka.html icon
grammars/tree-sitter-vuka/
  vuka/                 grammar.js over tree-sitter-go, test/corpus
  vuka_html/            grammar.js over vuka, test/corpus
  common/scanner.h      the scanner both share
```

The grammars live in this repository and Zed fetches them by commit. After
changing one:

```sh
cd grammars/tree-sitter-vuka
npm ci
npm run generate   # writes vuka/src and vuka_html/src, committed
npm test           # the corpus tests
```

then commit, and set `rev` of both grammars in `extension.toml` to that
commit. CI checks the generated parsers are current, the corpus passes, every
query compiles against its grammar, the Go part of the two highlights files
agrees, and the snippets match the VS Code extension's.

The icon theme's file associations are Zed's default theme's at the Zed
commit `gen_icon_theme.go` names. To take newer ones, set `zedRev` there and
run `go run gen_icon_theme.go` in `editors/zed`.

## License

MIT.
