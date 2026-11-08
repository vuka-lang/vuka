# Widgets

[[toc]]

## Introduction

A form field's control is a widget: an ordinary component, from package
`github.com/vuka-lang/ui/v2/widget` or written by the app. The form fills in
what the widget asks for — the field's label, its error, its value, its
choices — so a widget is markup and nothing else, and one written for the
app sits beside the built-in ones on equal terms:

```html
<form for={Form} onSubmit={save}>
  <widget.Summary/>
  <widget.Field for={Form.Name}/>
  <widget.Textarea model={Form.Notes} rows={6}/>
  <components.ProductPicker model={Form.Product}/>
  <button>Save</button>
</form>
```

The plain tags (`<input model>`, `<select model>`) stay the building blocks:
a widget's markup is made of them.

## The Built-in Widgets {#built-in}

| Widget | Control | Options |
|---|---|---|
| `widget.Text`, `Email`, `Password`, `Tel`, `URL`, `Search` | `<input>` of that type | `Class` |
| `widget.Number` | `<input type="number">` | `Min`, `Max`, `Step` |
| `widget.Textarea` | `<textarea>` | `Rows` (3) |
| `widget.Select`, `widget.MultiSelect` | `<select>` of the field's choices | `Size` (MultiSelect) |
| `widget.Radios`, `widget.Checkboxes` | a radio or a checkbox per choice | |
| `widget.Checkbox`, `widget.Switch` | one box; a switch is `role="switch"` | |
| `widget.Date`, `DateTime`, `Time` | the date and time pickers | `Min`, `Max` (`Step`) |
| `widget.File` | `<input type="file">` | `Accept` |
| `widget.Related` | a select of an `orm.FK`'s rows | |
| `widget.RelatedSearch` | a search box and the rows it finds | `Limit` (20) |

A widget's options are its fields: `<widget.Textarea rows={6}/>` in markup,
`widget.Textarea{Rows: 6}` in Go.

The form's other components come from the same package:
`<widget.Field for={…}/>` (label, hint, error and the field's widget),
`<widget.Error for={…}/>`, `<widget.Summary/>`, `<widget.Fieldset …>` and
`<widget.Choices for={…}/>`. Their former names in `ui` (`ui.Field`, …)
still work, as the same components; vukavet notes each use
([W0320](/errors/W0320)).

## Which Widget a Field Gets {#resolution}

First to last:

1. The markup's: `<widget.Radios model={Form.Plan}/>`,
   `<components.ProductPicker model={Form.Product}/>`.
2. `@ui.Spec{Widget: …}` on the field: on a form type's `ui.Meta` member
   for that form, or on the model's field for every form of it.
3. `widget.Use[T]`: the app's widget for every field of type `T` (and of
   `Option[T]` or `*T`), called once as the app starts.
4. The control the field's type gives: a text input, a checkbox for a
   bool, a date picker for a `time.Time`, a select for a field with
   choices or an `orm.FK`.

`<widget.Field for={Form.X}/>` and a childless `<form for={Form}>` follow the
same order. A tag or attribute naming a control (`input:"textarea"`,
`@ui.Widget("password")`) wins over `widget.Use[T]`.

<!-- go:context
type Product struct {
	Name  string
	Notes string
	Plan  string
}
-->

```go
package products

import (
	"time"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/widget"
)

type ProductForm struct {
	ui.FormFor[Product]
	Notes ui.Meta
	Plan  ui.Meta
}

func init() {
	widget.Use[time.Time](widget.DateTime{})
}
```

```vuka
type ProductForm struct {
	ui.FormFor[Product]

	Notes ui.Meta @ui.Spec{Widget: widget.Textarea{Rows: 4}}
	Plan  ui.Meta @ui.Spec{Widget: widget.Radios{}}
}
```

## Writing a Widget {#writing}

A widget is a component embedding `widget.Props` (or with a `Model
ui.FieldRef` field of its own). The form fills, by name and type, the props
it declares that are still empty — what the tag sets wins:

| Prop | Filled with |
|---|---|
| `Model ui.FieldRef` | the field: `<input model={Model}/>` gives a control its name, id, value, type and ARIA links |
| `Label`, `Help`, `Placeholder string` | the field's, in the visitor's language |
| `Error string` | the field's error, while it shows one |
| `Required`, `Disabled bool` | the field's |
| `Value T` | the field's value; an `Option`'s value when it holds one |
| `Choices []widget.Choice[T]` | the field's choices, each with `Value`, `Label`, `Selected` and its `Row` |
| `Search func(ctx, q string) []widget.Choice[T]` | the field's rows whose label holds `q`, up to the widget's `Limit` |

