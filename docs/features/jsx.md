# JSX

**Go:** HTML comes from `html/template`, string building, or a template
language with its own files and its own generator.

**Vuka adds** JSX: markup is an expression in a `.vuka` file, and a component is
a function. What the markup *means* — the node type, how it renders — comes
from a library the file imports, its **JSX target**, the way TypeScript's
`jsxImportSource` picks React, Preact or Solid. The examples here use
[`github.com/vuka-lang/ui`](/ui/), the default target.

::: vuka
```vuka
import "github.com/vuka-lang/ui"

type Pet struct {
	Name string
	Age  int
}

func PetRow(pet Pet) ui.Node {
	return <tr><td>{pet.Name}</td><td>{pet.Age}</td></tr>
}

func Pets(pets []Pet, admin bool) ui.Node {
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
:::

::: go
In Go, markup is a [`.vuka.html` template](/ui/templates) kept beside the
package's Go, or written inline with [`ui.HTML`](/ui/#markup-in-go): HTML
whose holes are labelled with the Go of their values, the values after it
in the order the labels first appear. The same pets, as a template:

```html
<!-- pets.vuka.html -->
func Pets(pets []Pet, admin bool)
<section class="pets">
  <h1>Pets ({len(pets)})</h1>
  <table>
    for _, p := range pets {
      <tr><td>{p.Name}</td><td>{p.Age}</td></tr>
    }
  </table>
  if admin {
    <button>Add a pet</button>
  }
</section>
```

and inline, `ui.Each` and `ui.If` for the list and the condition:

```go
type Pet struct {
	ID   int
	Name string
	Age  int
}

func PetRow(pet Pet) ui.Node {
	return ui.HTML(`<tr><td>{pet.Name}</td><td>{pet.Age}</td></tr>`, pet.Name, pet.Age)
}

