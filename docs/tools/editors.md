# Editors

[[toc]]

## Introduction

`vuka lsp` is a language server for `.vuka` files: [gopls](https://go.dev/gopls)
behind a proxy that keeps the Go generated from each `.vuka` file open in gopls
as an unsaved buffer — never written to disk — and maps every position both
ways.

You get the editor features you know from Go in `.vuka` and `.vuka.html`
files, through extensions for VS Code and GoLand or any editor that speaks LSP.

## What the Language Server Does {#what-the-language-server-does}

Completion, hover, signature help, go to definition, references, rename,
outline, code actions, inlay hints and formatting (by
[`vuka fmt`](/tools/cli#fmt); the VS Code extension formats on save) work in
`.vuka` files.

Vuka's own errors
and Go's type errors appear on the `.vuka` lines; an import of a folder of your
module written without the module path gets a hint with the right one. Overloads and statics show as
you wrote them (`area`, `User.New`), and typing `@` lists the project's
decorators.

### Attributes and Decorators

After a struct field, `@orm.` lists what the package has for a
field attribute — its types (`Char{…}`, `Column("…")`, `PK`) and the
functions returning one (`DefaultTo(…)`), types declaring
[`vuka.OnField`](/features/attributes#targets) first and types whose targets
leave fields out omitted.

A [`vuka.File`](/features/decorators#files) path such as `@Page("views/pets.templ")` is a link:
go to definition opens the file (a `.templ` file with one component, at that
component), and hover shows the path and the `.templ` file's components.

Hover on an [`@ui.Island`](/web/islands#declare-the-props-in-go) shows the
component file it names or infers (relative to the module root) and its
framework; go to definition opens it; a missing or ambiguous file is an error
on the decorator as you type, the build's message.

Hover on a method
named `Mount`, `Changed` or `Load<Group>` that no hook attribute marks says
it isn't a hook (a hook is declared with `@ui.Mount`, `@ui.OnChange` or
`@ui.Loads`), and names the attribute.

### Shape Files {#templates}

`.vuka.html` files are served like `.vuka` files: hover, go to definition,
completion and rename on the Go in `<script lang="go">` (or a Go header), holes and statements go
through the Go they compile to (a bare `{Title}` is the receiver's field, so
go to definition lands on it in the shape's script), and their errors show at
their lines.

VS Code highlights them as HTML with Go in braces and at line
starts, TypeScript in `<script lang="ts">` and CSS in `<style>`, Zed reads the markup as HTML.

### Scripts {#scripts}

A shape's `<script lang="ts">` is TypeScript with its component's types
(`component<Chart>`: `props`, `ctx.live`, `ctx.state`, `ctx.ref`, `ctx.on`,
`ctx.emit`, `navigate` and `patch`, the names completed from the shape's
markup and Go): completion, hover (a live method's Go doc), signature help
and errors in the `.vuka.html`, at their lines. Go to definition on
`ctx.live.reorder` opens `func (p *Index) Reorder`, and that method's
references list the scripts calling it. In a script `listen` completes an
event listener removed on `destroy`, and an import's specifier completes
`vuka:component` and the module's npm packages.

`vuka lsp` gives each script to a
[typescript-language-server](https://github.com/typescript-language-server/typescript-language-server):

| | |
|---|---|
| `-tsls path` (`$VUKA_TSLS`) | the server: its executable, or its `lib/cli.mjs` run by Node; else one on `PATH`, else in the module's or its frontend's `node_modules/.bin` |
| `-tsls-node path` (`$VUKA_TSLS_NODE`) | the Node running a `.js`/`.mjs` server, `node` on `PATH` by default; an Electron binary runs as Node |
| `-tsserver path` (`$VUKA_TSSERVER`) | the TypeScript (`tsserver.js` or its `lib` directory) used when the module has no `typescript` of its own |

VS Code's extension passes all three: the server it bundles, VS Code's own
Node and the TypeScript inside VS Code, so scripts need nothing installed
(`vuka.typescript.server` names another server). In another editor, install
one (`npm i -g typescript typescript-language-server`, or as the module's
dev dependencies) or pass `-tsls`. See [Component Scripts](/ui/scripts).

### Editing Markup {#markup}

Shape markup is served from the `.vuka.html` text itself, so it works while a tag
is half-typed. It offers:

- completion: after `<` every HTML element (and the SVG most pages draw with)
  beside the components in scope, each with a one-line description and its
  MDN link; in a tag the element's own attributes first (`href` on `<a>`,
  `colspan` on `<td>`, `type`, `value` and `placeholder` on `<input>`), then
  the global ones (`class`, `id`, `style`, `key`…), the event handlers
  (`onClick`, `onInput`…), `aria-*` and `data-`; inside the quotes of an
  enumerated attribute its values (`type="checkbox"`, `target="_blank"`,
  `rel="noopener"`); after `</` the element still open; after the `{` of a
  child, `for`, `if` and `match` complete to whole blocks; a component's
  attributes are its parameters or props, and the Go in braces completes as
  Go, the surrounding tags unfinished or not
- hover on an element or an attribute: its description and MDN link
- linked editing: renaming an opening tag renames its closing tag as you type
  (`editor.linkedEditing`, on for `.vuka` files in VS Code); highlights pair
  the two
- folding of elements and blocks, beside gopls's folding of the Go
- one diagnostic at an unclosed tag while it is typed, not a flood
- each diagnostic with its code, linked to its page in the
  [catalogue](/errors/), and its help; what `vuka vet` reports of an island
  page (a deferred field without a loader, a bad `bind:` tag) shows as you
  type
- go to definition, references and rename on the Go inside braces

### Completing Attributes, Events and Refs {#attributes-events-refs}

In a tag, completion offers every ARIA attribute (`aria-pressed`,
`aria-current`, `aria-level`, `aria-relevant`…) and inside its quotes the
values it takes (`true` `false` `mixed` for `aria-pressed`; `page` `step`
`location` `date` `time` for `aria-current`), and the WAI-ARIA roles for
`role`.

Event handlers are the ones the element fires: `onClick`, `onInput`,
`onKeyDown` and the pointer, focus and drag events on any element, `onSubmit`
and `onReset` on a `<form>`, `onClose` and `onCancel` on a `<dialog>`,
`onToggle` on `<details>`, `<dialog>` and popovers, the playback events on
`<audio>` and `<video>`. Each item says the handlers Vuka takes: a method
(`{Save}`), a call (`{Add(2)}`), browser state (`{open = !open}`), a func or
a `ui.JS(…)` command. The event's name can be written in any case
(`onkeydown`).

`ref` names an element for [`ui.Ref`](/ui/js#targets-and-refs); inside the
quotes of `ui.Ref("…")`, completion lists the refs the same file declares.

### Keying a Loop's Items {#keying-loops}

A loop's items need a `key` ([W0102](/errors/W0102)). Typing `for` on a line
of markup offers a loop whose first element is keyed:

```html
for _, item := range items {
  <li key={item.ID}></li>
}
```

In a loop you wrote, the first element completes with the key: when the
item's type has an `ID` field (or `Id`, `Key`, `UUID`, `Slug`, `Code`, or a
field ending in `ID`), `<l` completes to `<li key={t.ID}`.

### Keeping the Struct in View {#outline}

The outline of a shape is its struct holding its fields, its methods and its
markup's elements (`section#todos.list`, its children inside). The struct spans
the file down to the end of its markup, so with sticky scroll
(`editor.stickyScroll.enabled`) the `type … struct` line stays pinned above the
markup you scroll through, with the elements around the cursor below it.

The `<script lang="go">` folds as one, from its start tag to `</script>` (in
the Go-header form, from the package clause to the line before the markup), beside the folds of its declarations and of the markup's elements and
blocks.

### Formatting a Selection {#format-selection}

Format Selection (`textDocument/rangeFormatting`) formats only the selected
lines of a `.vuka` or `.vuka.html` file by [`vuka fmt`](/tools/cli#fmt)'s
rules; the rest of the file stays as it is. A file that doesn't parse is left
alone.
### Telling Shape Names Apart {#shape-tokens}

In a `.vuka.html` file, semantic tokens say what each name in the markup reads,
so a theme colours them apart:

| Token type | A name that is |
|---|---|
| `vukaProp` | a prop: an exported field of the struct |
| `vukaState` | server state: an unexported field |
| `vukaBrowserState` | browser state: a `ui.State[T]` field |
| `vukaEvent` | an event: an exported method an `on…` attribute sends |
| `vukaComponent` | a component tag: `<components.Card>` |
| `vukaLocal` | a local a markup statement declares: `for _, r := range rows` |
| `vukaContextVar` | a [context processor](/web/context-processors)'s variable read by name: `user`, `request` |

The field declarations in the script carry the same types with the
`declaration` modifier. The rest of the Go — calls, types, packages — keeps
gopls's tokens. The VS Code extension maps each type to a TextMate scope
(`variable.other.constant` for props, `variable.other.property` for server
state, `variable.language` for browser state, `entity.name.function` for
events, `entity.name.type.class` for components), so themes without rules
for them still colour them apart. To pick colours yourself:

```json
"editor.semanticTokenColorCustomizations": {
  "rules": { "vukaBrowserState": "#c586c0", "vukaEvent": { "bold": true } }
}
```

Other editors read the types from the server's legend, after gopls's.

### Seeing What a Name Reads {#holes-events}

Hover on a name in a hole or a statement says what it is, its type, and
where it is set:

- `{stats.Posts}` on `stats`: **server state** `stats Stats` · set in
  `mount` (@ui.Mount), and on a live shape that the server keeps it
  between events
- a prop: set by the tag's attribute (`title` for `Title`), or on a page
  from the matching `[title]` path segment, else the `title` query parameter
- browser state: kept in the browser, and the `on…` attributes setting it
- a local: the statement declaring it, `for _, r := range rows`

Hover on an `on…` attribute says where its handler runs:
`onClick={Add}` is an **event** · runs on the server · patches the page;
`onClick={open = !open}` sets browser state in the browser, with no round
trip; `onClick={ui.JS.Toggle("#menu")}` runs browser commands;
`onClick={open = false; Save()}` sets browser state, then sends the event.

These come from the file's text, so they keep up while you type, before the
next build.

### Writing Component Tags {#component-tags}

Inside a component's tag, completion lists the attributes it takes, each
with its type: a struct's props by their attribute names (`ownerID` for
`OwnerID`), a function's parameters. Required ones are marked and sort
first. The rule is the compiler's:

- a struct's props may be left out: an attribute left out is the field's
  zero value
- a function's parameters are all required, except a first
  `context.Context`, `children`, a `ui.Node` (a slot, written
  `<slot:name>`) and a variadic last one

Hover on the tag name shows what the component is (component, live
component, function), a link to its file at its declaration, its attributes
with their types, and its slots.

Typing `<slot:` inside a component's tag lists its slots with their types:
a `ui.Node` field, a func field returning one (inserted with a `let` naming
its values) and a slice of structs (in the singular, `<slot:column>` for
`Columns`). Fields tagged `vuka:"required"` are marked. The names a `let`
declares are template locals in the slot's content; hover on one shows the
value it names and its type.

An attribute the component doesn't take, and a function's parameter left
out ([E0144](/errors/E0144)), are the compiler's errors, underlined as you
type.

### Following Routes {#routes}

For the pages under `pages/`, the editor reads the route table the module's
last build made, the one [`vuka routes`](/web/folder-routes#vuka-routes) prints:

- **A code lens** above each page's struct and each handler or endpoint
  method, in `.vuka.html`, `.vuka` and `.go` files alike: `GET
  /orgs/{org} · live · layout org.Layout · guard org.Guard`. In VS Code,
  clicking it opens the **Vuka Routes** output with the route's entry —
  middleware, guards, layouts, file — and the module's whole table;
  **Vuka: Show Routes** opens the table from the command palette.
- **A hover** on a page's or an endpoint's struct name says the routes it
  serves, with the guards, layouts and middleware around them; on a
  handler's or an endpoint's method (`Post`, `Get`, `Delete`…), the route
  it serves; on a live page's event method, the page it is an event of. In
  `.go` and `.vuka` files the route goes below gopls's own hover:

  ```text
  route POST /auth/logout · endpoint logout.Index.Post
  layout pages.Layout · middleware pages.Middleware
  ```

  An endpoint's struct gets a lens of its methods' routes too, `GET, POST
  /api/orgs · endpoint`. The paths are the ones `vuka routes` prints:
  `pages/users/[id]` is `/users/{id}`, and a route group such as
  `(admin)` is left out. A `.go` file of no folder-route package keeps
  gopls's hover and lenses as they are.
- **Links**: a `urls.Orgs.Org(7).Edit.Path()` chain and an `href="/orgs/new"`
  literal naming a page are links to the page's file, and go to definition
  on them opens the page at its struct — in `.go` files too, for chains.
- **References** on a page's struct name (or on the file's package clause)
  list the chains and literal `href`s naming the page, besides gopls's
  references to the type.

> [!NOTE]
> A literal `href` matches a route by its pattern: `/orgs/7?tab=x` is
> `/orgs/{org}`'s page, and `/orgs/new` wins over `/orgs/{org}`.

### Renaming Pages {#renaming-pages}

Renaming or moving a file or folder under `pages/` in the editor's explorer
renames the `urls` chains naming its pages, before the rename: renaming
`pages/orgs/new.vuka.html` to `create.vuka.html` makes `urls.Orgs.New` into
`urls.Orgs.Create` across the module, and renaming the folder `pages/orgs`
to `pages/teams` makes every `urls.Orgs.…` into `urls.Teams.…`, keeping
their arguments. The generated `pages/urls` package follows at the next
build.

> [!WARNING]
> Literal `href`s and paths in strings aren't rewritten: a rename changes
> the page's URL. Find them first with references on the page's struct.

A page named by its type — a struct in a `.go` or `.vuka` file — keeps its
name when its file is renamed; only moving it to another folder changes its
chain.

### Refactoring {#refactoring}

A shape grows. Its markup holds a row worth naming, its script more methods
than the struct can show at a glance. `vuka lsp` adds code actions for both
beside gopls's, in either form of the file; the files they write are in the
`<script lang="go">` form, formatted as [`vuka fmt`](/tools/cli#fmt-shapes)
formats them, and they compile as they are.

#### Extracting a Component {#extract-component}

Select one or more whole elements of a shape's markup and run **Extract
component…** (`refactor.extract`). VS Code asks the new component's name;
another editor runs the server's command `vuka.extractComponent` with the
selection, and the server names it `Part` (`Part2`, …). The selection
becomes a shape file of its own, and a tag of it takes its place:

```html
<ul>
  for _, it := range items {
    <li key={it.ID}>
      <span>{it.Name}</span>
      <button onClick={func() { Remove(it.ID) }}>Remove</button>
    </li>
  }
</ul>
```

Extracting the `<li>` as `Row` writes `_row.vuka.html`:

```html
<script lang="go">
package pages

type Row struct {
	Item   Item
	Remove func(id int)
}
</script>

<li key={Item.ID}>
  <span>{Item.Name}</span>
  <button onClick={func() { Remove(Item.ID) }}>Remove</button>
</li>
```

and the loop renders `<Row Item={it} Remove={Remove} />`. What the
selection reads from around it becomes the new struct's props, typed by
the package's type check:

- a field of the shape — bare, `count`, or through the receiver, `p.count`:
  a prop of the field's type, as the script writes it; `Children` is
  passed as the tag's content
- a variable of the markup around it, a loop's item or index: a prop of its
  type, named after the type when the variable's name is short (`it` of
  type `Item` is `Item`)
- a method of the shape, called (`{Total()}`) or named as a handler
  (`onClick={Add}`): a prop of the method's func type, passed the method
  value, `Add={Add}`. A method value keeps its receiver, so the call and the
  event still run on the shape and change its state, and the event stays
  the shape's: a live shape gets it and renders again. A shape that was live
  only because the selection sent its events embeds `ui.Live`, so it stays
  live (the compiler made it so before)

A `key` stays on the element it keys, now the new component's first
element: a struct component's tag takes none.

Where the file goes:

- under `pages/`, in the private folder `_parts` beside the page (created
  when it isn't there), a package the page imports as
  `…/pages/…/private_parts` and names by `parts.Row`
- under `pages/`, when the selection needs the page's own package — a type
  or function declared there, `Item` above — in the page's folder as
  `_row.vuka.html`, which routes nothing
- elsewhere, beside the shape, in its package

The extraction is refused, with the reason, when the selection writes a
field (`count++`, `tab = "x"`: the new component would change its own copy
— move the write into a method, which the selection then passes), takes a
field's address, or reads a variable only its place declares.

#### Moving Methods to the Logic File {#move-methods}

On a method in a shape's script, **Move Add to board.vuka**
(`refactor.move`) moves it to the logic file beside the shape,
`board.vuka`, creating it with its package clause; **Move the methods of
Board to board.vuka** moves them all. The logic file gets the imports the
methods use, and the shape drops the ones nothing in it uses any more. A
`.vuka` file lowers tracked state as the script does, so the methods read
and write the fields as they did.

Moving all leaves in the script what the shape's kind depends on:

- `Title`, which names a page
- decorated methods: hooks (`@ui.Mount`, `@ui.Loads("…")`) and handlers
  with attributes
- on a shape live only because its markup sends events, the methods it
  sends, which make it live

#### Quick Fixes {#quick-fixes}

Diagnostics with one obvious fix have it as a quick fix:

| Diagnostic | Fix |
|---|---|
| [W0102](/errors/W0102), a live list's item without a key | `key={it.ID}` on the item's first element: the item's `ID`, `Id`, `Key`, `UUID`, `Slug`, `Code` or a field ending in `ID`; else the loop's index, named `i` when the loop writes `_` |
| [W0703](/errors/W0703), a layout reading the page's title in `Load` | the field's reads in the markup become `{ui.PageTitle(ctx)}` (or `ui.PageCrumbs`); the field goes, and `Load` with it when it did nothing else |
| [W0703](/errors/W0703), a layout marking its current link by the request's path | `if strings.HasPrefix(path, "/orders") { <a … class="tab on"> } else { <a … class="tab"> }` becomes `<ui.Link href="/orders" active="prefix" class="tab" activeClass="on">` (`active="exact"` for `path == "/orders"`), and a lone `<a href="/about">` a `ui.Link` with `active="prefix"`; a field nothing reads any more goes, with the statement setting it |
| [E0155](/errors/E0155), tracked state in a `.go` file | the method moves to the shape's logic file, as above; a `.go` file left empty goes |
| [E0142](/errors/E0142), a template file without a package clause | its folder's package clause |
| [E0329](/errors/E0329), `*ui.Live` | `ui.Live`, embedded by value |
| [W0701](/errors/W0701), a `pages/` tree no app serves, on its `route.go` | the tree's `Routes()` passed to the `web.New` of the main that owns it, importing the package, as [`vuka fix routes`](/tools/cli#add-page-routes) does; when no one main can be edited, the action is disabled and says what to add where |
| [W0107](/errors/W0107), the Go-header form | **Write the Go in &lt;script lang="go"&gt;** |

`vukavet`'s warnings (`W…`) don't need to be on screen: the fixes are
offered where the file has what they report.

## Installing gopls {#installing-gopls}

The language server needs gopls. Install it:

```sh
go install golang.org/x/tools/gopls@latest
```

## VS Code {#vs-code}

The extension lives in
[`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode):
the language server, highlighting for Vuka's additions on top of Go's grammar
(field attributes, statics, `match` patterns, shape markup with its blocks, event
attributes and components — the grammar these pages use), snippets for
components, live pages, routes, models and decorators, closing markup tags as you
type (`>` adds the closing tag, `</` the name of the element still open, inside
blocks and fragments too), Enter between tags indenting, `{` and quotes pairing
in markup, and the Vuka icons.

### Tailwind CSS {#tailwind}

With [Tailwind CSS IntelliSense](https://marketplace.visualstudio.com/items?itemName=bradlc.vscode-tailwindcss)
installed, class completion, hover, color swatches and linting work in
`.vuka.html` and `.vuka` files. The Vuka extension sets its defaults:
`tailwindCSS.includeLanguages` reads both as HTML, and
`tailwindCSS.experimental.classRegex` finds classes in:

- `class="…"`
- the strings of `class={…}`: `class={"mt-2 " + extra + " text-sm"}`
- the strings of a Go function whose name ends in `Class` or `Classes`, where
  it is called (`tabClass(on, "px-5")`) and in its body:

```go
func tabClass(on bool, extra string) string {
    if on {
        return "bg-blue-600 text-white " + extra
    }
    return "text-gray-700"
}
```

> [!NOTE]
> A `tailwindCSS.includeLanguages` or `tailwindCSS.experimental.classRegex`
> of your own replaces the extension's. Copy the Vuka entries into it from
> the extension's `package.json` (`contributes.configurationDefaults`).

### Emmet

Emmet abbreviations expand in `.vuka.html` files: the extension sets
`"emmet.includeLanguages": {"vuka-html": "html"}`. `.vuka` files are left
out, where markup sits inside Go functions.

> [!WARNING]
> Emmet reads the whole file as HTML; it can't tell the Go in
> `<script lang="go">` from the markup. In the script it can suggest an abbreviation for a word that is also
> a tag name (`map`, `select`) or has a dot (`p.Q`). The script is still Go to
> the editor — its comments, brackets and snippets are Go's. To turn Emmet off
> for shapes, set `"emmet.excludeLanguages": ["vuka-html"]`.

The language status shows `vuka version`; clicking
it restarts the server.

### Serving Go Files {#go-files}

The extension offers, once, to serve your Go files through Vuka as well. Run
under the name `gopls`, vuka is a drop-in gopls, so `.go` files in a package
with `.vuka` files see the code those define — instead of "undefined" errors.

The extension links `gopls` to `vuka` in its storage folder and sets the Go
extension's `go.alternateTools.gopls` to it; **Vuka: Serve Go Files Too** and
**Vuka: Stop Serving Go Files** switch it on and off.

### Serving templ Files {#templ-files}

templ's own extension serves `.templ` files with `templ lsp`, which runs gopls.
When a workspace has both `.vuka` and `.templ` files, the Vuka extension offers,
once, to make that gopls Vuka: it writes a small `templ` wrapper next to the
`gopls` link that puts the link first on `PATH`, and sets `templ.executablePath`
to it. A `.templ` file then sees components defined in `.vuka` files —
`@Nav(current)` type-checks, and hover and go to definition reach the `.vuka`
file — while templ stays the only server for `.templ` files.

**Vuka: Let templ
Use Vuka as Its gopls** and **Vuka: Stop templ Using Vuka** switch it on and
off; a `templ.executablePath` you set yourself is left alone.

> [!NOTE]
> Both servers use gopls's shared daemon (`-remote=auto`), so the type-checking is
> done once for the whole editor.

## GoLand {#goland}

The plugin lives in
[`editors/goland`](https://github.com/vuka-lang/vuka/tree/main/editors/goland),
for GoLand and the other JetBrains IDEs with LSP support (2025.1 and later):
the language server for `.vuka` and `.vuka.html` files, highlighting of
`.vuka` files by the VS Code extension's grammar, and its snippets. It runs
`go tool vuka lsp` when `go.mod` declares vuka as a tool, else `vuka lsp`;
**Settings | Languages & Frameworks | Vuka** sets the binaries.

`.vuka.html` files open as HTML. GoLand serves `.go` files itself, not through gopls, so
`.go` files don't see code declared in `.vuka` files.

## Other Editors {#other-editors}

Run `vuka lsp` over stdio for the `vuka` file type. For Go files, point the
editor's gopls at a link named `gopls` to `vuka`; for `.templ` files, run
`templ lsp` with that link first on `PATH`.
