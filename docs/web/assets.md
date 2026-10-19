# Styles and scripts

Some things a page needs don't fit in class names: a keyframe animation, a
`:hover` rule on a table row, a chart drawn into a canvas, a counter that
bumps when it changes. Give a page its own stylesheet and script, next to
the component that uses them:

::: vuka
```vuka
@web.Live("/counter",
	web.Layout(counterPage),
	web.Style("counter.css"),
	web.Script("counter.ts"),
)
type Counter struct {
	ui.Live
	Start int
	n     ui.Assign[int]
}
```
:::

::: go
<!-- go:context
func counterPage(page ui.Node) ui.Node { return page }

func (c *Counter) Inc() { c.n.Update(func(n int) int { return n + 1 }) }

func (c *Counter) Render() ui.Node { return ui.HTML(`<output>{c.n.Get()}</output>`, c.n.Get()) }
-->

```go
// counter.go
type Counter struct {
	ui.Live
	Start int
	n     ui.Assign[int]
}

func NewCounter() *Counter { return &Counter{} }

func init() { web.Routes.Live("/counter", NewCounter, web.Layout(counterPage)) }
```

```html
<!-- counter.vuka.html -->
<output>{n.Get()}</output>
<button onClick={Inc}>+</button>

<style scoped>
output {
	display: inline-block;
	@apply text-4xl font-semibold text-indigo-700;
}

output.bump { animation: bump 320ms ease-out; }

@keyframes bump {
	40% { transform: scale(1.4); }
}
</style>

<script lang="ts">
import { component } from 'vuka:component'

export default component<Counter>((el) => ({
  update() {
    const out = el.querySelector("output")!
    out.classList.remove("bump")
    void out.offsetWidth // start the animation again
    out.classList.add("bump")
  },
}))
</script>
```
:::

```css
/* counter.css */
output {
	display: inline-block;
	@apply text-4xl font-semibold text-indigo-700;
}

output.bump { animation: bump 320ms ease-out; }

@keyframes bump {
	40% { transform: scale(1.4); }
}
```

```ts
// counter.ts
export default {
	updated(el: HTMLElement) {
		const out = el.querySelector("output")!;
		out.classList.remove("bump");
		void out.offsetWidth; // start the animation again
		out.classList.add("bump");
	},
};
```

The stylesheet styles this component's `<output>` and no other: another
component's `output { color: red }` doesn't reach it, and its rules don't
reach theirs. The script runs when the counter is on the page, and its
`updated` hook each time a click changes it. There's nothing else to set
up: `vuka run` and `vuka build` compile both, and every page that shows a
`Counter` gets them.

::: go
In Go, a component's stylesheet and script go after the markup of its
[template](/ui/templates#scripts-and-styles): `<style scoped>` and
`<script lang="ts">`, bundled by the `vuka` command (`go tool vuka run`,
`go tool vuka build-islands` for production). The `web.Style`, `web.Script`,
`web.CSS` and `web.JS` options below are read from `.vuka` source by the
`vuka` command; given in Go code, they are not read.
:::

## Where they go

The four options are valid on `@web.Live(...)`, on
`web.Layout(fn, options…)` and `web.LiveLayout(fn, options…)`, and on
`web.New(options…)`:

| | |
|---|---|
| `web.Style(paths…)` | stylesheets, scoped to the component the option belongs to |
| `web.Script(paths…)` | hook modules (TypeScript or JavaScript) for that component |
| `` web.CSS(`…`) `` | a stylesheet written inline |
| `` web.JS(`…`) `` | a hook module written inline |

On `@web.Live` they belong to the live component; on `web.Layout` and
`web.LiveLayout`, to the layout's own markup (the `<h1>` a layout function
writes around the page, say); on `web.New`, to the whole app, unscoped.
`web.Global` as one more argument makes an option unscoped wherever it is:
`web.Style("print.css", web.Global)`.

Paths are relative to the `.vuka` file. A call takes several, calls may
repeat, and globs work, expanded in name order: `web.Style("counter.css",
"parts/*.css")`, `web.Script("hooks/**/*.ts")`. They must be string
literals, because the `vuka` command reads them from the source. A file
that doesn't exist, or a glob that matches nothing, is an error at that
line:

```
counter.vuka:10: web.Style("countr.css"): no such file; did you mean counter.css?
```

A layout gets its own styles and scripts when it is a named function of a
`.vuka` file, or a templ component (`web.Layout(views.Shell,
web.Style("views/shell.css"))`, for a `templ Shell(page templ.Component)`).

## Scoped stylesheets

Each scoped component has a short scope id, the same in every build. Every
element its own markup writes carries it — `<output data-vk-s="ab4d62">` —
and its selectors are rewritten to match only those elements:

| you write | it matches |
|---|---|
| `output`, `.row:hover`, `ul > li::before` | elements this component writes |
| `.card .title` | a `.title` this component writes, inside any `.card` |
| `:global(.dark) .title` | `.dark` anywhere, then this component's `.title` |
| `:global(body.wide)` | the page's `<body class="wide">`, unscoped |
| `.list :deep(li)` | any `<li>` inside this component's `.list`, a child component's too |
| `:scope` | the component's root element |
| `:root { --gap: 1rem }` | the document, unscoped |

