# Templates

A `.vuka.html` file is markup kept beside the Go of a package. Its holes
and statements are Go, checked by the Go compiler; the `vuka` command
compiles each file to [`ui.HTML`](/ui/#markup-in-go) calls, and compile
errors point at the `.vuka.html` line.

## A component

A component is a struct whose fields are its props. `sales.vuka.html` is
the `Render` of the type `Sales`, declared in `sales.go` (or `sales.vuka`)
of the same package:

```go
// sales.go
type Sales struct {
	Title    string
	Week     []int
	Children ui.Node
}

func (s Sales) Total() int {
	n := 0
	for _, d := range s.Week {
		n += d
	}
	return n
}
```

```html
<!-- sales.vuka.html -->
<section class="sales">
  <h2>{Title}</h2>
  for _, d := range Week {
    <span class="day" class:big={d > 10}>{d}</span>
  }
  <p>Total: {Total()}</p>
  {Children}
</section>
```

The markup before the file's first `func` line is the component's
`Render`. Fields and methods are read by bare name — `{Title}`,
`{Total()}` — and locals and loop variables shadow them, as in Go. The
receiver is a pointer when the type embeds `ui.Live`, holds a `ui.State`
or has pointer-receiver methods, and a value otherwise. A file whose
package declares no such type is an error; start it with an explicit
header, `func (s *Sales) Render()`, to name the receiver.

## More templates in a file

A `func` line at the start of a line starts another template; its body is
the markup up to the next `func` line. The result, `ui.Node`, is implied:

```html
// Page lists the orders below a week of sales.
func Page(orders []Order)
<main>
  <Sales title="Week" week={[]int{3, 12}}>
    <em>this week</em>
  </Sales>
  <ul>
    for _, o := range orders {
      {row(o)}
    }
  </ul>
</main>

func row(o Order)
<li key={o.ID}><a href="/orders/{o.ID}">{o.Ref}</a></li>
```

A template taking one struct is a component, used as a tag; one taking
anything else is a partial, called in a hole: `{row(o)}`. A `//` line
above a header is its doc comment. A first parameter `ctx
context.Context` is passed by tags and the runtime. `import "x"` lines at
the top of the file add imports; the package's `.go` imports are visible
too.

## Holes

Holes are any Go expression, and take what [`ui.HTML`'s
holes](/ui/#markup-in-go) take:

| | |
|---|---|
| `<td>{o.Ref}</td>` | a child |
| `title={o.Ref}`, `title="{o.Ref}"` | an attribute's value |
| `href="/orders/{o.ID}"` | part of a value |
| `class:late={o.Late}` | a toggled class: a bool or a `ui.State[bool]` |
| `<input {attrs...}>` | an attribute set |
| `onClick={c.Inc}`, `onClick={c.Add(2)}` | an event: a method value, or a call with no result, which runs on the event |

`{{` is a literal brace and `{/* … */}` a comment.

## Statements

A Go statement at the start of a line, with markup for its body:

```html
if len(orders) == 0 {
  <p>No orders yet.</p>
} else {
  <ul>
    for _, o := range orders {
      {row(o)}
    }
  </ul>
}
n := len(orders)
switch n {
case 1:
  <p>one order</p>
default:
  <p>{n} orders</p>
}
```

`if`, `else if`, `else`, `for` (over a range or a condition), `switch`
with `case` and `default`, and `x := expr`. Text that starts with a
keyword is written as a string, `{"if you can"}`.

## Components and slots

A capitalised tag is a component: `<Sales …/>` in the same package,
`<ui.Card>` or `<layouts.Admin>` from another.

- A struct with a `Render` method: `<Sales title="Week" week={w}/>` is
  `Sales{Title: "Week", Week: w}.Render()`. A pointer type embedding
  `ui.Live` is a [stateful component](/ui/stateful), `&Counter{…}`; with
  `key={…}`, `ui.Key`.
- A function taking one struct: its attributes are the struct's fields.

Attributes on a component are camelCase — the field is the name with its
first letter upper-cased, so `noPad` is `NoPad` and `onSelect` is
`OnSelect`. A bare attribute is `true`, `"text"` a string, `{expr}` a
typed value. HTML elements keep HTML's names: `class`, `for`, `data-*`,
`aria-*`.

Content between the tags fills `Children ui.Node`; `<slot:actions>…</slot:actions>`
fills the `Actions ui.Node` field:

```go
// admin.go
type Admin struct {
	Title    string
	Actions  ui.Node
	Children ui.Node
}
```

```html
<!-- admin.vuka.html -->
<div class="admin">
  <header><h1>{Title}</h1>{Actions}</header>
  <main>{Children}</main>
</div>
```

```html
<!-- orders.vuka.html -->
func Orders(n int)
<Admin title="Orders">
  <slot:actions><button>New</button></slot:actions>
  <p>{n} orders</p>
</Admin>
```

A component file imported at the top of a template is an
[island](/web/islands) tag, its props read from the file:

```html
import Clock "./clock.tsx"

<main>
  <Clock zone={Zone} client:visible />
</main>
```

## Scripts and styles

A `<script lang="ts">` after a template's markup is its component's
script, and a `<style scoped>` its CSS — one of each per template:

```html
<figure class="chart">
  <figcaption>{Title}</figcaption>
</figure>

<script lang="ts">
import { component } from 'vuka:component'

export default component<Chart>((el, props, ctx) => ({
  update(next) { el.dataset.n = String(next.Points.length) },
  destroy() {},
}))
</script>

<style scoped>
figcaption { font-weight: bold }
</style>
```

The script runs once per instance: `el` is its root, `props` the struct's
data fields as JSON (`ui.Node`, func and unexported fields are not sent),
`update` runs when a live re-render changes them, `destroy` when the
instance leaves the page; `ctx.live` calls the component's exported
methods. The `vuka` command bundles the script and scopes the style to
the template's own elements (`:global(…)` escapes). A style has no holes.

`<script is:inline>` is written as it is, not bundled; its one hole is
`{json(x)}`, the value as script-safe JSON:

```html
<script is:inline>window.points = {json(Points)};</script>
```

## Building

`vuka run`, `vuka build` and `vuka test` compile `.vuka.html` files through
the overlay, writing nothing to the tree. `vuka gen` writes each one's Go
as `x_html.go`, so `go build` works without the `vuka` command. A package
may mix `.go`, `.vuka` and `.vuka.html` files in one scope.

`vuka eject -rm` turns a `.vuka` file's markup functions — its `Render`
methods and components — into templates of `x.vuka.html`, and keeps the
rest as `x.go`.
