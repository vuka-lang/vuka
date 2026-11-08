# Forms

[[toc]]

## Introduction

A form is a Go struct. vuka binds what the visitor sends into it, checks it
with the struct's rules and its own methods, renders it with accessible
markup, and keeps its state — which fields the visitor used, which show an
error — on a live page and as a plain HTML POST, from the same code.

You write the struct and a submit handler. The handler runs only once every
check passed, and gets the checked value; everything a visitor reads on the
way — the error under a field, the summary of what went wrong — comes from
the struct's tags and methods.

<!-- go:context
type Users struct{}

func (u *Users) Exists(ctx context.Context, email string) bool { return false }
func (u *Users) Create(ctx context.Context, name, email, password string) error { return nil }

type User struct{ Name, Email string }

func (u *Users) Get(ctx context.Context, id int64) (User, error) { return User{}, nil }
func (u *Users) Rename(ctx context.Context, id int64, name, email string) error { return nil }
func (u *Users) Update(ctx context.Context, user User) error { return nil }

type Geo struct{}

func (g *Geo) Countries(ctx context.Context) ([]ui.Option, error) { return nil, nil }
func (g *Geo) Cities(ctx context.Context, country string) ([]ui.Option, error) { return nil, nil }
-->

## Declaring a Form {#declaring}

