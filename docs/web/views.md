# Views

A handler that returns data gets JSON; one with `@web.Template` gets a page
rendered from its result. Three kinds of view, picked by what the handler
names or returns:

| | |
|---|---|
| `@web.Template("views/pets.templ")` | a [templ](https://templ.guide) component, its parameters filled from the result |
| `@web.Template("views/pet.html")` | an `html/template` file, executed with the result |
| a `vuka.Node` result | a [JSX](/features/components) render function; no view needed |

```vuka
@web.Get("/pets")
@web.Template("views/pets.templ")
func ListPets(store *PetStore) Result[PetList] {
	pets := store.All()?
	return Ok(PetList{Title: "All pets", Pets: pets})
}

@web.Template("views/pet.html")         // before or after the route decorator
@web.Get("/pets/{id}")
func ShowPet(id int, store *PetStore) Result[model.Pet] { return store.Find(id) }

@web.Get("/")
func Home(r *http.Request) vuka.Node {
	return <views.Layout title="Home"><h1>Welcome</h1></views.Layout>
}
```

The file is a [`vuka.File`](/features/decorators#files): checked when the
program compiles, embedded in the binary, and parsed once when the app is
built — a missing file, a template that doesn't parse or a component whose
parameters the result can't fill is a Build error, never a 500 later.

## templ

`@web.Template("views/pets.templ")` uses the file's one exported component;
with several, name one: `@web.Template("cards.templ", "Card")`. The result
fills the component's parameters:

- whole, when the component has one parameter the result is assignable to;
- otherwise the result is a struct (or a pointer to one), and each parameter
  takes the field of its name — exactly, or with the first letter's case
  changed (`title` ↔ `Title`).

```templ
package views

templ PetsPage(title string, pets []model.Pet) {
	@Layout(title) {
		<h1>{ title }</h1>
		<ul>
			for _, p := range pets {
				<li>{ p.Name }</li>
			}
		</ul>
	}
}
```

```vuka
// PetList fills views.PetsPage's parameters by name: title, pets.
type PetList struct {
	Title string
	Pets  []model.Pet
}
```

A missing field is a Build error naming it:
`component PetsPage: main.PetList has no field for parameter title`. Layouts
are templ's own composition, `@Layout(title) { … }`.

### Subfolder packages

A `.templ` file beside the handler is part of the handler's package. One in a
subfolder is that folder's package, as Go has it — `views/pets.templ` starts
with `package views` — and Vuka compiles it and imports it for you:

```text
examples/pets/
  pages.vuka           @web.Template("views/pets.templ"), @web.Template("views/pet.html")
  model/pet.go         package model: Pet, shared by handlers and views
  views/layout.templ   package views: Layout
  views/pets.templ     package views: PetsPage, composed with @Layout
  views/pet.html
```

Only a subfolder's exported components can be views, and its types can't
come from the handler's package (`package main` can't be imported): put
shared types in a package of their own, like `model` here. A JSX page uses a
subfolder's component through its import: `<views.Layout title="Home">`.

## html/template

Any file that isn't `.templ` is an `html/template`, executed with the result
as its data:

```html
<!DOCTYPE html>
<html>
<head><title>{{.Name}} · Pets</title></head>
<body><h1>{{.Name}}</h1><p>A {{.Kind}}, pet #{{.ID}}.</p></body>
</html>
```

A second argument names a <span v-pre>`{{define}}`</span>d template to
execute: `@web.Template("pages.html", "pet")`. Layouts are templates defined
in the same file.

## JSX

A handler returning `vuka.Node` is a page: JSX, a templ component, or
`web.Redirect`. Components from `.templ` files and from `.vuka` files mix
freely — see [Components and JSX](/features/components):

```vuka
func Shell(title string, children vuka.Node) vuka.Node {
	return <html lang="en">
		<head><title>{title}</title></head>
		<body><main>{children}</main></body>
	</html>
}

@web.Get("/about")
func About() vuka.Node { return <Shell title="About"><p>Pets, since 2026.</p></Shell> }
```

## Which to use

- **JSX** for pages written in Vuka: components are functions, typed, with
  no view file and no binding step; a page can hold
  [live components](/web/live).
- **templ** when the markup lives in `.templ` files or uses a templ library;
  a templ view can render Vuka components too, and be live.
- **html/template** for plain pages; never live.

## Limits

- Views live in the handler's package directory or below it (`go:embed`
  doesn't reach up); `../shared/x.templ` is an error.
- A `vuka.File` literal is embedded only where a decorator or attribute takes
  one, so there is no `web.Layout(file)` option: compose layouts in templ or
  JSX, or <span v-pre>`{{define}}`</span> them in one html file.
