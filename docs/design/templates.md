# Templates: `.vuka.html`

## Principle

`.vuka.html` is the markup form. A hand-written app is `.go` files and
`.vuka.html` files; a `.vuka` file's JSX lowers to exactly that (`vuka eject`
writes `x.go` and `x.vuka.html`). A `.vuka.html` file is compiled to Go — the
vuka render tree, templ's escaping — and delivered with `go build -overlay`
under `go tool vuka run|build|test|vet`, writing nothing to the tree;
`vuka gen` writes the generated Go (`x_html.go`) for a plain `go build`.

There is no runtime interpreter and no expression subset: every expression
and statement is Go, checked by the Go compiler, and its errors map back
through `//line` to `x.vuka.html:line:col`.

Inline `ui.HTML(`…{label}…`, args…)` in plain Go is the one form that needs
no tools: the same holes and tags inside the string, each label the Go
expression of its argument, checked against the arguments by vukavet.
`ui.Component[P](string)` is not part of the design; the unreleased
`ui.Template` and its numbered holes are removed.

## 1. Files

- A package is its `.go` files and any number of `.vuka.html` files, sharing
  one package scope, as Go files do.
- A `.vuka.html` file holds several templates. Each starts with a `func`
  header at the start of a line — `func Card(p CardProps)`,
  `func orderRow(o Order, cancel func(int) ui.Action)`,
  `func (p *Orders) Render()`, `func (p CardProps) header()` — and its body is
  the markup up to the next `func` line or the end of the file. The result,
  `ui.Node`, is implied. An optional first parameter `ctx context.Context` is
  passed by tags and the runtime.
- Exported and unexported names, and duplicate names, behave as in Go. A `//`
  comment line above a header is the template's doc comment.
- Imports: the package's `.go` files' imports are visible; a template file may
  add `import "x"` lines at its top.
- Comments in markup: `{/* … */}`.

## 2. Markup

- Statements are Go: `if`/`else if`/`else`, `for … range` and `for cond`,
  `switch`/`case`/`default`, `x := expr`. A statement starts a line and its
  body is markup inside `{ }`, as in templ. Text starting with a keyword is
  written `{"if …"}`.
- Holes `{expr}` are any Go expression. Attributes: `a={expr}`, or text with
  holes `href="/o/{o.ID}"`; `class:x={bool or ui.State[bool]}`; spread
  `{attrs...}`; `bind:value={p.Q}`; events `onClick={p.Cancel}` or
  `onClick={p.Cancel(o.ID)}` (a method value or call is a live event; a JS
  command value runs in the browser). <span v-pre>`{{`</span> is a literal brace.
- The render tree is the one vuka builds today: live diffs, keys, client
  state, islands, templ's escaping and sanitizing.

## 3. Nesting

- A capitalised tag is a component, a lowercase one HTML. In the same
  package `<Filters q={p.Q}/>`; from another as a Go selector, `<ui.Card>`,
  `<layouts.Admin>`, resolved as Go: an exported template or Go function
  taking one props struct, or a stateful type.
- Attributes on a component's tag (a struct or function component, an
  island, a layout) are camelCase, as in `.vuka` JSX: the field is the name
  with its first letter upper-cased (`noPad` is `NoPad`, `onSelect`
  `OnSelect`); on an island's component file they match its props and
  emits (`points` the prop `points`, `onSelect` the emit `select`). A bare
  attribute is `true`, `"text"` a string, `{expr}` a typed value. HTML
  elements keep HTML's attribute names (`class`, `for`, `data-*`, `aria-*`,
  `onClick={…}` for events). There is no kebab-case.
- Content between the tags is `Children ui.Node`; `<slot:header>…</slot:header>`
  is the `Header ui.Node` field. An unknown field, or one that isn't a Node,
  is an error at its position.
- Partials — templates whose parameters aren't one props struct — are
  called, not tagged: `{orderRow(o, p.Cancel)}`.
- Keys: `key={…}` on a loop item's first element or on a component tag.
  Recursion is allowed.
- Templates are values (render props): `<ui.DataTable rows={p.Orders} row={orderRow}/>`.
- Stateful components: `type Counter struct{ Start int; n ui.State[int] }` and
  `func (c *Counter) Render()`; `<Counter start={5}/>` is `&Counter{Start: 5}`,
  keyed `<Counter key={r.ID} …/>` is `ui.Key`. Nested live components keep
  their own instance and events, as now.
