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