func Pets(pets []Pet, admin bool) ui.Node {
	rows := ui.Each(pets, PetRow)
	add := ui.If(admin, ui.HTML(`<button>Add a pet</button>`))
	return ui.HTML(`
<section class="pets">
	<h1>Pets ({len(pets)})</h1>
	<table>{rows}</table>
	{add}
</section>`, len(pets), rows, add)
}
```

The rest of this page is about `.vuka` files; for Go, the
[templates](/ui/templates) page and [markup in Go](/ui/#markup-in-go) have
the details.
:::

## Markup

::: vuka
| JSX | |
|---|---|
| `<div a="x">` | a string attribute; HTML entities (`&amp;`) are decoded |
| `<div a={expr}>` | any Go expression |
| `<input checked>` | `true`: written bare |
| `{expr}` | a child; what a value becomes is the target's business (ui: a node as is, a string or number as text, an `error` as its message, a slice of nodes as each node, `nil` as nothing) |
| `<>…</>` | a fragment: children with no element around them |
| `{/* … */}` | a comment |
| `on<Letter>…={expr}` | an event attribute, when the target has events (below) |
| `key={…}` | names an element or component among its siblings |

Whitespace follows React: text on one line keeps its spaces, a line break
between text and a tag disappears, and lines of text are joined by one space.
:::

::: go
A template's or `ui.HTML`'s holes take any Go expression; what each place
takes — a child, an attribute's value, an event, a toggled class — is in
[holes](/ui/templates#holes). HTML keeps HTML's names: `class`, `for`.
:::

## Blocks

<!-- go:context
type User struct {
	ID   int
	Name string
	Year int
}

func lookup(id int) (User, error) { return User{ID: id}, nil }

type Item struct {
	Name  string
	Done  bool
	Tags  []string
	Owner string
}
-->

::: vuka
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

`match` is the [same match](/features/match), exhaustive.

Inside a block's body, a block nests without its braces: a body is already
in braces, so `if`, `for` and `match` starting a child there open a nested
block, with the same headers and `else if`/`else` chains:

```vuka
{for _, it := range items {
	if it.Done {
		<li className="done">{it.Name}</li>
	} else {
		<li>{it.Name}</li>
	}
	for _, t := range it.Tags { <i>{t}</i> }
	match it.Owner {
	case Some(o):
		<b>{o}</b>
	case None:
		<i>unassigned</i>
	}
}}
```

A word starts a nested block when it is `if` or `for` followed by a space or
`(`, or `match` followed by an expression, at the start of a child: after the
body's `{`, or after another child. Anywhere else it is text: in an element
(`<p>if you like</p>`), in the middle of text, or not followed that way
(`iffy`, `if-then`). Write `{"if"}` for the word itself at the start of a
child in a body. Only `if`, `for` and `match` nest; other Go statements
still go in a function literal.

The Go inside braces is Vuka too: a function literal there takes `?` and
`match` like any other function, at any depth.
:::

::: go
In a template, `for`, `if` and `switch` are statements at the start of a
line, their bodies markup, nesting as Go's do:

```html
<!-- users.vuka.html -->
func Users(users []User, found User, err error)
<ul>
  for _, u := range users {
    <li key={u.ID}>{u.Name}</li>
  }
</ul>
if len(users) == 0 {
  <p>nobody yet</p>
} else {
  <p>{len(users)} users</p>
}
if err != nil {
  <p class="error">{err}</p>
} else {
  <p>{found.Name}</p>
}
```

Inline, the statements are Go's, before the `return`, building the nodes a
hole takes — a `[]ui.Node` is each node:

```go
func Users(users []User, id int) ui.Node {
	var items []ui.Node
	for _, u := range users {
		items = append(items, ui.HTML(`<li key={u.ID}>{u.Name}</li>`, u.ID, u.Name))
	}
	count := ui.HTML(`<p>nobody yet</p>`)
	if len(users) > 0 {
		count = ui.HTML(`<p>{len(users)} users</p>`, len(users))
	}
	var who ui.Node
	if u, err := lookup(id); err != nil {
		who = ui.HTML(`<p class="error">{err}</p>`, err)
	} else {
		who = ui.HTML(`<p>{u.Name}</p>`, u.Name)
	}
	return ui.HTML(`<ul>{items}</ul>{count}{who}`, items, count, who)
}
```
:::

## Components

<!-- go:context
func (c UserCard) Render(ctx context.Context, w io.Writer) error { return nil }
-->

::: vuka
A tag that starts with a capital letter, or has a dot (`<theme.Card>`), is a
function or method call:

```vuka
func UserCard(user User, admin bool, children ui.Node) ui.Node { … }

<UserCard user={u} admin>since {u.Year}</UserCard>
```

- Attributes bind to **parameters by name**, in any order; `user` and `User`
  both find a parameter `user`. A parameter left out gets its zero value.
- Children go to a parameter named `children`. A component of your own with
  none takes no children: `<Layout title="x"><p>…</p></Layout>` for `func
  Layout(title string) ui.Node` is an error, not children silently dropped.
- A function taking **one struct**, or a variadic of one (`func Button(props
  ...ButtonProps)`, templUI's shape), takes the attributes as its fields, and
  the children as a `Children` field. An attribute naming the parameter itself
  passes the struct whole: `<PetRow pet={p}/>` for `func PetRow(pet Pet)`.
- A component returns the target's node type, or a node and an `error`; the
  error is the target's to render (ui fails the render, unless an
  `ui.ErrorBoundary` above it shows a fallback).
- A **generic** component's type arguments are inferred from its attributes,
  as a call's are from its arguments, or given on the tag: `<List[User] …>`.
  This holds for a generic props struct too — `<Table Rows={rows}
  Cell={fn}/>` for `func Table[T any](p TableProps[T])` infers `T` by matching
  each field's declared type (`Rows []T`, `Cell func(T) string`) against the
  attribute's value — and for a generic **stateful** component the same way:
  `<Box Value={7}/>` for `type Box[T any] struct{ ui.Live; Value T }` infers
  `T = int` and writes `&Box[int]{Value: 7}`. Give the arguments on the tag
  when no attribute pins a parameter, or when they would conflict.
- An **overloaded** component's overload is the one its attributes' types fit
  best, as for a call.
- A props struct's **promoted fields** are attributes too: a struct embedding
  `Base{ID int}` takes `id={…}`, written into the literal as `Base: Base{ID: …}`
  (`&Base{…}` for an embedded pointer). A field two embedded structs both have
  at the same depth is ambiguous, as in Go, and an error.
- A tag naming a **struct type** is a stateful component, when the target has
  them: [ui's stateful components](/ui/stateful).

```vuka
func List[T any](items []T, render func(T) ui.Node) ui.Node { … }

<List items={users} render={func(u User) ui.Node { return <li>{u.Name}</li> }} />
```

Mistakes are compile errors at the tag:

```text
main.vuka:12:14: <UserCard> has no attribute usr; did you mean user?
main.vuka:20:3: expected </p> to close <p> at line 18, found </div>
main.vuka:31:3: <Plain> returns int, which isn't ui.Node (missing method Render)
```
:::

::: go
A component is a struct whose fields are its props, and a `ui.Node` itself;
its markup is the `.vuka.html` file of the same name, its `Children` field
what goes between the tags:

```go
// usercard.go
type UserCard struct {
	User     User
	Admin    bool
	Children ui.Node
}
```

```html
<!-- usercard.vuka.html -->
func (c UserCard) View()
<div class="user-card">
  <h3>{c.User.Name}</h3>
  if c.Admin {
    <span class="badge">admin</span>
  }
  {c.Children}
</div>
```

<!-- go:fragment -->
```go
u := User{Name: "ada", Year: 2020}
card := UserCard{User: u, Admin: true, Children: ui.HTML(`since {u.Year}`, u.Year)}
```

A field left out is its zero value, and a wrong one the compiler's error on
the literal. A generic component takes its type arguments as a generic
struct does, `List[User]{…}`; a function works too:

```go
func List[T any](items []T, render func(T) ui.Node) ui.Node {
	rows := ui.Each(items, render)
	return ui.HTML(`<ul>{rows}</ul>`, rows)
}
```

<!-- go:fragment -->
```go
users := []User{{Name: "ada"}}
list := List(users, func(u User) ui.Node { return ui.HTML(`<li>{u.Name}</li>`, u.Name) })
```

A struct embedding `ui.Live` is a [stateful component](/ui/stateful),
`&Counter{…}`, or `ui.Key(k, &Counter{…})` with a key.
:::

## JSX targets

::: vuka
A file's JSX lowers to calls of the one JSX target it imports:

```vuka
import "github.com/vuka-lang/ui" // or import h "…", or import . "…"

func Hello(name string) ui.Node { return <p>Hello, {name}!</p> }
```

A file with JSX and no target is an error at its first JSX expression —
`JSX needs a target: import github.com/vuka-lang/ui (or another package
declaring VukaJSX)` — and so is a file importing two. There is no module-wide
setting: the import is the switch, file by file, as anything else in Go.

A package is a JSX target when it declares `const VukaJSX = 3`, the version
of the contract it implements, and these names:

| Name | Required | Used for |
|---|---|---|
| `Node` | yes | the type every JSX expression has; a component returns one |
| `HTML(markup string, values ...any) Node` | yes | each piece of JSX: its markup with holes labelled by your expressions, the values in the order the labels first appear |
| `Try(Node, error) Node` | yes | a `(Node, error)` component |
| `El`, `Safe`, `Child` | yes | scripts and styles with values in them |
| `Child(any) Node`, `Fragment(children ...Node) Node` | yes | placeholders while a component's type is unknown |
| `On(handler) …` | no | `onClick={…}` on an element is an event, its value checked; without `On`, a plain attribute |
| `Stateful` + `Key(key any, props Stateful) Node` | no | a tag naming a struct type whose pointer implements `Stateful` |
| `WithChildren(n, children Node) Node` | no | children for a component that reads them from its context (a templ component) |
| `UseState`, `UsePageState`, State's methods | no | [client state](/ui/state) |
| `Ref(names ...string) …` | no | refs: `ref="name"` on an element names it for code in the same function, which finds it with `Ref("name")` (ui: [JS commands](/ui/js)) |
| `GetAttr(name string, values ...string) …` | no | elements picked by an attribute the function's JSX writes |

What the compiler checks is generic, against those declarations: a
component's result is assignable to `Node`; a stateful tag's pointer
implements `Stateful` (the message gives go/types' reason, and the target's
struct to embed when the missing method is unexported); an event attribute's
value is a function, `nil`, or a value of a type with methods; and, for a
target declaring `Ref`, each literal name of a `Ref("name", …)` in a function
with JSX names an element of that function's JSX — `no ref="mnu" in Menu's
render; did you mean "menu"?`. A function with a computed `ref={…}` isn't
checked, nor a `Ref` outside any function with JSX (a helper building a chain
for another). For a target declaring `GetAttr`, `GetAttr("name", "usrname")`
in such a function, when its JSX writes `name` literally and never computes
it, must name a value one of its elements has — `ui.GetAttr("name",
"usrname"): no element in Login has name="usrname"; did you mean
"username"?`; values that aren't string literals aren't checked. What a
handler may take, what a `Mount` looks like, how a value renders, is the
target's business, checked by its runtime.
:::

::: go
Go code calls the target's functions itself — `ui.HTML`, `ui.Each`,
`ui.If`, `ui.Key`:

```go
func Hello(name string) ui.Node { return ui.HTML(`<p>Hello, {name}!</p>`, name) }
```
:::

## What it becomes

::: vuka
The Go you would write by hand. Each piece of JSX — a tree, each block
body, a component tag's children — is a `ui.HTML` call: the markup with
your expressions as its holes' labels, written where you wrote them, and
the same expressions after it as the values. A block is Go statements
before the `return`, building a `[]ui.Node` that a hole takes:

```vuka
func PetList(pets []Pet) ui.Node {
	return <section className="pets">
		<h1>Pets ({len(pets)})</h1>
		<table>
			{for _, p := range pets {
				<tr key={p.ID}><td>{p.Name}</td></tr>
			}}
		</table>
	</section>
}
```

```go
func PetList(pets []Pet) ui.Node {
	var trs []ui.Node
	for _, p := range pets {
		trs = append(trs, ui.HTML(`<tr key={p.ID}><td>{p.Name}</td></tr>`, p.ID, p.Name))
	}
	return ui.HTML(`
<section class="pets">
  <h1>Pets ({len(pets)})</h1>
  <table>{trs}</table>
</section>`, len(pets), trs)
}
```

A `{for}` is the same `for`; an `{if}` an `if`/`else`; a `{match}` a chain
of `if`s over its cases, each branch appending its markup. A stateful
component's tag is `&Counter{…}` (`ui.Key(k, &Counter{…})` with a key), an
island's its props (`ui.Mount(props, opts…)` with options); a tag inside a
loop is computed into a variable first. `vuka explain` shows it line by
line.

`vuka eject -rm` writes a file's markup functions out as a
[`.vuka.html` template](/ui/templates) beside its Go.

A component tag becomes a call of that component. For a function taking one
struct, the attributes and children become that struct's literal:

```go
// <List Items={users} Key={key}>{…}</List>   for  func List[T any](p ListProps[T]) ui.Node
List(ListProps[User]{Items: users, Key: key, Children: ui.HTML(`…`, …)})
```

The inferred type argument lands on the **props struct literal**
(`ListProps[User]`); the function's own parameter (`List`'s `T`) is then
inferred by Go from that argument, so the call needs no `[User]`. Writing the
arguments on the tag (`<List[User] …/>`) lowers the same way — it only pins
`T` yourself when the attributes can't.
:::

::: go
A `.vuka` file's JSX compiles to `ui.HTML` calls like the ones on this
page, and a
`.vuka.html` template to the same, written by `vuka gen` as `x_html.go`
beside it, so `go build` needs no `vuka` command.
:::

## Known limits

::: vuka
- JSX goes in function bodies and package-level variable initialisers, not
  in constant declarations or struct tags (Go allows neither an expression
  there).
:::

::: go
`ui.HTML`'s markup is a string, so it goes wherever Go takes an expression.
:::
