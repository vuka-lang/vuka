# JSX

**Go:** HTML comes from `html/template`, string building, or a template
language with its own files and its own generator.

**Vuka adds** JSX: markup is an expression in a `.vuka` file, and a component is
a function. What the markup *means* — the node type, how it renders — comes
from a library the file imports, its **JSX target**, the way TypeScript's
`jsxImportSource` picks React, Preact or Solid. The examples here use
[`github.com/vuka-lang/ui`](/ui/), the default target.

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

## Markup

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

## Components

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
  as a call's are from its arguments, or given on the tag: `<List[User] …>`. A
  generic props struct needs them given.
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

## JSX targets

A file's JSX lowers to calls of the one JSX target it imports:

```vuka
import "github.com/vuka-lang/ui" // or import h "…", or import . "…"

func Hello(name string) ui.Node { return <p>Hello, {name}!</p> }
```

A file with JSX and no target is an error at its first JSX expression —
`JSX needs a target: import github.com/vuka-lang/ui (or another package
declaring VukaJSX)` — and so is a file importing two. There is no module-wide
setting: the import is the switch, file by file, as anything else in Go.

A package is a JSX target when it declares `const VukaJSX = 1`, the version
of the contract it implements, and these names:

| Name | Required | Used for |
|---|---|---|
| `Node` | yes | the type every JSX expression has; a component returns one |
| `Attr` | yes | `struct{ Name string; Value any }`, an element's attribute |
| `El(tag string, attrs []Attr, children ...Node) Node` | yes | an element |
| `Text(any) Node`, `Child(any) Node` | yes | text, a `{expr}` child |
| `Fragment(children ...Node) Node` | yes | `<>…</>`, a component's children |
| `Nodes(func(add func(Node))) Node` | yes | a `{for}`, `{if}` or `{match}` block |
| `Try(Node, error) Node` | yes | a `(Node, error)` component |
| `On(handler) …` | no | `onClick={…}` on an element is an event; without `On`, a plain attribute |
| `Stateful` + `Component(site string, key any, props S) Node` | no | a tag naming a struct type whose pointer implements `Stateful` |
| `WithChildren(n, children Node) Node` | no | children for a component that reads them from its context (a templ component) |
| `F(fingerprint uint64, shape string, children ...Node) Node` | no | frames: each piece of JSX with its compile-time shape, for render trees |

What the compiler checks is generic, against those declarations: a
component's result is assignable to `Node`; a stateful tag's pointer
implements `Stateful` (the message gives go/types' reason, and the target's
struct to embed when the missing method is unexported); an event attribute's
value is a function, `nil`, or a value of a type with methods. What a
handler may take, what a `Mount` looks like, how a value renders, is the
target's business, checked by its runtime.

## What it becomes

Calls of the target, with your expressions where you wrote them:

```go
return ui.F(0x8ec4a9df618b03d4, "Es(E(tht)E(b)…)", ui.El("section", []ui.Attr{{Name: "className", Value: "pets"}},
	ui.El("h1", nil, ui.Text("Pets ("), ui.Child(len(pets)), ui.Text(")")),
	ui.El("table", nil, ui.Nodes(func(__add func(ui.Node)) {
		for _, p := range pets { __add(ui.F(0xa26a48f8c2b75e32, "h", PetRow(p))) }
	})),
	…))
```

For a target declaring `F`, each piece of JSX — a tree, each block body, a
component tag's children — is wrapped in a frame: a fingerprint of what its
fixed markup is made of (tags, attribute names and literal values, text,
where the expressions go; formatting doesn't change it) and its *shape*,
which of its nodes are markup the source fixes and which are Go expressions
(`t` text, `h` an `{expr}`, `b` a block, `c` a component tag, `E` an element
with an `s` or `d` per attribute, `G` a fragment). A target without `F` gets
the same calls without the frames. `vuka explain` shows it line by line.

## Known limits

- A generic stateful component needs its type arguments on the tag:
  `<Box[int] Value={7} />`.
- JSX goes in function bodies and package-level variable initialisers, not
  in constant declarations or struct tags (Go allows neither an expression
  there).