`Props` is the contract a widget embeds; `widget.Field` is the field group
component. A `Value` of another type than the field's, or `Choices` whose
`Row` isn't the type of the field's rows, is a compile error
([E0195](/errors/E0195)).

```html
<!-- components/product_picker.vuka.html -->
<script lang="go">
package components

import (
    "github.com/vuka-lang/ui/v2/widget"

    "example.com/shop/models"
)

// ProductPicker shows each product as a card to choose.
type ProductPicker struct {
    widget.Props
    Choices []widget.Choice[models.Product]
}
</script>

<fieldset class="picker" aria-describedby={Model.ID() + "-error"}>
  <legend>{Label}</legend>
  if Error != "" {
    <p id={Model.ID() + "-error"} class="picker-error">{Error}</p>
  }
  for _, c := range Choices {
    <label class:chosen={c.Selected}>
      <input type="radio" model={Model} value={c.Value}/>
      {c.Row.Name} — {c.Row.Price}
    </label>
  }
</fieldset>
```

A widget written in Go fills itself as it renders, with `ui.FillWidget(ctx,
&w)`, as the built-in ones do. A widget's `FieldLayout()` method says how
`<widget.Field>` lays it out: `"check"` puts the label beside it (a
checkbox), `"fieldset"` makes the label a legend (radios).

## Related Fields {#related}

An `orm.FK[T]` field is a select of every row of `T` (`widget.Related`),
labelled by the rows' `String()` (else their `Name`, `Title` or `Label`
field); an optional one (`@orm.Null`) has an empty first option.

A `<Field>Choices` method returning a query narrows the rows, and is the
allow-list a submitted key is checked against — a key the query doesn't
hold is the field's error, never saved. The method reads the form's values,
so the choices follow other fields: a field it reads changing re-renders
the choices, and a value no longer offered is cleared.

```vuka
type Order struct {
	orm.Model[Order]
	ID       int64
	Customer orm.FK[Customer]
	Address  orm.FK[Address]
}

type OrderForm struct {
	ui.FormFor[Order]
}

// AddressChoices are the customer's addresses.
func (f *OrderForm) AddressChoices() orm.QuerySet[Address] {
	return Address.Objects.Filter(Address.Customer.Eq(f.Value.Customer))
}
```

A table too large to list takes `widget.RelatedSearch`: a search box, and the
rows whose label holds what was typed (`Limit`, 20 by default) as radios.
The current row stays offered, and the submitted key is checked against the
same query.

```vuka
type OrderForm struct {
	ui.FormFor[Order]
	Customer ui.Meta @ui.Spec{Widget: widget.RelatedSearch{Limit: 10}}
}
```

## Widgets of Several Inputs {#multi}

A widget with `Format` and `Parse` methods owns several inputs for one
field: `Format` turns the field's value into what each input shows,
`Parse` what they send into the value. Each input is named
`Model.Part(name)` and shows `Model.PartValue(name)` — what was typed, kept
when it didn't parse. `Parse`'s error is the field's.

```go
package components

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vuka-lang/ui/v2/widget"
)

type Amount struct {
	Cents    int64
	Currency string
}

// Money edits an Amount as a number and a currency.
type Money struct{ widget.Props }

func (Money) Format(v Amount) map[string]string {
	return map[string]string{"amount": fmt.Sprintf("%d.%02d", v.Cents/100, v.Cents%100), "currency": v.Currency}
}

func (Money) Parse(in map[string]string) (Amount, error) {
	whole, frac, _ := strings.Cut(in["amount"], ".")
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil || len(frac) != 2 {
		return Amount{}, errors.New("Enter an amount such as 12.50")
	}
	return Amount{Cents: n, Currency: in["currency"]}, nil
}
```

```html
<input name={Model.Part("amount")} value={Model.PartValue("amount")} inputmode="decimal"/>
<select name={Model.Part("currency")}>
  <option selected={Model.PartValue("currency") == "EUR"}>EUR</option>
  <option selected={Model.PartValue("currency") == "USD"}>USD</option>
</select>
```

## Errors and the Summary {#errors}

`widget.UseError` and `widget.UseSummary` make an app's components what
renders every field's error and every form's error summary. They embed
`widget.ErrorProps` (`Model`, `Message`, `Attrs`) and
`widget.SummaryProps` (`Title`, `Items`, `Attrs`), and put `Attrs` on
their root element: the ids and data attributes ARIA and the runtime rely
on.

```go
package components

import "github.com/vuka-lang/ui/v2/widget"

type FieldError struct{ widget.ErrorProps }

func init() { widget.UseError(FieldError{}) }
```

```html
<p {Attrs...} class="text-sm text-red-600">{Message}</p>
```