The scope goes on the last part of a selector that isn't `:global` or
`:root`, before any `:deep`. Rules in `@media`, `@supports`, `@container`
and `@layer` are scoped; nested rules are too (`&:hover` is its parent's).
`@font-face`, `@property` and Tailwind's `@theme`, `@utility` and `@variant`
pass through as they are.

What carries the scope follows where the markup is written: elements a
child component renders have its scope (or none), and children you pass to
a component from your markup keep yours. The scope is fixed markup, written
when the page is compiled, so a live page's updates never carry it.

A `@keyframes` belongs to its component too: its name gets the scope, and
so do the `animation` and `animation-name` declarations of that
component's stylesheets naming it. Keyframes meant for several components
go in a global stylesheet.

The stylesheets are compiled with the app's [Tailwind CSS](/web/styling),
into the same `app.css`: `@apply`, `theme()` and variants work, and the
browser loads one cached file. They come after Tailwind's own layers, in
the order listed, so later files win and your rules win over utility
classes; write them in `@layer components { … }` to let utility classes
override them instead. A file listed twice for a component is compiled
once.

The `vuka` command checks each selector against the component's markup and
warns of a class, id or element it never writes — counting the classes its
scripts add — with the nearest one it does:

```
counter.css:3: .bmp matches no element in Counter; did you mean .bump?
```

A syntax error stops the build at its file and line
(`counter.css:12: unclosed {`).

## Scripts

A component's scripts are bundled, in the order listed, into one module
served under `/_vuka/c/`, which the page imports the first time the
component renders on it — on load, or when a [live
navigation](/web/navigation) brings it. TypeScript and JavaScript both
work, and relative imports are bundled with them (`import { spring } from
"./motion"`); packages from npm aren't resolved.

Each file may export a hook object:

```ts
export default {
	mounted(el, ctx) {},   // on page load or live join, or when a patch brings it
	updated(el, ctx) {},   // after a patch that keeps it
	destroyed(el, ctx) {}, // when it leaves the page, through a live navigation too
	bump(el, ctx, by) {},  // a method: ui.JS.Call("bump", 2) runs it
};
```

All of a component's hooks run in the listed order, once per instance on
the page. `el` is the instance's root element. `ctx` is the instance's own
object, the same in each hook, so state kept on it is per instance:

| | |
|---|---|
| `ctx.el` | the root element |
| `ctx.ref(name)` | the instance's element with `ref="name"` |
| `ctx.push("Inc", payload?)` | call the component's Go method `Inc`, as a live event (below): a value, or an object of form fields; a promise of the reply |
| `ctx.on(name, fn)` | `fn(payload)` for each `c.PushEvent(name, payload)` the component sends |

`ctx.push` takes one payload at most, as an event handler does. It reaches
only the methods the component's scripts name: the `vuka` command collects
every `ctx.push("Name")` in them, and those names are all the server lets
the browser call by name for that component. Everything else a component
declares stays out of reach, whoever opens a socket to the page — a method
you wrote for Go callers, or one a page only shows behind a check — and a
push of any other name is refused like an unknown handler. The handlers
the page renders (`onClick={c.Inc}`) work as they always have, and a
component without scripts takes no pushes by name at all.

So call it as `ctx.push` (the parameter named `ctx`), with the name as a
string literal, naming a method of the component's own
(not `Render`, `Mount`, an `Update`, or one it embeds); anything else stops
the build at its line:

```
counter.ts:8: ctx.push needs the method's name as a string literal (the vuka command allows the methods scripts name)
counter.ts:9: ctx.push("Incr"): Counter has no method Incr; did you mean Inc?
```

A pushed method still runs with the page's request, as an event handler
does: check what the caller may do there.

::: vuka
```vuka
func (c *Counter) Inc() {
	c.n.Update(func(n int) int { return n + 1 })
	c.PushEvent("bumped", c.n.Get())
}
```
:::

::: go
```go
func (c *Counter) Inc() {
	c.n.Update(func(n int) int { return n + 1 })
	c.PushEvent("bumped", c.n.Get())
}
```
:::

A [JS command](/ui/js) `ui.JS.Call("bump")` reaches the component's own
methods first, then the app's (global scripts), then `vuka.register`'s and
`window`'s; it is called as `bump(el, ctx, ...args)`, with `ctx.event` and
`ctx.target` the command's event and element. Two files of a component
exporting the same method is an error naming both.

On a live page the server's markup wins: a patch puts back the attributes
the server renders, so a class a hook adds lasts until that element's next
patch. Set it again in `updated`, as the counter above does.

A global script (`web.Script("analytics.ts", web.Global)`, or one on
`web.New`) is loaded on every page; its `mounted` runs once, `el` being
`document.body`.

## While you work, and in production

`vuka run` compiles the stylesheets and scripts beside the program and
keeps them current: save a stylesheet and the open page restyles without a
reload, as [with Tailwind](/web/styling#while-you-work); save a script and
the component's next mount, or a reload, takes the new version. The
[development toolbar](/web/toolbar)'s Assets panel lists the page's scoped
components, their files and their module.

`vuka build` minifies the modules and embeds them with the stylesheet: a
generated `vuka_assets_gen.go` in the main package registers them
(`web.RegisterAssets`, `web.RegisterScript`), and the modules are cached
by browsers for good. `vuka gen -inplace` writes that file too, for plain
`go build`.

Options given in Go code (`app.Live(path, NewCounter, web.Style(…))`) are
read by nobody: declare styles and scripts in `.vuka` files.
