# templ interop

`ui.Node` is `templ.Component`, so templ components and Vuka components call
each other with no adapter.

## .templ files beside .vuka files

In a module that requires `github.com/a-h/templ`, `.templ` files sit in the
same packages as `.vuka` and `.go` files, and the vuka command compiles them
with templ's own parser and generator, in memory: no `templ generate` step,
nothing extra to commit. Their errors and go to definition point into the
`.templ` file, and `vuka lsp` serves them (as templ's language server's
gopls, too).

```templ
// layout.templ
package main

templ Shell(title string) {
	<main><header>{ title }</header>{ children... }</main>
}

templ Footer(pets []Pet) {
	@PetRow(pets[0])        // a Vuka component, called from templ
}
```

```vuka
// pets.vuka
func Page(pets []Pet) ui.Node {
	return <Shell title="Pets">        // a templ component, called from JSX
		<table>{for _, p := range pets { <PetRow pet={p} /> }}</table>
		<Footer pets={pets} />
	</Shell>
}
```

This is a plugin of the vuka command, not of the language: the command links
templ's parser and generator and turns them on for a module whose go.mod
requires templ (`go get github.com/a-h/templ` first, in a module that
doesn't yet). The language and its runtime know nothing of templ.

## Children

A templ component has no `children` parameter: JSX children reach its
`{ children... }` through `ui.WithChildren` (templ's context). So do the
children of a component from another package, which may be a templ
library: templUI works as it is. `templx.WithChildren` is the same function,
for Go code.

## On live pages

A templ layout or view on a live page changes as little as JSX does: an
event that changes one value inside a templ component sends that value, and
moving between pages that share a templ layout sends the title, the nav
link that changed and the new content — not the document again. On a page
with a templ layout and a templ panel, a click changing the panel's badge
sends 93 bytes where the panel's whole markup took 507; moving to another
page of the layout takes 708 bytes where the document took 1,736 (most of
it the new page's markup, sent once), and going back to a page seen before,
289.

In a module requiring ui (v0.5.0 or later), the vuka command compiles each
`.templ` file to record a [render tree](/ui/live#what-a-reply-holds) under a
live render, the way JSX's frames do:

| templ | in the tree |
|---|---|
| the markup the source fixes | statics, sent once per connection |
| `{ expr }`, an attribute's value, `{ attrs... }`, classes | a dynamic |
| `for` | a block: a frame per iteration |
| `if` / `else`, `switch` | a frame for the branch taken |
| `@component`, `{ children... }` | a frame of its own: a templ component's, a JSX node's, or a stateful component's instance |

A templ component's frames are identified by their statics, so the same
layout keeps its frames from one page to the next, and the page's change is
a change inside them. Templ inside JSX, and JSX inside templ, nest as
frames both ways.

Nothing else changes. On an HTTP page, a v1 session, or when a component
renders into a string of its own, the compiled code writes exactly what
templ's would, byte for byte, at about the same cost. A templ component
compiled elsewhere — a component library, `templ generate` output — renders
as before: its markup is one dynamic.

## A .templ file's components

A string literal passed where a decorator or attribute takes a `vuka.File`
(`@Page("views/pets.templ")`) is checked at compile time and embedded. In a
module requiring templ and ui, the file's components are also registered,
with their parameters' names, and `templx.Components(f)` returns them — for
a `.templ` file of the package, or of a subdirectory that is a package of
its own (exported components only):

```go
comps, _ := templx.Components(view)
for _, c := range comps {
	fmt.Println(c.Name, c.Params) // c.Func is func(params…) templ.Component
}
```

A framework uses it to render a view by name; the web framework's views do.
