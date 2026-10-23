# Vuka for VS Code

Write web apps in Go — server-rendered pages, live pages that update over a
socket, and client islands — with UI shapes in `.vuka.html` and, if you want
them, [Vuka](https://github.com/vuka-lang/vuka)'s additions to Go in `.vuka` files.

- **UI shapes** (`.vuka.html`): a Go header with imports, one struct, decorated hooks and methods, followed by markup; embedded TypeScript and scoped CSS stay beside the markup.
- **Vuka** (`.vuka`): Go with `Result`, `Option`, `?`, `match`, overloading,
  decorators. Logic belongs here; markup belongs in `.vuka.html`.
- **Plain Go**: everything checks and navigates the same in `.go` logic files beside your shapes.

## Features

- Completion, hover, signature help, go to definition, references, rename,
  outline, code actions and inlay hints, through gopls
- Diagnostics from Vuka (non-exhaustive matches, ambiguous overloads, misplaced
  `?`, unknown component attributes) and from Go's type checker, on the `.vuka` lines
- Formatting by `vuka fmt`, on save by default
- Highlighting for Vuka's additions on top of Go's grammar, inside function bodies too:
  - attributes and decorators: `@ui.Mount`, `@ui.Loads("lists")`, `@orm.Meta{Table: "posts"}`, `decorator logged(c) { … }`
  - struct fields: names, types, tags and the attributes after them
    (`` Title string `json:"title"` @orm.Char{Max: 200} @orm.Index ``), embedded fields with attributes
    (`orm.Base @orm.Meta{…}`)
  - statics and static methods (`static Table = "users"`, `func User.New(…)`), `Self`/`self`
  - `match` and its patterns: `case Ok(Some(u)):`, guards (`case Err(e) if …:`), `^pin`, `_`; `Result`,
    `Option`, `Ok`/`Err`/`Some`/`None`; the `?` operator
- Shape highlighting: Go headers (including frontend component imports), decorator hooks,
  field attributes, HTML and component tags, Go expression holes and line statements,
  `class:`, `style:`, `bind:` and `client:` attributes, slots, scoped CSS and TypeScript
- Browser-state assignments in event attributes, including multiple statements and a
  server call: `onClick={open = false; Save()}`
- Markup completion: every HTML element (and SVG's basics) after `<`, with a description and its MDN link;
  each element's own attributes, then the global ones, event handlers, `aria-*` and `data-`; the values of
  enumerated attributes (`type="…"`, `target="…"`, `rel="…"`); the open element after `</`; `for`, `if` and
  `match` blocks; component props; Go inside `{ … }` while the tags around are unfinished
- Hover on HTML elements and attributes (description + MDN link)
- Linked editing: renaming an opening tag renames its closing tag (`editor.linkedEditing`, on by default for
  `.vuka.html`); folding of elements and blocks
- Closing tags added as you type an opening tag's `>` and the open element's name after `</`
  (`vuka.autoCloseTags`), inside blocks and fragments and after another tag on the line; Enter between tags
  indents; `{` and quotes pair in markup
- Vuka HTML snippets: `livepage` / `live`, `component`, `layout` (save as
  `_layout.vuka.html`), `islandpage`, `mount`, and `forrows`
- Vuka logic snippets: `api` / `endpoint` (GET/POST functions under `pages/api/`),
  `webmain` (`web.New(pages.Routes())` and `app.Service`), `match`, `model`,
  `decorator`, `decoratorp`, `tx`, `static`, and `sfunc`
- The language status in `.vuka` and `.vuka.html` shows `vuka version`; clicking it restarts the server. An older server shows a warning and an update command.
- `.templ` files beside `.vuka` files are compiled by vuka itself (no `templ generate`); go to definition
  from Vuka code lands in the `.templ` file. Use templ's own extension for editing `.templ` files.

## Requirements

```
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
go install golang.org/x/tools/gopls@latest
```

The extension runs `vuka lsp`, which runs gopls and keeps the Go generated from
your `.vuka` files open in it, so nothing is written to your tree. `.vuka`
files are formatted by `vuka fmt` on save (`"[vuka]": {"editor.formatOnSave":
false}` turns that off).

## Templates

This extension is for **Vuka 2.0**: it needs a language server of v2.0.0 or
newer. The extension uses your separately installed `vuka lsp`; updating the
extension alone does not update that binary. To update it, run:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
```

Then run **Vuka: Restart Language Server**. The language status shows the
actual binary path and version, including in `.vuka.html` files. Check
`vuka.path` if you intentionally use another installation.


A `.vuka.html` file starts with `package` and imports, then one struct and
its decorated hooks and methods. Imports may name `.vue`, `.tsx`, `.jsx`,
`.svelte`, `.ts`, or `.js` components. The first line starting with `<`
outside Go blocks, strings and comments begins markup, which continues to EOF.
A channel receive (`<-ch`) or a raw string containing `<div>` stays Go.

```html
package pages

import "github.com/vuka-lang/ui/v2"

type Index struct {
    open ui.State[bool]
}
<main>
  <button onClick={open = !open}>Toggle</button>
  <p hidden={!open}>Hello</p>
</main>
```

Folder pages use the same language for every `.vuka.html` filename, including
`[id].vuka.html`, `[path...].vuka.html`, and `_layout.vuka.html`. No special
association is needed for bracket folders. Save logic and API endpoints as
`.go` or `.vuka`; write rendering components as shape files.

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
see types and helpers from `.vuka` logic files; hover and go to definition
reach their source.
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
| `vuka.autoCloseTags` | `true` | add a markup closing tag in `.vuka.html` when an opening tag's `>` is typed, and the open element's name after `</` |

Both Vuka languages default to `editor.formatOnSave` and
`editor.linkedEditing`. Emmet is not enabled automatically because a shape
contains both Go and markup.

Run **Vuka: Restart Language Server** after reinstalling vuka.

## Development

Run `npm ci && npm test`. Tests cover logic grammar snapshots, legacy grammar
recovery, shape fixtures and expected scopes, Go/markup boundary detection,
tag editing, route filename associations, snippet expansions and toolchain
integration. `node test/grammar.test.mjs --update` rewrites logic snapshots;
`node test/html.test.mjs --update` rewrites shape snapshots. `npm run package`
builds a local `.vsix`; it does not publish the extension.
