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
`ui.Template` and its `{{n}}` holes are removed.

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
  command value runs in the browser). `{{` is a literal brace.
- The render tree is the one vuka builds today: live diffs, keys, client
  state, islands, templ's escaping and sanitizing.

## 3. Nesting

- A capitalised tag is a component, a lowercase one HTML. In the same
  package `<Filters q={p.Q}/>`; from another as a Go selector, `<ui.Card>`,
  `<layouts.Admin>`, resolved as Go: an exported template or Go function
  taking one props struct, or a stateful type.
- Attributes are props fields: kebab-case names the field (`on-change` is
  `OnChange`); a bare attribute is `true`, `"text"` a string, `{expr}` a typed
  value.
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
- Islands: `<islands.Chart series={p.Sales} client:visible on-select={p.Zoom}/>`,
  the props struct registered with `ui.RegisterIsland[Chart]()`.
- Layouts are components with `Children` and slots
  (`func Admin(ctx context.Context, p AdminProps)`); route layouts
  (`ui.Island(ui.Layout(…))`) are unchanged.

## 4. JSX lowering

- A function returning markup becomes a template with the same signature in
  `<file>.vuka.html`; `Render()` methods likewise.
- JSX inside Go logic (`head := <div>{t}</div>`, arguments, returns mid-way
  through a function) becomes a named partial in the `.vuka.html` — its
  parameters the free variables with their types, its name from the variable
  assigned or the enclosing component (`cardHead`) — called from the `.go`.
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

## Delivery

1. Syntax and compiler (`.vuka.html` → Go through the overlay, `vuka gen`),
   `ui.HTML` with labelled holes, `ui.Template` removed.
2. JSX lowered to `.vuka.html`; `vuka eject`.
3. Scripts and scoped styles.
4. vukavet, lsp, fmt, editor grammars, docs.
