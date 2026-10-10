# Vuka for VS Code

Language support for [Vuka](https://github.com/vuka-lang/vuka): Go with Result,
Option, `?`, `match`, overloading, decorators, statics, typed field attributes
and JSX components — stateless, stateful and live.

- Completion, hover, signature help, go to definition, references, rename,
  outline, code actions and inlay hints, through gopls
- Diagnostics from Vuka (non-exhaustive matches, ambiguous overloads, misplaced
  `?`, unknown component attributes) and from Go's type checker, on the `.vuka` lines
- Formatting by `vuka fmt`, on save by default
- Highlighting for Vuka's additions on top of Go's grammar, inside function bodies too:
  - attributes and decorators: `@web.Get("/pets")`, `@orm.Meta{Table: "posts"}`, `decorator logged(c) { … }`
  - struct fields: names, types, tags and the attributes after them
    (`` Title string `json:"title"` @orm.Char{Max: 200} @orm.Index ``), embedded fields with attributes
    (`orm.Base @orm.Meta{…}`)
  - statics and static methods (`static Table = "users"`, `func User.New(…)`), `Self`/`self`
  - `match` and its patterns: `case Ok(Some(u)):`, guards (`case Err(e) if …:`), `^pin`, `_`; `Result`,
    `Option`, `Ok`/`Err`/`Some`/`None`; the `?` operator
- JSX highlighting: tags and components (`<div>`, `<Card />`, `<theme.Card>`, `<List[User]>`, stateful
  `<Counter Start={5} />`), fragments, attributes (`data-id={x}`, `hx-get="/x"`), event attributes
  (`onClick={c.Inc}`), `key`, text, `{/* comments */}`, JSX in package-level `var`s, and the Go inside
  `{ … }`: expressions and `{for …}`, `{if …} else {…}`, `{match …}` blocks, including the blocks nested
  without braces in a block's body
- Markup completion: every HTML element (and SVG's basics) after `<`, with a description and its MDN link;
  each element's own attributes, then the global ones, event handlers, `aria-*` and `data-`; the values of
  enumerated attributes (`type="…"`, `target="…"`, `rel="…"`); the open element after `</`; `for`, `if` and
  `match` blocks after a child's `{`; component props; Go inside `{ … }` while the tags around are unfinished
- Hover on HTML elements and attributes (description + MDN link)
- Linked editing: renaming an opening tag renames its closing tag (`editor.linkedEditing`, on by default for
  `.vuka`); folding of elements and blocks
- Closing tags added as you type an opening tag's `>` and the open element's name after `</`
  (`vuka.autoCloseTags`), inside blocks and fragments and after another tag on the line; Enter between tags
  indents; `{` and quotes pair in markup
- Snippets: `live` (stateful component), `component`, `match` (Result / Option), `{for`, `{match`,
  `route` (`@web.Get` + handler), `routet` (`@web.Template`), `livepage` (`@web.Live`), `webmain`,
  `model` (`orm.Base @orm.Meta{…}` + fields with attributes), `decorator`, `decoratorp`, `tx`
  (`@orm.Transaction`), `static`, `sfunc`
- The language status shows `vuka version`; clicking it restarts the server
- `.templ` files beside `.vuka` files are compiled by vuka itself (no `templ generate`); go to definition
  from Vuka code lands in the `.templ` file. Use templ's own extension for editing `.templ` files.

## Requirements

```
go install github.com/vuka-lang/vuka/cmd/vuka@latest
go install golang.org/x/tools/gopls@latest
```

The extension runs `vuka lsp`, which runs gopls and keeps the Go generated from
your `.vuka` files open in it, so nothing is written to your tree. `.vuka`
files are formatted by `vuka fmt` on save (`"[vuka]": {"editor.formatOnSave":
false}` turns that off).

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
| `vuka.autoCloseTags` | `true` | add a JSX closing tag when an opening tag's `>` is typed, and the open element's name after `</` |

`.vuka` files default to `editor.formatOnSave` and `editor.linkedEditing`. Emmet isn't enabled for them: it
can't tell markup from Go and would offer abbreviations in Go code. To use it anyway, set
`"emmet.includeLanguages": {"vuka": "javascriptreact"}`.

Run **Vuka: Restart Language Server** after reinstalling vuka.

## Development

`npm test` checks the grammar and the JSX editing logic (`jsx.js`, the rules of
`language-configuration.json`; `test/jsx.test.mjs`, in node, no VS Code). The grammar test tokenizes `test/fixtures/*.vuka` (and every
`vuka` sample of the docs site, which highlights with this grammar) the way VS
Code does, checks the scopes at key positions (`test/assertions.mjs`) and
compares every token with `test/fixtures/*.vuka.snap`; `npm test -- --update`
rewrites the snapshots. `npm run package` builds the `.vsix`.
