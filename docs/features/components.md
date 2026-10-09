# Components and JSX

**Go:** HTML comes from `html/template`, string building, or a template
language with its own files and its own generator.

**Vuka adds** JSX: markup is an expression in a `.vuka` file, and a component is
a function returning a `vuka.Node`.

```vuka
type Pet struct {
	Name string
	Age  int
}

func PetRow(pet Pet) vuka.Node {
	return <tr><td>{pet.Name}</td><td>{pet.Age}</td></tr>
}

func Pets(pets []Pet, admin bool) vuka.Node {
	return <section className="pets">
		<h1>Pets ({len(pets)})</h1>
		<table>
			{for _, p := range pets {
				<PetRow pet={p} />
			}}
		</table>
		{if admin { <button>Add a pet</button> }}
	</section>
}
```

Rendering is the runtime's job, and it is built on
[templ](https://templ.guide): `vuka.Node` is `templ.Component`, so a Vuka
component works anywhere a templ component does, and the other way round.

```vuka
http.Handle("/pets", vuka.Handler(func(r *http.Request) (vuka.Node, error) {
	return Pets(store.All(), true), nil
}))
```

## Markup

| JSX | |
|---|---|
| `<div a="x">` | a string attribute; HTML entities (`&amp;`) are decoded |
| `<div a={expr}>` | any Go expression |
| `<input checked>` | `true`: written bare |
| `className`, `htmlFor` | `class` and `for`, as in React |
| `{expr}` | a child: a `vuka.Node` as is, a string or number as text, an `error` as its message, a `[]vuka.Node` as each node, `nil` as nothing |
| `<>…</>` | a fragment: children with no element around them |
| `{/* … */}` | a comment |

Text is escaped, and whitespace follows React: text on one line keeps its
spaces, a line break between text and a tag disappears, and lines of text are
joined by one space.

An attribute's value renders the way templ renders it: `nil` and `false` leave
it out, `class` takes anything `templ.Classes` does, `style` takes a
`vuka.Style{"color": "red"}`, and `href` on `<a>`, `action` on `<form>` are
checked as URLs (a `javascript:` URL becomes a harmless placeholder; wrap a
trusted one in `templ.SafeURL`). `vuka.Safe(html)` writes HTML unescaped.

## Blocks

`for`, `if` and `match` go inside braces, and their bodies are markup:

```vuka
<ul>
	{for _, u := range users {
		<li key={u.ID}>{u.Name}</li>
	}}
</ul>
{if len(users) == 0 {
	<p>nobody yet</p>
} else {
	{len(users)} users
}}
{match lookup(id) {
case Ok(u):
	<p>{u.Name}</p>
case Err(e):
	<p className="error">{e}</p>
}}
```

`match` is the [same match](/features/match), exhaustive. `key` is accepted
and ignored for now; it starts to matter with stateful components.

## Components

A tag that starts with a capital letter, or has a dot (`<theme.Card>`), is a
function or method call:

```vuka
func UserCard(user User, admin bool, children vuka.Node) vuka.Node { … }

<UserCard user={u} admin>since {u.Year}</UserCard>
```

- Attributes bind to **parameters by name**, in any order; `user` and `User`
  both find a parameter `user`. A parameter left out gets its zero value.
- Children go to a parameter named `children`.
- A function taking **one struct**, or a variadic of one (`func Button(props
  ...ButtonProps)`, templUI's shape), takes the attributes as its fields, and
  the children as a `Children` field. An attribute naming the parameter itself
  passes the struct whole: `<PetRow pet={p}/>` for `func PetRow(pet Pet)`.
- A component returning `(vuka.Node, error)` renders its error: the page fails
  to render with that error, unless a `vuka.ErrorBoundary` above it shows a
  fallback instead.

Mistakes are compile errors at the tag:

```text
main.vuka:12:14: <UserCard> has no attribute usr; did you mean user?
main.vuka:20:3: expected </p> to close <p> at line 18, found </div>
```

## templ, both ways

`.templ` files sit in the same packages as `.vuka` and `.go` files. Vuka
compiles them with templ's own parser and generator, in memory, so there is no
`templ generate` step and nothing extra to commit. Their errors and go to
definition point into the `.templ` file.

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
func Page(pets []Pet) vuka.Node {
	return <Shell title="Pets">        // a templ component, called from JSX
		<table>{for _, p := range pets { <PetRow pet={p} /> }}</table>
		<Footer pets={pets} />
	</Shell>
}
```

A templ component has no `children` parameter: JSX children reach its
`{ children... }` (through `templx.WithChildren`). Component libraries written
for templ, such as templUI, work as they are.

## Rendering

| | |
|---|---|
| `vuka.Handler(f)` | an `http.Handler` rendering the node `f` builds per request |
| `vuka.Write(w, r, n)` | render into a response, all at once, so an error still gets a clean 500 |
| `vuka.String(ctx, n)` | render to a string |
| `n.Render(ctx, w)` | templ's own method, to any `io.Writer` |

A JSX tree is a value you can walk, not only HTML: `vuka.Walk` hands its
elements, text and raw HTML to a `vuka.Renderer`. The package
`github.com/vuka-lang/vuka/term` renders the same components as terminal text:
headings, lists, aligned tables, wrapped paragraphs and optional color.

```vuka
term.Render(ctx, os.Stdout, Pets(pets, false), term.Options{Width: 80, Color: true})
```

`term` reads the HTML of templ components too, so a page inside a templ
layout keeps its headings, tables and lists.

## What it becomes

Calls to the runtime, with your expressions where you wrote them:

```go
return vuka.El("section", []vuka.Attr{{Name: "className", Value: "pets"}},
	vuka.El("h1", nil, vuka.Text("Pets ("), vuka.Child(len(pets)), vuka.Text(")")),
	vuka.El("table", nil, vuka.Nodes(func(__add func(vuka.Node)) {
		for _, p := range pets { __add(PetRow(p)) }
	})),
	…)
```

`vuka explain` shows it line by line.

## Coming next

Stateful components: a struct with `Mount`, `Update` and `Render`, event
handlers (`onClick={…}`), state that re-renders only what changed, live over a
WebSocket. Until then `on…` attributes with expressions are an error, and a tag
naming a type says so.

Known limits today: JSX goes inside function bodies; `?`, `match` and decorators
inside a function literal within `{…}` aren't rewritten; components can't be
generic or overloaded; a props struct binds its own fields, not embedded ones.
