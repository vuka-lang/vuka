# Views

<!-- go:context
type Pet struct {
	ID   int
	Name string
}

type PetStore struct{}

func (s *PetStore) All() ([]Pet, error)      { return nil, nil }
func (s *PetStore) Find(id int) (Pet, error) { return Pet{ID: id}, nil }

func Layout(title string, children ui.Node) ui.Node {
	return ui.HTML(`<html><head><title>{title}</title></head><body>{children}</body></html>`, title, children)
}
-->

A handler that returns data gets JSON; one with <span class="vuka-only">`@web.Template`</span><span class="go-only">the `web.Template` option</span> gets a page
rendered from its result. Three kinds of view, picked by what the handler
names or returns:

| | |
|---|---|
| <span class="vuka-only">`@web.Template("views/pets.templ")`</span><span class="go-only">`web.Template(vuka.File("views/pets.templ"))`</span> | a [templ](https://templ.guide) component, its parameters filled from the result |
| <span class="vuka-only">`@web.Template("views/pet.html")`</span><span class="go-only">`web.Template(vuka.File("views/pet.html"))`</span> | an `html/template` file, executed with the result |
| a `ui.Node` result | a <span class="vuka-only">[JSX](/features/jsx) render function</span><span class="go-only">component or `ui.HTML`</span>; no view needed |

::: vuka
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
func Home(r *http.Request) ui.Node {
	return <views.Layout title="Home"><h1>Welcome</h1></views.Layout>
}
```
:::

::: go
```go
type PetList struct {
	Title string
	Pets  []Pet
}

func ListPets(store *PetStore) (PetList, error) {
	pets, err := store.All()
	if err != nil {
		return PetList{}, err
	}
	return PetList{Title: "All pets", Pets: pets}, nil
}

func ShowPet(id int, store *PetStore) (Pet, error) { return store.Find(id) }

func Home(r *http.Request) ui.Node { return Layout("Home", ui.HTML(`<h1>Welcome</h1>`)) }

func init() {
	web.Routes.Get("/pets", ListPets, web.Template(vuka.File("views/pets.templ")))
	web.Routes.Get("/pets/{id}", ShowPet, web.Template(vuka.File("views/pet.html")))
	web.Routes.Get("/", Home)
}
```
:::

The file is a [`vuka.File`](/features/decorators#files): <span class="vuka-only">checked when the
program compiles, embedded in the binary, and</span><span class="go-only">from Go, read from disk relative to the working directory, and</span> parsed once when the app is
built — a missing file, a template that doesn't parse or a component whose
parameters the result can't fill is a Build error, never a 500 later.

## templ

<span class="vuka-only">`@web.Template("views/pets.templ")`</span><span class="go-only">`web.Template(vuka.File("views/pets.templ"))`</span> uses the file's one exported component;
with several, name one: <span class="vuka-only">`@web.Template("cards.templ", "Card")`</span><span class="go-only">`web.Template(vuka.File("cards.templ"), "Card")`</span>. The result
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

::: vuka
```vuka
// PetList fills views.PetsPage's parameters by name: title, pets.
type PetList struct {
	Title string
	Pets  []model.Pet
}
```
:::

::: go
```go
// PetList fills views.PetsPage's parameters by name: title, pets.
type PetList struct {
	Title string
	Pets  []Pet
}
```
:::

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
execute: <span class="vuka-only">`@web.Template("pages.html", "pet")`</span><span class="go-only">`web.Template(vuka.File("pages.html"), "pet")`</span>. Layouts are templates defined
in the same file.

## Components

A handler returning `ui.Node` is a page: <span class="vuka-only">JSX</span><span class="go-only">a component, `ui.HTML`</span>, a templ component, or
`web.Redirect`. Components from `.templ` files and <span class="vuka-only">from `.vuka` files</span><span class="go-only">Go's</span> mix
freely — see <span class="vuka-only">[Components and JSX](/features/jsx)</span><span class="go-only">[Components](/ui/components)</span>:

::: vuka
```vuka
func Shell(title string, children ui.Node) ui.Node {
	return <html lang="en">
		<head><title>{title}</title></head>
		<body><main>{children}</main></body>
	</html>
}

@web.Get("/about")
func About() ui.Node { return <Shell title="About"><p>Pets, since 2026.</p></Shell> }
```
:::

::: go
<!-- go:context
func (s Shell) Render(ctx context.Context, w io.Writer) error {
	return Layout(s.Title, s.Children).Render(ctx, w)
}
-->

```go
type Shell struct {
	Title    string
	Children ui.Node
}

func About() ui.Node { return Shell{Title: "About", Children: ui.HTML(`<p>Pets, since 2026.</p>`)} }

func init() { web.Routes.Get("/about", About) }
```

```html
<!-- shell.vuka.html -->
package views

<html lang="en">
  <head><title>{Title}</title></head>
  <body><main>{Children}</main></body>
</html>
```
:::

## Which to use

- **<span class="vuka-only">JSX</span><span class="go-only">Components</span>** for pages <span class="vuka-only">written in Vuka</span><span class="go-only">written as structs and `.vuka.html` templates or `ui.HTML`</span>: typed, with
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
  <span class="vuka-only">JSX</span><span class="go-only">components</span>, or <span v-pre>`{{define}}`</span> them in one html file.

Views are styled with Tailwind classes out of the box: see [Styling with Tailwind](/web/styling).