- Islands: `<islands.Chart series={p.Sales} client:visible onSelect={p.Zoom}/>`,
  the props struct registered with `ui.RegisterIsland[Chart]()`.
- Layouts are components with `Children` and slots
  (`func Admin(ctx context.Context, p AdminProps)`); route layouts
  (`ui.Island(ui.Layout(…))`) are unchanged.

## 4. JSX lowering

- A function returning markup becomes a template with the same signature in
  `<file>.vuka.html`; `Render()` methods likewise.
- JSX inside Go logic (`head := <div>{t}</div>`, arguments, returns mid-way
  through a function, a decorated function's body) becomes a named partial
  in the `.vuka.html` — its parameters the free variables with their types,
  its name from the variable assigned or the enclosing component
  (`cardHead`, else `cardView`; inside a hole's Go, the root's tag:
  `cardB`) — called from the `.go`. Islands move with their templates, their
  component imports with them.
- `{for x in xs}` is `for _, x := range xs`, `{if}` an `if`, `{match}` a
  `switch`; markup given to a Node prop is a `<slot:x>`; component tags are
  unchanged.
- The `.go` keeps only Go. The pipeline is `.vuka` → `.go` + `.vuka.html` →
  Go, through the overlay. `contract.txt` and the goldens show the
  `.vuka.html` and the final Go.

## 5. Scripts

- A component script, `<script lang="ts">` after a template's markup, belongs
  to that template (one per template):

  ```ts
  import { component } from 'vuka:component'
  export default component<ChartProps>((el, props, ctx) => ({ update(next) {…}, destroy() {…} }))
  ```

  It runs once per instance: `el` is the instance's root, `props` the props
  struct's data fields as JSON (`ui.Node` and unexported fields are never
  sent); `update` runs on a live re-render with changed props (the morph keeps
  `el`), `destroy` when the instance leaves (a patch, a navigation, a list
  removal). `ctx.live` is the live connection (its exported methods),
  `ctx.state(name)` reads and writes a `ui.State`. Its types are generated from
  Go, as `vuka-live.d.ts` is. It loads on its first appearance, once per page;
  instances may take `client:visible`/`idle`/`media`. It is a Vite module in
  development (`vuka run`, `web.Dev()`) and built by `go tool vuka
  build-islands` for production: a template with a script is a framework-free
  island with server-rendered markup, on the island machinery. A live page's
  `Render()` template may have one too (props: its exported fields; `ctx.live`:
  its methods).
- `<script is:inline>` is written as it is, not bundled; its one allowed hole
  is `{json(x)}` (script-safe JSON through templ's script escaping); any other
  hole inside a script is a compile error. A CSP nonce is added when one is
  configured.

## 6. Scoped styles

- `<style scoped>` after a template's markup (one per template, beside its
  script if it has one) is CSS scoped to that component's instances: its
  selectors are rewritten with a per-template hash attribute (`[data-v-xxxx]`)
  added to the template's own elements, not to child components' insides;
  `:global(…)` escapes, and `:root`-level rules are allowed only through
  `:global`. `lang="css"`; Tailwind's `@apply` works when Tailwind is in the
  pipeline. It is bundled into the page's or island's CSS (Vite or `web.Dev()`
  in development, `build-islands` for production), loaded once per page with
  the component. A style has no holes; dynamic values go through a `style={…}`
  attribute or CSS variables, `style:--accent={p.Color}`.
- A plain `<style>` is global, bundled once.

## 7. Tooling

- `vuka lsp` in `.vuka.html` maps to the generated Go through gopls (hover,
  definition, completion and rename across `.go` and `.vuka.html`: renaming
  `CardProps.Title` updates the `title=` attributes), HTML completion, the TS
  server for script blocks with the generated types, CSS for style blocks.
- vukavet: a live list's missing key, an unknown slot, a misplaced hole, a
  keyless stateful component in a loop, a hole inside a script or style, two
  scripts or styles in one template, a script reading a prop that isn't sent;
  and `ui.HTML`'s labels against its arguments.
- `vuka fmt` formats `.vuka.html`.
- Development: a saved template is overlaid again and rebuilds only its
  package; live pages reload keeping their state.
- Editor grammars (editors/vscode, editors/zed) for `.vuka.html`: HTML with Go
  statements and holes, TS and CSS blocks.

## Addendum

### A. Struct-first components