A struct embedding `ui.FormState` is a form; a form of a model, or of any
struct, without one of its own is a [`ui.FormFor[T]`](#formfor). Its exported fields are the
values the visitor edits, and the only ones a submit binds; `form:"-"`
leaves one out. Owner, tenant and record ids come from the route or the
session, never from the form.

```go
// account/signup.go
package account

import (
    "context"

    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/ui/v2/forms"
)

type Signup struct {
    ui.FormState
    Name     string `label:"Full name" validate:"required,max=80"`
    Email    string `validate:"required,email" help:"We'll send a confirmation link"`
    Password string `input:"new-password" validate:"required,min=12"`
    Postcode string `input:"numeric" validate:"required,len=5"`
    Plan     string `input:"radio" validate:"required"`
    Agree    bool   `label:"I accept the terms" validate:"required" msg:"required=Accept the terms to continue"`
}

func (f *Signup) PlanChoices() []ui.Option {
    return []ui.Option{{Value: "free", Label: "Free"}, {Value: "pro", Label: "Pro"}}
}

// CleanEmail checks what the rules can't: an address already registered.
func (f *Signup) CleanEmail(ctx context.Context, users *Users) error {
    if forms.Live(ctx) {
        return nil // a database lookup waits for the submit
    }
    if users.Exists(ctx, f.Email) {
        return forms.Errorf("That email address is already registered")
    }
    return nil
}
```

A page's own form may also be declared in the page's file, beside the
page's struct: a struct embedding `ui.FormState` is data, never the struct
the markup renders.

```html
<script lang="go">
package pages

import "github.com/vuka-lang/ui/v2"

type Contact struct {
    Form Message
}

type Message struct {
    ui.FormState
    Email string `validate:"required,email"`
    Text  string `input:"textarea" validate:"required,max=2000"`
}
</script>

<ui.Form for={Form}/>
```

Beside any other struct, the markup renders the struct named after the file
([E0147](/errors/E0147)).

Each field's tags say how it is checked and shown:

| Tag | |
|---|---|
| `validate:` | the rules: `required`, `min=n`, `max=n`, `len=n`, `oneof=a b c`, `email`, `omitempty` |
| `label:` | the label; by default the field's name in words (`FirstName` is "First name") |
| `help:` | a hint shown between the label and the control |
| `input:` | the control: `textarea`, `radio`, `select`, `password`, `new-password`, `one-time-code`, `numeric`, `tel`, `date`, … |
| `autocomplete:` | the `autocomplete` value, when the one inferred from the name and type isn't right |
| `placeholder:` | an example value; never a label |
| `msg:` | a rule's message: `msg:"required=Accept the terms to continue"`; several, `rule=message` separated by commas (see [Errors](#errors)) |
| `trim:"-"` | keep leading and trailing whitespace |
| `form:` | the field's name in the form and its paths (`form:"email"`); `-` leaves it out |
| `depends:` | the field its choices follow (`depends:"Country"`); see [Choices](#choices) |

Three kinds of methods add to the tags. Their dependencies are injected, as
an event handler's are:

- `Clean<Field>(ctx, deps…) error` checks one field once its rules passed;
- `Clean(ctx, deps…) error` checks the form as a whole, after every field;
- `<Field>Choices(ctx, deps…) ([]ui.Option, error)` gives a select's or
  radio group's options (see [Choices](#choices)).

`forms.Live(ctx)` is true for checks run while the visitor types or leaves
a field. A check that is slow, or touches the outside world, returns nil
early when it is: its error then shows on submit, and stays until the field
changes. A field of a group is named by its path without dots:
`CleanAddressCity` for `Address.City`.

## Rendering a Form {#rendering}

A page holds the form as an exported field and renders it on a plain
`<form>` tag: `for={Form}` makes the element the form, bound, checked and
submitted by vuka:

```html
<script lang="go">
package pages

import (
    "context"

    "example.com/app/account"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
)

type Join struct {
    ui.Live
    Form account.Signup
}

func (j *Join) Save(ctx context.Context, f account.Signup, users *account.Users) error {
    if err := users.Create(ctx, f.Name, f.Email, f.Password); err != nil {
        return j.Form.Fail("We couldn't create your account. Try again later")
    }
    return web.Redirect("/welcome")
}
</script>

<main>
  <h1>Create an account</h1>
  <form for={Form} onSubmit={Save} class="stack">
    <label for={Form.Name}/>
    <input model={Form.Name} class="input"/>
    <widget.Error for={Form.Name}/>

    <label for={Form.Email}/>
    <input model={Form.Email} class="input"/>
    <widget.Error for={Form.Email}/>

    <button>Create account</button>
  </form>
</main>
```

`<form for>` gets everything a form needs from vuka: the binding of each
`model`, the checks as the visitor goes and on submit, the submit policy and
busy state, CSRF, the error summary and the form's hooks (`Init`). Its
options are attributes the compiler consumes; every other attribute —
`class`, `autocomplete`, `aria-label`, `data-*` — is the element's:

| Attribute | |
|---|---|
| `for={Form}` | the form: a struct embedding `ui.FormState`, a `ui.FormFor[T]`, or a form type embedding one |
| `onSubmit={Save}` | runs once every rule and Clean method passed, with the checked value |
| `onChange={Check}` | runs as fields change, for checks of your own |
| `key={…}` | tells forms of one type apart ([Several Forms on a Page](#several)) |
| `validate="submit"` / `"off"` | checks only on submit, or never ([When Checks Run](#timing)) |
| `submit="abort"` / `"allow"` | what a second submit does while one is on its way (`"prevent"` drops it) |
| `keep` | a FormFor keeps its values after a submit that didn't save it (a search) |
| `confirmLeave` | asks before the visitor leaves with changes not saved |
| `noSummary` | no error summary: focus goes to the first field in error |
| `eager` | runs the form's `Init` on the first render even when the form is hidden |
| `error={msg}` | a message of the form's own, above its fields |

A misspelt option is a compile error with the one it most likely meant
(`<form> has no attribute confirmLeav; did you mean ConfirmLeave?`), and
`for` takes the form itself, never text ([E0186](/errors/E0186)).

The element's `id` is the form's identity — its type's name in kebab case
(`Signup` is `signup`), then `-` and its key when it has one — so a form
never needs one written. A written `id` wins for the element; the controls'
ids keep starting with the identity either way.

`<ui.Form for={Form}>` is the same form with the kit's styles (the
`vk-form` class) and the same attributes. Childless, it renders every field
in order and a submit button, which is the quickest way to a working form:

```html
<ui.Form for={Form} onSubmit={Save} submitLabel="Create account"/>
```

`Save` runs only when every rule and Clean method passed, with `f` the
checked value. What it returns decides what the visitor sees next:

| `Save` returns | |
|---|---|
| `nil` | the form stays, its errors cleared |
| `web.Redirect(url)` | the page moves to `url` |
| `j.Form.Fail(msg)`, a `forms.Error` | the message shows in the form's error summary |
| a `*web.Error` | its message shows in the summary too |
| any other error | an error of the page, as any handler's |

`Form.Valid()`, `Form.Submitting()`, `Form.Touched(&Form.Email)`,
`Form.Changed(&Form.Email)` and `Form.ErrorOf(&Form.Email)` read the form's
state; `Form.SetError(&Form.Email, msg)` gives a field an error from your
own code. A field is named by a pointer to it, so renaming one is a compile
error rather than a silent miss.

A form belongs to a routed page (a shape under `pages/`, or one served with
`web.Live`), or to a live component on one. A component's form is its
own: its parent's render sets the component's other props again, never its
form, so what the visitor typed and the errors they see stay.

A form's values stay on the server and in its fields: a page or component
with a script of its own doesn't hand its form to the script as a prop,
and a field tagged `input:"password"`, `"new-password"`,
`"current-password"` or `"one-time-code"` is never a prop wherever it is,
so what a visitor types is never written into the page's HTML or a live
patch. A script reads a value from its field
(`el.querySelector('[name=email]').value`). See [Scripts](/ui/scripts).

## Writing the Markup {#markup}

Inside `<form for>` (or `<ui.Form for>` with children) the markup is
yours. `model` on a control and `for` on a label name a field; the
compiler checks the name and writes the control's `name`, `id`, `value`,
`type`, `inputmode`, `autocomplete` and ARIA links for you:

```html
<form for={Form} onSubmit={Save}>
  <label for={Form.Email}/>
  <widget.Error for={Form.Email}/>
  <input model={Form.Email} class="input"/>

  <widget.Field for={Form.Password}/>

  <label for={Form.Plan}>Plan</label>
  <select model={Form.Plan}></select>

  <fieldset for={Form.Address}>
    <legend/>
    <label for={Street}/>
    <input model={Street}/>
    <label for={City}/>
    <input model={City}/>
  </fieldset>

  <button>Create account</button>
</form>
```

What each tag does with a field:

| Tag | |
|---|---|
| `<form for={Form}>` | the form, as [above](#rendering) |
| `<label for={Form.Email}/>` | empty or self-closing: the field's label (`@ui.Label`, `label:`, else its name in words) and "(optional)" or "(required)" as [configured](#accessibility); with text, the text. Either way `for` is the control's id |
| `<label><input model={Form.Agree}/> I accept</label>` | a label around its control needs no `for` |
| `<input model={…}>`, `<textarea model>`, `<select model>` | the control, from the field's type and tags (table below); `aria-required` on a required field, safe under `novalidate` |
| `<select model>` with no options | the field's choices, after an empty "Choose…" option (`@ui.Placeholder` changes the text) when the field may be empty or has no value yet; options with a `ui.Option.Group` go under an `<optgroup label>` |
| `<input type="number" model>`, `type="range"` | `min` and `max` from the field's rules |
| `<fieldset for={Form.Address}>` | scopes its children to a struct field: `model={Street}` is `Form.Address.Street`. A reference starting with the form's own name (`Form.Address.City`) is taken as written |
| `<fieldset for={Form.Plan}>` | a field with choices: its radios take the fieldset's legend, the fieldset carries the field's error state and the `aria-describedby` of its hint and error |
| `<legend/>` | empty, in a `<fieldset for>`: the field's label, as an empty `<label for/>` |
| `<button form={Form}>`, `<input form={Form}>` | a control outside the form (a modal's footer, a toolbar) that belongs to it: `form` is the form element's id. A submit button attached so is busy while its submit is on its way, as one inside |
| `<input type="hidden" model>` | a warning ([W0115](/errors/W0115)): the browser can send any value back, so an id or an owner comes from the route or the session (`form:"-"`) and an upload is a `ui.File`; a value the page's own script writes is fine |

While a submit is on its way, the button that sent it has `aria-busy` and
`data-vk-busy` (style it with `[data-vk-busy]`), and the form's other
controls `aria-disabled` — not `disabled`, which would leave them out of the
post.

The form's components fill in what the tags don't:

- `<widget.Field for={Form.Email}/>` is one whole field: label, hint, error and
  control. Its attributes (`label`, `help`, `placeholder`, `class`) override
  the tags.
- `<widget.Error for={Form.Email}/>` is the field's message, rendered only while
  it shows one.
- `<widget.Fieldset legend="…">` groups fields; `<widget.Fieldset for={Form.Address}/>`
  renders a struct field's fields under its label.
- `<widget.Summary/>` puts the error summary somewhere other than the top of the
  form: among the fields, or inside an element of your own. The summary
  appearing or going never moves what was typed into another field.
- `<widget.Choices for={Form.Plan}/>` is a field's radios (or, for a slice, its
  checkboxes), each with its label, from its `<Field>Choices`, for a field
  you lay out yourself; a `<select model>` with no options gets them the
  same way.
- `<widget.Field for={Form.Email} ok/>` shows a tick once the visitor used the
  field and it passes every check. Ticks are off by default.

An app that styles its controls through components of its own writes them
as [widgets](./widgets): a component embedding `widget.Props` takes
`model={Form.Email}`, gives it to its control with `model={Model}`, so the
control gets its name, id, value, type and ARIA links as one written in the
page would, and has its label, hint and error filled by the form:

```html
<!-- components/text_field.vuka.html -->
<script lang="go">
package components

import "github.com/vuka-lang/ui/v2/widget"

// TextField is the app's text field: a label, the error, the control.
type TextField struct {
    widget.Props
}
</script>

<div class="field">
  <label class="field-label" for={Model.ID()}>{Label}</label>
  <widget.Error for={Model}/>
  <input model={Model} class="field-input"/>
</div>
```

```html
<form for={Form} onSubmit={Save}>
  <components.TextField model={Form.Email} label="Email"/>
  <components.TextField model={Form.Name}/>
  <button>Create account</button>
</form>
```

The compiler checks the field as it does on a control, and refuses
`model` on a component whose props have no `Model ui.FieldRef`; a modifier
(`model.lazy`) goes on the component's control. The `label` set on the tag
wins over the field's; what the tag leaves out the form fills.

A misspelt field is a compile error naming the one it most likely meant:
`model={Form.Emial}: Signup has no field Emial; did you mean Email?`
([E0159](/errors/E0159)).

A component laying out its own label, hint and error reads them from its
`Model`: `ui.LabelText(Model)` is the field's label, `ui.HelpText(Model)`
its hint and `ui.ErrorText(Model)` the message of the error it shows
(nothing while it shows none), each as a node of text, beside
`ui.ModelID(Model)`, the control's id.

The control follows the field's Go type:

| Control | Field |
|---|---|
| text, `<textarea>` | `string` |
| text with a numeric keyboard | `int`, `float64`; anything else typed is the error "Enter a number" |
| checkbox | `bool` |
| a checkbox per choice, the same `model` | `[]string`, `[]int` |
| radio group | a scalar with `input:"radio"` and choices |
| `<select>` | a scalar; its options from `<Field>Choices` when the markup has none |
| `<select multiple>` | a slice |
| date | `time.Time`; any `encoding.TextUnmarshaler` takes its text |
| the control of `T`, optional | `Option[T]`: an empty control is `None` |

An `Option` field takes `model` as it is, the control showing the value a
`Some` holds and nothing for `None`. On submit an empty control (or only
whitespace, which is trimmed) is `None`, and any other value is `Some` of
it, held to the field's checks:

```html
<!-- Notes is an Option[string] -->
<ui.Form for={Form} onSubmit={Save}>
  <label for={Form.Notes}>Notes</label>
  <input model={Form.Notes} class="input"/>
</ui.Form>
```

That holds in every kind of form: a `FormFor[Product]` (`Form.Notes` is its
`Form.Value.Notes`), a struct embedding `ui.FormState`, and a
`ui.ModelForm[Product]` through `Form.Value.Notes`. After
`Form.Load(ctx, Product{Notes: Some("fragile")})` the input shows `fragile`;
submitting it empty gives `None`, and `gift wrap` gives `Some("gift wrap")`.

`model` takes modifiers, which change what the server binds, not only the
browser:

- `model.lazy` sends the field's value when it changes, not as the visitor
  types;
- `model.raw` keeps leading and trailing whitespace, which every other text
  field loses (passwords and codes always keep it);
- `model.number` takes only a number in a text field.

### Events on the form tags {#events}

DOM events work on the form tags as on any other (`onClick`,
`onInput.debounce.300ms`, `onKeyDown.enter`, call syntax, `ui.JS`
chains), beside `for`, `model` and `form`. A `<form for>` also has
events of its form's lifecycle:

| Event | Takes | Runs |
|---|---|---|
| `onSubmit` | a method | on submit, once the checks pass |
| `onChange` | a method | as fields change, after the form's own checks |
| `onSuccess` | a `ui.JS` chain | in the browser, once a submit's reply is in place without errors: `onSuccess={ui.JS.Set(open, false)}` |
| `onError` | a `ui.JS` chain | once a submit's reply shows errors (a field's or the summary) |
| `onInvalid` | a `ui.JS` chain | when the [browser's checks](#browser-checks) stop a submit before it is sent |
| `onReset` | a method or a `ui.JS` chain | after the form is reset: a `<button type="reset">` resets it on the server (the loaded values back, its errors cleared), then the method runs, or the chain in the browser |

An event of a control with `model`, or of a `<fieldset for>`
(`<fieldset for={Form.Address} onChange={addressChanged}>`), runs after
the form has taken the field's new value, so its handler reads the
bound field. A button outside the form attached with `form={Form}` runs
its `onClick` before the submit, and `onClick.prevent` stops the submit.

## When Checks Run {#timing}

Every rule runs on every change, so an error that depends on two fields
follows both. What a visitor sees is decided on the server, from which
fields the runtime says they used:

| Moment | What shows |
|---|---|
| typing in a field that shows no error | nothing |
| leaving a field with something in it | that field's error |
| typing in a field that shows an error | the error, updated; gone as soon as it is fixed |
| a fixed-length field (`len=5`) reaching its length | its error |
| typing what can never become valid (letters where a number goes) | its error, at once |
| an empty required field | nothing until the form is submitted |
| submitting | every error, and the summary |

An error from `Clean<Field>` stays until that field changes, one from
`Clean` until any field does. What the visitor typed is kept and flagged,
never thrown away.

`<ui.Form validate="submit">` checks only on submit, for a page asking one
question at a time. `validate="off"` runs no checks at all — no rules, no
`Clean` methods, not even on submit — so `Save` runs with whatever was sent
and must check it itself.

## Errors and Messages {#errors}

Every error is a `forms.Error{Path, Rule, Params, Key}`: its field's path
(`email`, `address.city`), the rule it broke, the rule's values and a
message key. The same path names the field's id, the summary's link to it
and the error in tests.

Messages are plain instructions, saying what to do:

| | |
|---|---|
| `required` | "Enter full name", "Enter your email address", "Select plan" |
| `max=80` | "Name must be 80 characters or less" |
| `email` | "Enter an email address in the correct format, like name@example.com" |
| a number that isn't one | "Enter a number" |

`msg:"rule=…"` replaces one for a field, `forms.Errorf` writes the message
of a Clean method's error, and an app's catalogue changes any message, in
any language: see [Messages and Translations](#messages).

A `msg:` tag holds one message per rule, `rule=message`, separated by
commas. A message may hold commas of its own: a comma ends a message only
when the next rule's `name=` follows it.

```go
type Applicant struct {
    ui.FormState
    Name string `validate:"required,max=80" msg:"required=Enter your name, as on your ID,max=Shorten your name, to 80 letters or fewer"`
}
```

Write `\,` for a comma that a `name=` follows in a message's own text,
and `\\` for a backslash.

After a failed submit, the summary at the top of the form says "There is a
problem" and links each error to its field, in the same words. The browser
moves focus to it, and the page's title starts with "Error: ". An error
that shows when the visitor leaves a field never moves focus.

## Editing Records {#editing}

A form editing a record starts from the record's values and writes the
form's back. `Form.Load(record)` copies the fields the form and the record
share — the same Go name, the same kind of value — and makes them the
values the form compares with; `f.Apply(&record)` writes the form's fields
back, and only the ones it declares:

```go
package account

import "github.com/vuka-lang/ui/v2"

type ProfileForm struct {
    ui.FormState
    Name  string `validate:"required,max=80"`
    Email string `validate:"required,email"`
}
```

```html
<script lang="go">
// pages/users/[id]/edit.vuka.html
package edit

import (
    "context"

    "example.com/app/account"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
)

type Edit struct {
    ui.Live
    ID   int64 // the route's [id]
    Form account.ProfileForm
    user account.User
}

@ui.Mount
func (e *Edit) load(ctx context.Context, users *account.Users) error {
    u, err := users.Get(ctx, e.ID)
    if err != nil {
        return err
    }
    e.user = u
    e.Form.Load(u)
    return nil
}

func (e *Edit) Save(ctx context.Context, f account.ProfileForm, users *account.Users) error {
    u := e.user
    f.Apply(&u) // Name and Email; the record's other fields stay as loaded
    if err := users.Update(ctx, u); err != nil {
        return err
    }
    return web.Redirect("/users")
}
</script>

<ui.Form for={Form} onSubmit={Save} submitLabel="Save changes" confirmLeave/>
```

- A field takes the record's field when both hold the same kind of value:
  text, whole numbers, fractions, true or false, lists of them, times. A
  pointer stands for its value, and `Apply` sets a pointer to nil for an
  empty field. An `int` is never copied into a `string`.
- `Apply` writes only the fields the form rendered, and returns their Go
  names — the columns to save.
- The record's id is the page's, from its URL; the form holds only what the
  visitor may change.

`ui.FormOf[T]("Name", "Email")` is a form over a model's fields without a
struct of its own: the fields named, in that order, with the model's tags.
There is no "every field": a column added to the model later isn't a
submit's to set until the form names it, and a name the model doesn't have
is a compile error ([E0161](/errors/E0161)). Markup names the fields
through `Value`:

```html
<script lang="go">
package edit

import (
    "context"

    "example.com/app/account"
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/web/v2"
)

type Edit struct {
    ui.Live
    ID   int64
    Form *ui.ModelForm[account.User]
}

@ui.Mount
func (e *Edit) load(ctx context.Context, users *account.Users) error {
    u, err := users.Get(ctx, e.ID)
    if err != nil {
        return err
    }
    e.Form = ui.FormOf[account.User]("Name", "Email")
    e.Form.Load(u)
    return nil
}

func (e *Edit) Save(ctx context.Context, f *ui.ModelForm[account.User], users *account.Users) error {
    u, err := users.Get(ctx, e.ID)
    if err != nil {
        return err
    }
    if err := users.Update(ctx, *f.Apply(&u)); err != nil {
        return err
    }
    return web.Redirect("/users")
}
</script>

<ui.Form for={Form} onSubmit={Save}>
  <widget.Field for={Form.Value.Name}/>
  <widget.Field for={Form.Value.Email}/>
  <button>Save changes</button>
</ui.Form>
```

Embedding a database model in a form instead makes every column of it a
field a submit may set; `vuka vet` warns about it ([W0311](/errors/W0311)).

## One Form for Models and Plain Structs: FormFor {#formfor}

`ui.FormFor[T]` is a form of any struct `T`, with no form struct of its
own: its fields are `T`'s, described by `T`'s field attributes — a model's
own (`@orm.Char{Max: 100}`) and the form's (`@ui.Placeholder("…")`) — and
its type decides what is required. Markup names a field through the form,
`Form.Name`, which Go code writes `Form.Value.Name`.

### Forms of Models {#formfor-models}

```vuka
@orm.Meta{Table: "products"} @orm.Entity
type Product struct {
    orm.Base
    ID     int64          @orm.PK
    Name   string         @orm.Char{Max: 100, Unique: true, IgnoreCase: true, Trim: true} @ui.Placeholder("e.g. Desk lamp")
    SKU    string         @orm.Char{Max: 12, Pattern: `^[A-Z0-9-]+$`, Help: "Letters, digits and dashes"}
    Price  int64          @orm.Int{Min: 0}
    Status string         @orm.Char{Choices: []string{"draft", "live"}}
    Notes  Option[string] @orm.LongText
}
```

```html
<script lang="go">
package products

import (
    "context"

    "github.com/vuka-lang/ui/v2"
)

type Products struct {
    ui.Live
    ID   int64 // the route's [id]; 0 for a new product
    Form ui.FormFor[Product]
}

@ui.Mount
func (p *Products) load(ctx context.Context) error {
    if p.ID == 0 {
        return p.Form.Load(ctx, Product{})
    }
    return p.Form.Load(ctx, Product.Objects.Get(ctx, Product.ID.Eq(p.ID))?)
}

func (p *Products) save(ctx context.Context) error {
    return p.Form.Save(ctx)
}
</script>

if Form.IsNew() {
  <h1>Add product</h1>
} else {
  <h1>Edit product</h1>
}
<ui.Form for={Form} onSubmit={save}>
  <widget.Field for={Form.Name}/>
  <widget.Field for={Form.SKU}/>
  <widget.Field for={Form.Price}/>
  <widget.Field for={Form.Status}/>
  <widget.Field for={Form.Notes}/>
</ui.Form>
```

- `Load(ctx, row)` edits that row; `Load(ctx, Product{})` starts a new
  one. The loaded record, its primary key included, stays on the server:
  the key is never a field. `Load` takes the ctx and returns an error for
  a [form type's](#formfor-types) `Init`, which it runs; on a plain
  `FormFor` it returns nil (or the form's declaration error).
- `Save(ctx)` runs the form's checks when no submit did, copies the form's
  fields onto the record, inserts it when it is new and otherwise updates
  **only the form's fields** (`SaveFields`), so a change another request
  made to another column survives. It then reads the row again: a new one
  has its key and what the database computed, and the form is clean. A
  failed check returns `ui.ErrFormInvalid` and shows at its field.
- `IsNew()` reports whether the record is new: none loaded, or its key
  unset.
- `Save` needs `T` to have `Save(ctx)` and `SaveFields(ctx, fields…)`
  methods, as every `orm` model has; on a `FormFor` of another type it is a
  compile error ([E0182](/errors/E0182)).
- A unique column (`@orm.Char{Unique: true}`, `@orm.Unique`) is checked
  against the table as the form is submitted, the record's own row left
  out; `IgnoreCase` folds case for the check. The error shows at the field
  until its value changes.

### Forms of Any Other Struct {#formfor-plain}

```vuka
type Signup struct {
    Email    string         @ui.Email @ui.Label("Email address")
    Password string         @ui.Min(12) @ui.Widget("password")
    Plan     string         @ui.OneOf("free", "pro")
    Referrer Option[string] @ui.Max(40)
    Terms    bool           @ui.Required("Accept the terms to continue")
}

func (s Signup) Clean() error {
    if s.Plan == "pro" && s.Referrer.IsSome() {
        return ui.FieldError(Signup.Referrer, "Referrals are for the free plan")
    }
    return nil
}

type SignupPage struct {
    ui.Live
    Form ui.FormFor[Signup]
}

func (p *SignupPage) submit(ctx context.Context, in Signup, users *Users) error {
    if users.Exists(ctx, in.Email) {
        return ui.FieldError(Signup.Email, "That email is already registered")
    }
    return users.Create(ctx, in)
}
```

- The submit handler takes the checked value as a plain `T` (or `*T`), so
  the same type can be a service's input, and the handler is tested
  without ui.
- `ui.FieldError(Type.Field, msg)` is an error of one field: a field
  reference, so a misspelling is a compile error. Any other error is the
  form's.
- On success the form empties; `<ui.Form keep>` keeps its values (a
  search, filters). A model's form that saved itself keeps the saved row.
- `<Field>Choices(ctx, deps…)`, `Clean` and `Clean<Field>` on `T` are the
  form's, as on a form struct.

### Form Types: a FormFor with Code of Its Own {#formfor-types}

A struct embedding `ui.FormFor[T]` is a **form type**: a form of `T`, as
the `FormFor` is, with methods of its own. It may be declared in a `.go`
file or in the page's `<script lang="go">` beside the page's struct —
like a struct embedding `ui.FormState`, it is data, never the struct the
markup renders.

```html
<script lang="go">
package products

import (
    "context"

    "github.com/vuka-lang/ui/v2"
)

type ProductForm struct {
    ui.FormFor[Product]
}

// Init runs when the form is created (before its first render) and on
// every Load.
func (f *ProductForm) Init(ctx context.Context) error {
    if f.IsNew() {
        f.Status = "draft"
    }
    return nil
}

// Clean checks the form as a whole, as T's Clean would.
func (f *ProductForm) Clean(ctx context.Context) error {
    if f.Status == "live" && f.Notes.IsNone() {
        return ui.FieldError(Product.Notes, "A live product needs notes.")
    }
    return nil
}

type Products struct {
    ui.Live
    ID   int64
    Form ProductForm
}

@ui.Mount
func (p *Products) load(ctx context.Context) error {
    if p.ID != 0 {
        return p.Form.Load(ctx, Product.Objects.Get(ctx, Product.ID.Eq(p.ID))?)
    }
    return nil // a new product: the form's first render runs Init
}

func (p *Products) save(ctx context.Context) error {
    return p.Form.Save(ctx)
}
</script>

<ui.Form for={Form} onSubmit={save}>
  <widget.Field for={Form.Name}/>
  <widget.Field for={Form.Status}/>
  <widget.Field for={Form.Notes}/>
</ui.Form>
```

- **Everything a `FormFor` does, a form type does.** `Load`, `Save` and
  `IsNew` are promoted through the embedding; `Form.Name` is
  `Form.Value.Name` in markup and in Go, inside the form type's own
  methods too (`f.Status` above); `@ui.Fields`, the bound fields, what is
  required by type, the field attributes and [E0182](/errors/E0182) apply
  unchanged.
- **`Init(ctx, deps…) error`** runs when the form is created and on every
  Load: once for each instance of the form, and the page never calls it.
  - A new form runs it **just before its first render**: the
    `<ui.Form for={Form}>` shows what `Init` set on first paint, with no
    `Load` (`IsNew()` is true). The page's `@ui.Mount` runs before it.
  - `Load(ctx, row)` makes a new instance: it runs `Init` there, after the
    row is copied in (so `Init` sees it), and returns its error.
  - `Form.Reset()`, and a plain form emptied by its successful submit, make
    a new instance too: the next render runs `Init` again.
  - A `Save(ctx)` of a form never rendered runs it before the checks.
  - A change or a submit never runs it: what the visitor typed is never
    overwritten.
  - What `Init` sets is the form's starting point: the form is not dirty.
  - An error from `Init` leaves the form uninitialised: the render fails
    with it (the page's error), `Save` and `Load` return it, and the next
    trigger tries again.
  - Its dependencies are injected, as an event handler's are.
- **A form the page renders hidden runs `Init` when the browser first shows
  it.** On a live page, a `<ui.Form for={Form}>` inside an element rendered
  hidden — a closed `ui.Modal`, `<ui.Show when>` or `show={…}` not holding,
  the `hidden` attribute, `display: none`, a `<dialog>` without `open`, a
  popover — renders `data-vk-init` and `aria-busy="true"` instead. When the
  browser first shows it (the modal opens, the tab is chosen) the runtime
  sends one `vkinit` event; the server runs `Init` and renders the form
  again, busy until the reply. A `Load` in the event that shows the form
  (`JS.Set(open, true).Push(Edit)`, `Edit` calling `Load`) runs `Init`
  there, once: the form comes back without the marker, and the runtime
  sends nothing. `<ui.Form for={Form} eager>` runs `Init` at the first
  render, hidden or not. A page that isn't live runs it at the first
  render.
- **`Clean`, `Clean<Field>` and `<Field>Choices`** declared on the form type
  are the form's, exactly as on `T` or on a form struct (see
  [Declaring a Form](#declaring)): dependencies injected, `forms.Live(ctx)`
  for checks run while the visitor types, `ui.FieldError` and
  `forms.Errorf` for their errors. `Clean` runs after every field's checks,
  before the submit handler or `Save`'s write, and a failed check stops
  both. A method of these that `T` declares too is a compile error
  ([E0185](/errors/E0185)): keep one.
- **Fields the form type declares itself are not form fields.** They are
  the page's own state of the form (a mode, a lookup); one named as a field
  of `T` is a compile error ([E0185](/errors/E0185)), since `Form.Name`
  names `T`'s.
- An `Init` that doesn't take a ctx first and return an error, or a Clean
  method that doesn't return an error, is a compile error
  ([E0184](/errors/E0184)).
- Embed the `FormFor` by value, and use it through its form type: a copy of
  the inner `FormFor` taken out of it is not a form.

Go code using a form type that no page holds — a test, a job — calls
`ui.Attach(&f)` first, which makes the struct the form its `FormFor`
belongs to: `ui.Attach(&f).Load(ctx, row)`. In a `.vuka` file the compiler
writes it at each `Load` and `Save` of a form type.

### What Is Required {#formfor-required}

A `FormFor`'s field is required by its type: a plain field must be filled
(a number's input may not be left empty, `0` is a value), an `Option[T]`
may be left empty and is `None` (with `<widget.Field>` or a raw `<input
model={Form.Notes}/>` alike: see [the controls](#markup)), and so may a
pointer and a slice. A
`bool` unticked is a valid `false`; `@ui.Required` on a bool means it must
be ticked. `@orm.Null` makes a plain field optional.

### The Fields a Form Edits {#formfor-fields}

A `FormFor` has no field list: its fields are the ones its markup binds.
The compiler reads every `for={…}` and `model={…}` inside the
`<ui.Form for={Form}>` and writes them into the form, and a submit sets
only those, so a field the page doesn't show can never be a visitor's to
set, and a column added to the model later stays off the form until the
markup uses it. A childless `<ui.Form for={Form}/>` renders, and edits,
every field but the key and the times the database sets.

Markup the compiler can't see into — the form handed to a component of the
app's — names the fields on the form's field instead:

```vuka
type Products struct {
    ui.Live
    Form ui.FormFor[Product] @ui.Fields(Product.Name, Product.Price)
}
```

### Field Attributes {#field-attributes}

A model holds a field's data rules; a form holds how the field is shown
and what its messages say. Both are the field's attributes, which a form
reads (`field.Describer`) whatever library declares them.

**`@ui.Spec{…}`** gives a field's details in one attribute, a struct
literal like `@orm.Char{…}`:

```vuka
type Product struct {
    orm.Base
    ID   int64          @orm.PK
    Name Option[string] @orm.Char{Max: 100, Unique: true, IgnoreCase: true, Trim: true} @ui.Spec{
        Placeholder: "e.g. Blue mug",
        Msg:         ui.Msgs{"unique": "A product with this name already exists."},
    }
}
```

| `ui.Spec` member | |
|---|---|
| `Label`, `Help`, `Placeholder` | the label (the field's name in words otherwise), the hint, an example value |
| `Widget` | the control: `"textarea"`, `"radio"`, `"password"`, `"email"`, `"date"`, … |
| `Autocomplete` | the autocomplete token |
| `Required: "…"` | the message of a required field; it also makes an optional one required |
| `Optional: true` | makes a field its type or model requires optional |
| `Min`, `Max` | a text's length, a number's value: may only tighten the model's |
| `Choices` | the values it takes: may only narrow the model's, whose labels stay |
| `Msg: ui.Msgs{rule: "…"}` | a rule's own message: `required`, `min`, `max`, `len`, `pattern`, `oneof`, `choice`, `email`, `unique`, `number`, `whole`, `format`, `digits`, `file` |
| `Disabled`, `ReadOnly` | the value shown, not edited: a submit never sets it |

A key the struct hasn't is Go's own compile error; a `Msg` key that is no
rule (with the one it most likely meant), an empty `Required`, `Required`
beside `Optional`, a `Max` above or a `Min` below the model's, and
`Choices` the model hasn't are [E0189](/errors/E0189).

The single attributes are shorthand for one member each; one setting given
by a `Spec` and a single attribute, or by a tag and an attribute, is
[E0183](/errors/E0183):

| ui attribute | `ui.Spec` member | Tag |
|---|---|---|
| `@ui.Label("…")` | `Label` | `label:` |
| `@ui.Help("…")` | `Help` | `help:` |
| `@ui.Placeholder("…")` | `Placeholder` | `placeholder:` |
| `@ui.Widget("radio")` | `Widget` | `input:` |
| `@ui.Autocomplete("…")` | `Autocomplete` | `autocomplete:` |
| `@ui.Msg("max", "…")` | `Msg` | `msg:` |
| `@ui.Required`, `@ui.Required("…")` | `Required` | `validate:"required"` |
| `@ui.Min(n)`, `@ui.Max(n)` | `Min`, `Max` | `validate:"min=n"` |
| `@ui.OneOf("a", "b")` | `Choices` | `validate:"oneof=a b"` |
| `@ui.Email`, `@ui.Email("…")` | | `validate:"email"` |
| `@ui.Pattern("…")` | | |
| `@ui.Fields(T.A, T.B)` | | on a `FormFor` field: the fields it edits |

#### On the Form: `ui.Meta` {#field-meta}

A form type (a struct embedding `ui.FormFor[T]`) says how *its* form shows
T's fields: a member of type `ui.Meta` describes T's field of the same
name, with the attributes written after it. Different forms of one model
word the same field their own way, and the model keeps only its data
rules:

```vuka
type ProductForm struct {
    ui.FormFor[Product]

    Name ui.Meta @ui.Spec{
        Required:    "Name is required.",
        Placeholder: "e.g. Blue mug",
        Msg:         ui.Msgs{"unique": "A product with this name already exists."},
    }
    Notes ui.Meta @ui.Widget("textarea")
}
```

`ui.Meta` holds nothing: it is not the form's field, `Form.Name` is still
`Form.Value.Name`, and T's field must exist — a `ui.Meta` naming no field
of T, or in a struct that is no form type, is [E0190](/errors/E0190).

#### What the Model Implies {#field-orm}

A form takes the rest from the model's `@orm` attributes. What applies,
first to last, a later one's setting winning: what `@orm` implies, the
model's `@ui` attributes, then the form's (`ui.Meta`) — whatever order they
are written in.

| orm attribute or type | In a form |
|---|---|
| a plain field type | required |
| `Option[T]`, `Null` (`@orm.Null`, `Rel{Null: true}`) | optional; empty is `None` |
| `Max`, `Min` (`Char`, `Slug`, `Int`) | length and range rules, messages from the label; a numeric text's `maxlength` |
| `Unique`, `IgnoreCase` (`Char`, `Slug`, `Int`, `DateField`, `Datetime`, `@orm.Unique`) | a database check on submit (`field.Checker`), the record's own row left out |
| `Choices` | a select's options (radios with `Widget: "radio"`), the `oneof` rule |
| `Pattern` | a pattern rule, on the server |
| `Trim`, `Lower` | the text normalized as it is bound, before the checks |
| `Label`, `Help` | the label and the hint |
| `Default`, `@orm.DefaultTo(v)` | a new record's starting value, set before `Init` (not a change) |
| `@orm.LongText` | a textarea, no length limit |
| `@orm.PK`, `AutoNow`, `AutoNowAdd`, `DefaultNow` | never form fields |
| `Column`, `Index`, `Collation`, `Comment` | nothing |
| an attribute with `Validate(v) error` | a check of the field |

The default messages are built from the field's label — "Name must be 100
characters or less", "Enter name", "Name is already taken" — so most
fields need no `Msg` at all; an app's own wording or translations replace
them through the [message catalogue](#messages).

## Choices {#choices}

A field with a `<Field>Choices` method is a choice: a `<select>`, or radios
with `input:"radio"`, or a checkbox per choice for a slice. The method's
dependencies are injected, and it reads the form's current values, so one
field's choices can follow another's:

```go
package places

import (
    "context"

    "github.com/vuka-lang/ui/v2"
)

type Address struct {
    ui.FormState
    Country string `validate:"required"`
    City    string `validate:"required" depends:"Country"`
}

func (f *Address) CountryChoices(ctx context.Context, geo *Geo) ([]ui.Option, error) {
    return geo.Countries(ctx)
}

func (f *Address) CityChoices(ctx context.Context, geo *Geo) ([]ui.Option, error) {
    return geo.Cities(ctx, f.Country)
}
```

- Choosing a country updates the city's choices, and a city that isn't
  among them any more is cleared. On a live page the form renders again; on
  [one that isn't](#http) the form's check carries the new choices, which
  the runtime puts in the city's select. An empty required city shows its
  error on submit, not before.
- A submitted value that isn't among the field's choices is an error
  ("Select city from the list"), so a post can't choose what the form never
  offered.
- The choices follow whatever the method reads, with no tag to say so: a
  form's check carries the choices of every field with a `Choices` method.
  `depends:"Country"` is for browsers without script: that field gets an
  Update button, inside `<noscript>`: it sends the form, which renders
  again with the new choices, and saves nothing (a plain post answers 200).
- A `<Field>Choices` method may return an orm query instead of a list
  (`Category.Objects.Filter(…)`): its rows are the options, labelled by
  their `String()`, and a submitted key is checked against the query, one
  row looked up, the table never listed. An `orm.FK[T]` field is a select
  of every row of T without one; see [Related fields](./widgets#related).
- A field's `Choices` method runs once per change, however often the
  render reads it. `vuka vet` warns about a `depends:` naming no field
  ([W0313](/errors/W0313)).

A field's control is a [widget](./widgets): `<widget.Radios
model={Form.Plan}/>` in the markup, `@ui.Spec{Widget: widget.Radios{}}` on
the field, or `widget.Use[T]` for every field of a type.

## Formsets: Many Rows in One Form {#formsets}

A form editing many rows of another type at once — an order's lines, a
product's variants, a user's addresses — holds them as a `ui.Rows[T]`
field of its form type. Each row is a form of `T`: its fields, rules,
`Clean` and `Clean<Field>` methods and errors are its own.

```vuka
type OrderLine struct {
	orm.Model[OrderLine]
	ID       int64
	Order    orm.FK[Order]
	Product  orm.FK[Product]
	Quantity int @ui.Spec{Min: 1}
}

type OrderForm struct {
	ui.FormFor[Order]
	Lines ui.Rows[OrderLine] @ui.Spec{Min: 1, Max: 20}
}
```

```html
<form for={Form} onSubmit={save}>
  <widget.Field for={Form.Customer}/>
  for _, r := range Form.Lines.Each() {
    <widget.Related model={r.Product}/>
    <widget.Number model={r.Quantity}/>
    <widget.Error for={r.Quantity}/>
    <button type="button" onClick={Form.Lines.Remove(r)}>Remove</button>
  }
  <button type="button" onClick={Form.Lines.Add()}>Add line</button>
</form>
```

- `Each()` is the rows, each a `*ui.FormFor[T]`: markup names a row's field
  through it (`model={r.Quantity}`), and every widget works inside a row. A
  row's controls are named `lines.<key>.quantity`, so the rows never mix.
- The rows bind, check and submit with the form: a row's error shows at its
  field and in the form's summary, the form's `Clean` sees every row
  (`Form.Lines.Values()`), and the submit handler runs only when every row
  passes.
- `Add()` and `Remove(r)` are events; what was typed in the other rows
  stays. A new row starts from `T`'s zero value and its fields' defaults,
  then `T`'s `Init(ctx) error` when it has one.
- `@ui.Spec{Min, Max}` on the field bounds the rows: a submit with too few
  or too many shows "Lines must have at least 1", and `Add` beyond `Max`
  adds none.
- Rows of a model with an `orm.FK` back to the form's model are the
  record's related rows: the form's `Load` loads them, and its `Save`
  writes the record, the rows removed, changed and added (their back
  reference set) in one transaction. The back reference is never a row's
  field.
- Rows of any other struct work the same without saving: a page sets them
  with `Form.Lines.Load(ctx, rows...)` and reads `Form.Lines.Values()`.
  Go code adds and removes with `Append(ctx)` and `Delete(row)`.

Formsets need a live page: a form checked over HTTP without a connection
keeps no rows between requests.

## Files {#files}

A `ui.File` field takes a file. Its tags say what it accepts and how large
it may be:

```go
package profiles

import (
    "context"
    "io"

    "github.com/vuka-lang/ui/v2"
)

type Photo struct {
    ui.FormState
    Caption string
    Avatar  ui.File `accept:"image/*" max:"5MB" validate:"required"`
}

type Avatars interface {
    Put(ctx context.Context, name, contentType string, r io.Reader) error
}

func SavePhoto(ctx context.Context, f Photo, avatars Avatars) error {
    r, err := f.Avatar.Open()
    if err != nil {
        return err
    }
    defer r.Close()
    return avatars.Put(ctx, f.Avatar.Name(), f.Avatar.ContentType(), r)
}
```

- **The file goes up as soon as it is chosen,** over HTTP with a progress
  bar, before the form is submitted. Its size is checked as it streams: a
  file larger than `max:` stops at the limit and the field shows "Avatar
  must be smaller than 5 MB". A file `accept:` doesn't list shows "Avatar
  must be an image".
- **The form carries a reference, not the file.** Checks as the visitor
  types never send it again, and a submit that fails on another field keeps
  it: the field shows "photo.jpg ✓ Change". A submit sent while a file is
  still going up waits for it.
- **The submit handler reads the file** (`<ui.Form for={Photo}
  onSubmit={SavePhoto}/>`) with `Open`, and `Name`, `Size`,
  `ContentType` and `Empty` describe it. `Name` is what the visitor's device
  called it: show it, never use it as a path. `ContentType` is the type the
  file's first bytes show, else the one the browser declared.
- **`accept:`** takes what the HTML attribute does (`image/*`, `.pdf`,
  `application/pdf`), matched against the name, the declared type and the
  sniffed one. It guides the visitor; check what the handler stores. A field
  without `max:` takes up to `ui.DefaultFileMax` (10 MB).
- **Without script** the form posts as multipart: the first post stores the
  file, and a failed one renders it kept by reference, as above.

Files wait in the app's file store until the form is submitted, for
`ui.FileTTL` (24 hours): by default a directory of the machine. An app with
several replicas gives them one they share, `web.FileStore(s)`, else a
submit reaching another replica than the upload did asks the visitor to
choose the file again ("Select avatar again: the file sent before is no
longer kept"). `ui.DirFileStore(dir)` keeps them in `dir`; a `ui.FileStore`
is three methods, `Put`, `Open` and `Delete`, for any other storage.

## Browser Checks {#browser-checks}

Forms are `novalidate`: the server's messages are the ones a visitor reads.
A control carries a constraint attribute only where the browser decides
exactly as the server does:

| Attribute | Written |
|---|---|
| `minlength`, `maxlength` | on a field of digits (`input:"numeric"`), whose characters count the same in the browser (UTF-16 units) and in Go (characters) |
| `min`, `max` | on a `type="number"` input |
| `required` | with `ui.Config{RequiredAttr: true}`; the browser takes spaces for a value, where the server trims them |
| `pattern` | never: the browser's regular expressions aren't Go's |

`vukavet -forms.attrs` notes each attribute a field's control leaves out,
and why ([W0312](/errors/W0312)).

`ui.Configure(ui.Config{BrowserChecks: true})` adds a check before a submit
sent without a live connection — the socket down, or a page without live
parts. Each control carries the rules the browser can decide as the server
does (`required`, `len`, `min`, `max`, `email`, `oneof`), in the server's
order, with the server's messages; a submit breaking one shows the errors
and the summary at once, as the server's 422 would, and isn't sent. A value
the browser can't judge the same way, such as letters in a field of digits,
is left to the server, which checks every submit that goes. It's off by
default because the messages then ride in the page's markup.

## Messages and Translations {#messages}

Every message comes from a key: a rule's (`validate.required`,
`validate.max.string`), a key your code names in a `msg:` tag, `SetError`,
`Fail` or `forms.Errorf`, or the form components' own words
(`form.problem` for "There is a problem", `form.optional`, `form.save`).
`forms.DefaultMessages()` lists the default English wording.

An app's catalogue overrides any of them, and translates them by the
request's language:

```go
package app

import (
    "github.com/vuka-lang/ui/v2"
    "github.com/vuka-lang/ui/v2/forms"
)

func configureForms() {
    ui.Configure(ui.Config{Messages: forms.Translations{
        "en": forms.Messages{
            "signup.email.taken": "That email address is already registered",
        },
        "sw": forms.Messages{
            "validate.required":   "Weka {label}",
            "validate.max.string": "{Label} isizidi herufi {n}",
            "form.problem":        "Kuna tatizo",
            "form.optional":       "(si lazima)",
            "signup.email.taken":  "Barua pepe hii imeshasajiliwa",
            "Full name":           "Jina kamili",
        },
    }})
}
```

- The language is the request's `Accept-Language`, the first it lists that
  the catalogue has (`pt-BR` falling back to `pt`), else English. A
  middleware choosing another — a signed-in visitor's setting — puts it in
  the request's context with `forms.WithLocale` first.
- A label, hint, legend or placeholder is translated by its text:
  `"Full name": "Jina kamili"`. Messages put the translated label in
  `{label}` (in a sentence) and `{Label}` (starting one).
- `{n, plural, one {# character} other {# characters}}` chooses its words
  by the number's plural category in the message's language — one and
  other for English, one, few and many for Russian or Polish, one form for
  Japanese — `#` being the number; `=0 {…}` chooses an exact value.
  `forms.SetPlural` adds a language's rule.
- `forms.Messages{…}` alone is one language's catalogue; a request's own,
  put in its context with `forms.WithCatalogue`, comes before the app's.

## State {#state}

A form knows how it stands against the values it was loaded with:

| | |
|---|---|
| `Form.Dirty()` | some field differs from the values loaded (`Load`), or first rendered |
| `Form.Changed(&Form.Email)` | that field does |
| `Form.Touched(&Form.Email)` | the visitor used it |
| `Form.Submitting()` | its submit handler is running |
| `Form.Saved()` | the last submit succeeded, in the last 2 seconds |
| `Form.Reset()` | puts the loaded values back and clears the errors |

A successful submit makes its values the ones the form compares with, so
it isn't dirty after it. `Load` and `Reset` make every field take the
server's value, the one being typed in too; otherwise what the visitor
typed stands, and a focused field is never overwritten.

```html
<ui.Form for={Form} onSubmit={Save}>
  <widget.Field for={Form.Name}/>
  <widget.Field for={Form.Email} ok/>
  <button>Save</button>
  if Form.Dirty() {
    <button type="button" onClick={Discard}>Discard changes</button>
  }
  if Form.Saved() {
    <p role="status">Saved</p>
  }
</ui.Form>
```

`Discard` is a method calling `e.Form.Reset()`. On a live page "Saved" goes
by itself once the 2 seconds are up.

## Unsaved Changes {#unsaved}

`<ui.Form confirmLeave>` asks before the visitor leaves the page while the
form has changes not saved: values differing from the ones it was loaded
with, or typed and not sent yet. Closing the tab or reloading shows the
browser's question; a link or a navigation on a live page asks "You have
changes that are not saved. Leave this page?" (`form.leave`, translatable).
Submitting the form leaves without asking. Without script there is no
question.

## Several Forms on a Page {#several}

Each form is known by its type and an optional `key`, so several on one
page keep their submits, errors and ids apart:

```html
<ui.Form for={Profile} onSubmit={SaveProfile}/>
<ui.Form for={Password} onSubmit={ChangePassword}/>
```

Two forms of one type need a key each: `<form for={Billing}
key="billing">` and `<form for={Shipping} key="shipping">`, or `key={o.ID}`
in a list. Without keys they would share their ids, so the page's render
fails, saying to key them.

## Accessibility {#accessibility}

Every field group is the label, then the hint, then the error, then the
control:

```html
<div class="vk-field vk-field--invalid" data-invalid>
  <label class="vk-field__label" for="signup-email">Email</label>
  <div class="vk-field__help" id="signup-email-hint">We'll send a confirmation link</div>
  <p class="vk-field__error" id="signup-email-error"><span class="vk-sr-only">Error:</span> Enter an email address in the correct format, like name@example.com</p>
  <input class="vk-input vk-input--invalid" name="email" id="signup-email" type="email" value="pa@"
         autocomplete="email" aria-describedby="signup-email-hint signup-email-error" aria-invalid="true">
</div>
```

- The control's `aria-describedby` names its hint and error; `aria-invalid`
  is set only while the error shows. Field errors aren't live regions.
- `type`, `inputmode` and `autocomplete` follow the field's type, name and
  tags, so phones show the right keyboard and browsers fill in what they
  know.
- Password fields have a show/hide button, and pasting into any field
  works.
- Optional fields say "(optional)". `ui.Configure(ui.Config{MarkRequired:
  true})` marks required fields "(required)" instead.
- Forms are `novalidate`: the browser's own messages never show, and the
  server's are the ones read. Attributes the browser would check differently
  from the server aren't written ([Browser Checks](#browser-checks));
  `ui.Config{RequiredAttr: true}` adds `required` to required fields.

## Forms on Pages That Aren't Live {#http}

A page needs no `ui.Live` for its form to check as the visitor types. On a
page whose only live part is a struct form — a sign-in page, a contact
form — the form makes no connection: the runtime posts it to its page
over HTTP, at the same moments a live page sends it, and puts what the
page answers in place.

The handler's name doesn't matter here: `onSubmit={Save}` and `onSubmit={save}`
both post over HTTP unless the page embeds `ui.Live`. A handler can't keep
changes to the page's fields, because each request gets a fresh copy of the
page. To keep them, embed `ui.Live`.

```go
// account/credentials.go
package account

import "github.com/vuka-lang/ui/v2"

type Credentials struct {
    ui.FormState
    Email    string `validate:"required,email"`
    Password string `input:"current-password" validate:"required"`
}
```

```html
<script lang="go">
package pages

import (
    "context"
    "errors"

    "example.com/app/account"
    "github.com/vuka-lang/web/v2"
    "github.com/vuka-lang/web/v2/auth"
)

type SignIn struct {
    Form account.Credentials
}

func (p *SignIn) save(ctx context.Context, f account.Credentials) error {
    if _, err := auth.Login(ctx, f.Email, f.Password); err != nil {
        if errors.Is(err, auth.ErrInvalidLogin) {
            return p.Form.Fail("Wrong email or password")
        }
        return err
    }
    return web.Redirect("/account")
}
</script>

<main>
  <h1>Sign in</h1>
  <ui.Form for={Form} onSubmit={save} submitLabel="Sign in"/>
</main>
```

- **Checks.** Leaving a field with something in it, typing in a field
  that shows an error, or picking a choice sends the form's values with a
  check flag. The page runs the rules and `Clean` methods with
  `forms.Live(ctx)` true, as a live page does, and answers with the form's
  errors — never `save`.
- **Errors in place.** The runtime adds, changes or removes each field's
  error, its `aria-invalid` and `aria-describedby`, and the summary, in the
  summary's place (the form's top, or where a `<widget.Summary/>` is). The
  fields themselves are never re-rendered, so what was typed, the focused
  field and the caret stay as they are.
- **Submit.** The form is sent as a plain post is. When it fails, its
  errors and the summary show and the title starts "Error: "; when `save`
  succeeds the page moves to where it sent the visitor, as a link does.
  The reply is a real HTTP response, so `save` can set cookies:
  `auth.Login` and `auth.SignIn` work from it.
- **No state** is kept on the server between requests: each one carries
  the form's values, the fields the visitor used, the fields it rendered
  (signed), its submit key and CSRF token, as a post without script does.

- **Dependent choices.** A check answers the choices of the fields that
  depend on others (`depends:`) too, computed from the values sent, and
  the runtime puts them in those fields' selects: the options replaced
  (the placeholder kept), the value kept while it is still offered, else
  cleared. So [choices](#choices) following another field work the same
  with or without a connection. For a select outside a struct form,
  [`ui.JS.Push`](/ui/js#results) with `.Into` (or its shorthand
  [`ui.JS.Options`](/ui/js#options)) loads its options from a method of the
  page.

A page with anything else live — a live component, an `onClick`
handler — is live as a whole, and its form goes over its connection.

## Forms in Stream Rows {#stream-rows}

A [stream](/web/streams)'s rows are forgotten by the server once sent, and
so is a form in one: `<ui.Form for={renameOf(t)} onSubmit={ui.JS.Push(Rename,
t.ID)}/>` builds the form from the row's record each render, and its submit
pushes a method of the stream's owner whose last parameter is the form. The
submit runs the form's checks on what was sent; its errors come back as the
same report a form on a page that isn't live gets, put in place on that
row's form without rendering the row. A row's form checks only as it is
submitted. See [Forms in Rows](/web/streams#forms).

## Without JavaScript {#no-js}

A page with a form works without script. The form posts to the page
itself; the page renders as for a visit, the form's submit runs as over a
connection, and the answer is:

- **422** with the page, the fields keeping what was typed, their errors
  and the summary showing, and the title starting "Error: ";
- **303** to where `Save` sent the visitor, or back to the page.

A post carries the form's identity, the fields it rendered (signed, so a
post can't claim others), a key for the submit and a CSRF token, all as
hidden fields the form writes. An unticked checkbox reads as false because
the form rendered it.

## Submits, Retries and CSRF {#submit}

While a submit is on its way the form is `aria-busy`, its button showing
the busy state while staying enabled and clickable, and a second submit is
dropped by the runtime.
`<ui.Form submit="abort">` sends the second and ignores the first's reply;
`submit="allow"` sends every one. A change waiting to be sent goes before
the submit, and a reply to a change can't arrive after the submit's.

Each render of a form carries an idempotency key. Once a submit with it
starts, its outcome is kept, and the same submit again — the back button,
a retry after a lost reply — gets that outcome instead of running `Save`
twice. A submit that didn't pass its checks doesn't use its key up, so the
visitor can fix the form and send it. Keys are kept in memory for a day;
`web.FormStore(s)` keeps them where every replica of an app sees them.

Cross-site request forgery is refused by every app: a request a browser
sends from another site's page answers 403. `web.CSRFBypass("POST
/webhooks/")` lets one through by route, for a webhook or a sign-in
callback. A form's own CSRF token covers the browsers and clients that
don't say where a request comes from: an unsafe request carrying a session
cookie, with neither `Sec-Fetch-Site` nor `Origin`, needs it, as the form's
`_vk_csrf` field or an `X-CSRF-Token` header. The live connection checks
the page's origin.

## Testing Forms {#testing}

`viewtest.Mount` opens a page and drives its form as a visitor would:

<!-- go:context
type SignupPage struct {
    ui.Live
    Form Signup
}

func (p *SignupPage) Render() ui.Node { return ui.Form{For: ui.For(&p.Form)} }

var app = web.New()
-->

```go
package account_test

import (
    "testing"

    "github.com/vuka-lang/web/v2/viewtest"
)

func TestSignup(t *testing.T) {
    p := viewtest.Mount[*SignupPage](t, app, "/join")
    p.Fill(&p.Page.Form.Email, "pa@").Blur(&p.Page.Form.Email)
    p.Expect(&p.Page.Form.Email).Error("validate.email")
    p.Fill(&p.Page.Form.Email, "pa@example.com").Submit()
    p.Expect(&p.Page.Form.Name).Error("Enter full name")
}
```

A field is named by a pointer into `p.Page`, a value of the page's type, or
by its path (`"email"`); filling one the form doesn't render fails the test.
`Error` takes the message's key or its text. The page runs over its
connection; `viewtest.Plain()` posts the form as a browser without script
does, and `p.Status()` is the post's 422 or 303.

A form whose `Save` signs in (`auth.Login`, `auth.SignIn`) is tested
through the app with `authtest.As`: the identity it gives a request keeps
the request's response, so the sign-in sets its session cookie there, as it
does for a visitor. `authtest.As(nil)` posts as an anonymous visitor; the
response's `Set-Cookie` holds the new session.
