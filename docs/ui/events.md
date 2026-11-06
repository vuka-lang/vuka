# Events

[[toc]]

## Introduction

An `on…` attribute says what an element does when an event fires on it.
Most of the time that is a call of one of the shape's methods, with
something the browser knows at that moment: what was picked, whether a box
is ticked, a row's id. You write it as the call it is:

```html
<select name="status" onChange={Choose(this.value)}>…</select>
```

The method runs on the server when the visitor picks a status, never while
the markup renders.

## Calling a Method {#calling-a-method}

A call of one of the page's exported methods, or a live component's, in an
`on…` attribute is a [push](/ui/js#push): `onChange={Choose(this.value)}`
is `onChange={ui.JS.Push(Choose, ui.This.Value())}`. The method's arguments
are the call's, sent when the event fires:

```html
<script lang="go">
package pages

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

// Orders is a list the visitor sorts and prunes.
type Orders struct {
	ui.Live
	orders []Order
	sort   string
}

func (p *Orders) Sort(by string)                    { p.sort = by }
func (p *Orders) Remove(ctx context.Context, id int64, store *Store) error { return store.Delete(ctx, id) }
func (p *Orders) Ship(id int64, on bool) error      { return nil }
</script>

<main>
  <select onChange={Sort(this.value)}>
    <option value="date">Date</option>
    <option value="total">Total</option>
  </select>
  for _, o := range orders {
    <li key={o.ID}>
      <input type="checkbox" onChange={Ship(o.ID, this.checked)} />
      <button onClick={Remove(ctx, o.ID)}>Remove</button>
    </li>
  }
</main>
```

- A Go value — `o.ID` — is the one the row rendered with.
- `ctx`, first, is the event's context; it may be left out.
- The method's pointer and interface parameters — `store *Store` — are
  the dependencies the app injects, as for any push: the call leaves them
  out.

On a live page the method runs on its instance over the page's socket, and
the page renders again. On a page that isn't live it runs on a fresh copy
of the page over HTTP, and what it returns goes [into the
element](/ui/js#results). The method's result and arguments follow
[Push's rules](/ui/js#push-arguments).

## Reading the Element {#element-reads}

`this` is the element the event is on. Its reads are made in the browser
when the event fires:

| Read | Is | For a parameter |
|---|---|---|
| `this.value` | the element's value: an input's text, a select's choice | a string, number or bool, converted |
| `this.checked` | whether a checkbox or radio is checked | a bool |
| `this.<attr>` | an attribute's text; a dashed name is camelCase, `this.dataId` for `data-id` | a string |

A read is an argument of the method call itself. Anywhere else — inside
another call, in a hole — it is [E0170](/errors/E0170).

## Events on Components {#components}

The same call works on a component's tag. An `on…` attribute the component
doesn't declare as a prop goes on to the element it spreads its attributes
on, as an `Attrs []ui.Attr` field spread with `{Attrs...}`:

```html
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// Combobox is a select of choices.
type Combobox struct {
	Name    string
	Options []ui.Option
	Attrs   []ui.Attr
}
</script>

<select name={Name} {Attrs...}>
  for _, o := range Options {
    <option value={o.Value}>{o.Label}</option>
  }
</select>
```

```html
<components.Combobox name="status" options={choices()} onChange={Choose(this.value)} />
```

`this` is then the element the component spreads its attributes on, the
`<select>`. A component with no `Attrs` field takes no `on…` attribute it
doesn't declare: that is [E0106](/errors/E0106), naming the component.

A prop that takes browser commands — `OnClick ui.JSChain`, or `any` —
takes the call too, and the component forwards it to its element:
`<Chip label="Manage" onClick={Select(id)}/>`. See [Handing a Click to a
Child](/ui/state#click-to-a-child).

## Calls That Keep Their Meaning {#other-calls}

Only a call of the shape's exported methods is a push. Everything else is
the Go value it is:

| | |
|---|---|
| `onClick={toggle(i)}` | an unexported method returning nothing or an error: a handler of the live instance, run on the click |
| `onClick={design.OpenDialog("edit")}` | a function returning browser commands: the commands, built as the markup renders |
| `onClick={Open()}` | an exported method returning `ui.Commands` or a func, with no element read: the value it returns, built as the markup renders |
| `onClick={Save(ctx, p.store)}` | a method passed its dependencies: a handler of the live instance, run on the click |
| `onClick={Save}` | a method value: the [event handler](/ui/stateful#events) |

To push a method returning `ui.Commands`, read the element in the call or
write the push: `onInput={ui.JS.Push(CheckName, ui.This.Value())}`.

## Checking Calls {#checking}

The compiler checks each call against its method, as it checks a push
([E0168](/errors/E0168)): the number of arguments, each Go value's type,
and that each read converts to its parameter. A name the shape has no
method of is [E0172](/errors/E0172), with the closest method's name:

```text
error[E0172]: Chose isn't a method of Orders: an event calls one of its exported methods
  --> pages/orders.vuka.html:14:21
help: did you mean Choose?
```

In the editor, hover and go to definition on the method open its Go, and
completion after `this.` offers `value`, `checked` and the element's
attributes.