A component is, by default, a struct whose fields are its props, its markup
a `View` template (see Addendum 3: the struct is the Node):

```go
// sales.go
type Sales struct {
	Title    string
	Week     []int
	Points   []Point
	Zoom     func(ctx context.Context, from, to int) error
	Children ui.Node
}
```

```html
<!-- sales.vuka.html -->
func (s Sales) View()
<section class="sales">…{s.Title}…</section>
```

A value receiver is a stateless component; a pointer receiver (`*Counter`)
a stateful, live one. `<Sales title="x" week={w}/>` is
`Sales{Title: "x", Week: w}`, itself a `ui.Node` (a stateful type `&Counter{…}`, keyed `ui.Key`);
there is no separate `SalesProps`. Children and slots fill the struct's
`ui.Node` fields. Function components, `func Sales(p SalesProps)`, stay
supported — a tag works when the one parameter is a struct — and partials
are call-only. Docs, scaffolds, generated and ejected code and the ui kit
lead with structs. Lowering JSX: a `.vuka` `type Sales struct{…}` with
`func (s Sales) Render() Node { return <…> }` keeps the struct in the `.go`
and puts the `Render` template in the `.vuka.html`; a `.vuka` function
component stays a function template.

### B. `.vuka` and `.vuka.html` side by side

A package may mix `.go`, `.vuka` and `.vuka.html` files, all in one scope: a
struct declared in `sales.vuka` may have its `Render()` (and partials) in
`sales.vuka.html`. The transpiler compiles both into the package (through
the overlay); templates see `.vuka` declarations and the other way round.

### C. Component files as imports

`import Trend "./trend.vue"`, `import Spark "./spark.tsx"`,
`import Mini "./mini.ts"`, in templates and in `.vuka`, make island tags
`<Trend points={…} client:visible onSelect={…}/>`, their props type
generated from the component (internal/tsprops: defineProps, TS props,
Svelte props). A Go-first island (`type Trend struct{…}` +
`ui.RegisterIsland[Trend]()`) finds its component next to the Go file first
(`trend.vue|tsx|ts|jsx|svelte|js`), then at `islands/<module>/<Type>.<ext>`.

### D. Client code: three kinds, one model

The same tags, `client:*` strategies, typed props both ways,
`ctx = {live, state, navigate, patch}`, lifecycle (`update`, `destroy`), never
remounted by the morph, the same build commands.

1. Component and page scripts: `<script lang="ts">` after a template in a
   `.vuka.html`, or as a direct child of a component's root in `.vuka` JSX
   (lowering moves it after the template; `<script is:inline>` stays in
   place); the same for `<style scoped>`. No Node: bundled by the CLI's
   in-process esbuild.
   - Components: `export default component<Sales>((el, props, ctx) => ({ update(next) {…}, destroy() {…} }))`;
     `props` is the struct's data fields (`ui.Node`, func and unexported
     fields are never sent; func fields are events `ctx` can call).
   - Live pages: `export default page<Orders>((el, live) => ({ destroy() {…} }))`,
     `live` typed from the page struct: field handles (`live.q.get/set`),
     methods (`live.reorder(ids)`). It runs once on mount; live patches never
     run it again (the morph keeps the DOM, `data-nx-ignore` respected);
     `destroy` on navigating away (VukaLink, `live.navigate`); a patch of the
     same page keeps it.
2. Script islands: `.ts`/`.tsx`/`.jsx` exporting a component —
   `island<P>((el, props, ctx) => …)` for plain DOM, a default-exported React
   or Preact component in `.tsx`. No Node: esbuild compiles TSX.
