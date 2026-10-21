# Vuka for GoLand

A plugin for GoLand (and the other JetBrains IDEs with LSP support, 2025.1
and later) for [Vuka](https://vuka-lang.github.io/): `.vuka` files and
`.vuka.html` templates.

- **Language intelligence** through `vuka lsp`: completion, hover, signature
  help, go to definition, references, rename, diagnostics, code actions,
  inlay hints and formatting (`vuka fmt`). `vuka lsp` is gopls behind a proxy
  that keeps the Go generated from each `.vuka` file open in gopls, so
  nothing is written to your tree.
- **Highlighting** of `.vuka` and `.vuka.html` files by the VS Code
  extension's TextMate grammars (`editors/vscode/syntaxes`), which the build
  ships with the plugin.
- **Closing tags** as you type in `.vuka` files: an opening tag's `>` adds
  its closing tag, `</` the name of the element still open; Enter after an
  opening tag indents, and between two tags puts the closer on its own line.
- **Formatting on save**, by `vuka lsp` (`vuka fmt`).
- **Live templates** made from the VS Code extension's snippets at build
  time: `live`, `component`, `matchr`, `matcho`, `jfor`, `jmatch`, `route`,
  `routet`, `livepage`, `webmain`, `model`, `decorator`, `decoratorp`, `tx`,
  `static`, `sfunc` and their other names.
- **HTML in `ui.HTML(`…`)` strings** in `.go` files: highlighting,
  completion and tag matching.
- **The Vuka file icons**, and the server in the status bar's Language
  Services widget, linking to the settings, which show the running
  `vuka version`.
- **Vuka: Restart Language Server** in Find Action, after installing a new
  `vuka` or gopls.

## Requirements

```sh
go install golang.org/x/tools/gopls@latest
```

and vuka, either as a tool of the module (the server then is the version the
project builds with):

```sh
go get -tool github.com/vuka-lang/vuka/cmd/vuka
```

or installed:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

## Which vuka runs

The first of:

1. the binary set in **Settings | Languages & Frameworks | Vuka**,
2. `go tool vuka`, when the project's `go.mod` declares it as a tool,
3. `vuka` on `PATH`, then in `$GOBIN`, `$GOPATH/bin` and `~/go/bin`.

The same page sets the gopls binary, whether to share gopls's daemon
(`-remote=auto`, on by default), formatting on save and closing tags (on by
default). Applying restarts the server.

The messages between the IDE and `vuka lsp` (VS Code's `vuka.trace.server`)
go to the IDE log with `#com.intellij.platform.lsp` added in **Help |
Diagnostic Tools | Debug Log Settings**.

## Templates

`.vuka.html` files are highlighted with the Vuka grammar, as in VS Code: Go
in braces and at line starts, TypeScript in `<script lang="ts">`, CSS in
`<style>`, and `vuka lsp` serves the Go. Turn off **Highlight .vuka.html
files with the Vuka grammar** in the settings to open them as HTML instead.

## Go files

GoLand's own Go support serves `.go` files, not gopls, and it reads Go only
from disk, so it doesn't see the Go `vuka lsp` generates in memory: a `.go`
file using a declaration from a `.vuka` file shows it as unresolved.

Turn on **Write the Go of .vuka files beside them on save** in the
settings: saving a `.vuka` or `.vuka.html` file then runs
`vuka gen -inplace` on its package, and GoLand indexes the generated Go
(`pages/urls/urls_gen.go` among it), so `.go` files resolve it. The files
start with a `// Code generated` header; commit them, or ignore them in
`.gitignore`. `vuka gen -inplace -check` in CI reports stale ones.

## Not ported

- Linked editing of a tag's name and its closer's: the IDE's LSP client
  doesn't ask for linked editing ranges.
- Making templ's language server use Vuka as its gopls: the templ plugin
  for JetBrains IDEs has no setting naming the gopls it runs.

## Build

```sh
./gradlew buildPlugin    # build/distributions/vuka-goland-<version>.zip
./gradlew runIde         # a GoLand sandbox with the plugin
./gradlew test           # the tag-closing tests
```

Install the zip with **Settings | Plugins | ⚙ | Install Plugin from Disk**.
JDK 21 is needed; Gradle downloads GoLand to build against.

## Layout

```
editors/goland/
  build.gradle.kts         # IntelliJ Platform Gradle Plugin; copies ../vscode's grammars into the plugin
  gradle.properties        # plugin version, oldest build, GoLand version built against
  src/main/resources/META-INF/plugin.xml
  src/main/kotlin/io/github/vukalang/vuka/
    VukaLspServerSupportProvider.kt  # starts vuka lsp for .vuka and .vuka.html files
    VukaCommand.kt                   # which vuka runs, with which flags
    VukaSettings.kt                  # settings and their page
    VukaBundleProvider.kt            # the TextMate bundle
    VukaHtmlFileTypeOverrider.kt     # .vuka.html files on the Vuka grammar
    VukaJsx.kt                       # editors/vscode/jsx.js: which tag > or </ closes
    VukaEditing.kt                   # closing tags, Enter, the live template context
    VukaFormatOnSave.kt
    VukaGenOnSave.kt                 # vuka gen -inplace on save, when asked
    VukaGoHtmlInjector.kt            # HTML in ui.HTML strings (with the Go plugin)
    VukaIcons.kt
    RestartServerAction.kt
  buildSrc/                          # snippets/vuka.json → liveTemplates/Vuka.xml
  src/test/kotlin/…/VukaJsxTest.kt   # editors/vscode/test/jsx.test.mjs's cases
```

## License

MIT.