3. Framework islands: `.vue`/`.svelte`, with Node and the project's Vite, as
   today. Without Node: a clear error with help ("needs Node; or use a .tsx
   island").

Page-backed islands (`web.Routes.Live(path, NewX, ui.Island(ui.Layout(…)))`,
`@web.Live @ui.Island`) find their component next to the Go file
(`<snake>.vue|tsx|ts`), any of the three kinds; `pageIsland<ApplicantDetail>((el, live) => …)`
for `.ts`. Bare npm imports (`import { Chart } from 'chart.js'`) resolve from
`node_modules` by esbuild's standard resolution and `NodePaths`.

### E. Files per package

Recommended, in docs and scaffolds: `orders.go` (types, constructors,
events), `orders.vuka.html` (`Render`, its script and scoped style),
`row.vuka.html` (partials), `chart.tsx`, `trend.vue`, `util.ts`. `vuka eject`
of `orders.vuka` writes exactly this.

### F. Tooling additions

`vuka lsp` hands script blocks and `.ts`/`.tsx` islands to the TS server
with generated types for props, live and ctx. vukavet: a script reading a
field that isn't sent, a `.vue`/`.svelte` with no Node, a page with two
backing components. viewtest runs scripts and script islands;
viewtest.Browser runs framework islands.

A and B land in PRs 1 and 2; C and D in PR 3.

## Addendum 2: file-based components

Components are file-based by default.

- `sales.vuka.html` is the markup of type `Sales` — its `View`, or its
  `Render` for a stateful type (Addendum 3) — (the file's base name in
  PascalCase), declared in `sales.vuka` or `sales.go` of the same package. The
  main template has no header: everything up to the first `func` line is the
  component's markup. A missing type is an error with help ("declare type
  Sales, or start the file with func …").
- Fields and methods by bare name: `{Title}`, `onSelect={Zoom}`,
  `{Total()}`. It is still Go: the compiler rewrites identifiers that resolve
  (go/types) to fields or methods of the receiver into `s.Title`; locals and
  loop variables shadow them, as in Go.
- The receiver's kind comes from the type: a pointer when it embeds
  `ui.Live`, holds a `ui.State` or has pointer-receiver methods; a value
  otherwise.
- More templates in the file start with `func` lines after the main markup
  (partials, called `{row(o)}`).
- An explicit header on the first line, `func (s Sales) View()` (or
  `func (p *Orders) Render()` for a stateful type), is still allowed (a named
  receiver, another signature); expressions then use the receiver's name.
  A name the package itself declares is the package's, not the field: in such
  a package (ui's own) write the header and the receiver.
- Imports (`import Trend "./trend.vue"`) go at the very top, before the
  markup.
- Live pages are the same pair (`orders.vuka` + `orders.vuka.html`); a page
  backed by its component is the pair with `orders.vue|tsx|ts` in place of
  the `.vuka.html`.
- Inline JSX (`Render() Node { return <…> }` in a `.vuka`) and function
  components remain, as the alternative.
- The pair is the default everywhere: `vuka add component X` and `vuka add
  page X` scaffold it, `vuka eject` turns inline JSX `Render` methods into
  it, and the docs, examples and the hand-written ui kit use it.

## Addendum 3: the struct is the Node

A struct component is itself a `ui.Node` (`templ.Component`): `ui.Card{Title:
t, Children: body}` goes in a hole, is returned and passed as it is, with no
call after it.

- Its markup is its `View() ui.Node` method: the file-based template of a
  type that is not stateful compiles to `View`, and the compiler adds the
  templ method rendering it, `func (s Sales) Render(ctx context.Context, w
  io.Writer) error { return s.View().Render(ctx, w) }`. An explicit header
  names it, `func (s Sales) View()`, and gets the same method. A hand-written
  struct component writes both.
- A stateful component (it embeds `ui.Live`) keeps `Render() ui.Node` as
  its markup, as `ui.Stateful` has it: `&Counter{…}` in a hole is a
  component tag, not a Node.
- The renderers see through a struct component: HTML rendering, the live
  render tree and blocks read its `View`, so it renders, diffs, keys (its
  View's root `key`) and nests stateful components exactly as its markup;
  its `Render(ctx, w)` is for everything else (templ, `http.Handler`s).
- A tag naming such a struct compiles to the literal, `<Sales title="x"/>`
  is `Sales{Title: "x"}` (`&T{…}` when only the pointer is a Node). A struct
  whose `Render() ui.Node` is its markup (the earlier form, and `.vuka`'s
  inline JSX `Render`) is still a tag, `T{…}.Render()`; `vuka eject` keeps
  such a method's header, so ejected code keeps its API.
- The ui kit is written this way; its former props types are aliases of the
  components, so `ui.Card(ui.CardProps{…})` is a conversion yielding the
  component, and `vuka fix ui-structs` writes it `ui.Card{…}`.

## Delivery

1. Syntax and compiler (`.vuka.html` → Go through the overlay, `vuka gen`),
   `ui.HTML` with labelled holes, `ui.Template` removed.
2. JSX lowered to `.vuka.html`; `vuka eject`.
3. Scripts and scoped styles.
4. vukavet, lsp, fmt, editor grammars, docs.
