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

A struct embedding `ui.FormState` is a form. Its exported fields are the
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

Each field's tags say how it is checked and shown:

| Tag | |
|---|---|
| `validate:` | the rules: `required`, `min=n`, `max=n`, `len=n`, `oneof=a b c`, `email`, `omitempty` |
| `label:` | the label; by default the field's name in words (`FirstName` is "First name") |
| `help:` | a hint shown between the label and the control |
| `input:` | the control: `textarea`, `radio`, `select`, `password`, `new-password`, `one-time-code`, `numeric`, `tel`, `date`, … |
| `autocomplete:` | the `autocomplete` value, when the one inferred from the name and type isn't right |
| `placeholder:` | an example value; never a label |
| `msg:` | a rule's message: `msg:"required=Accept the terms to continue"` |
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

A page holds the form as an exported field and renders it with
`<ui.Form for>`. With no children, the form renders every field in order,
then a submit button:

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
  <ui.Form for={Form} onSubmit={Save} submitLabel="Create account"/>
</main>
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
`web.Live`), or to a stateful component on one. A component's form is its
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

To lay the form out yourself, give `<ui.Form>` children. `model` on a
control and `for` on a label name a field; the compiler checks the name and
writes the control's `name`, `id`, `value`, `type`, `inputmode`,
`autocomplete` and ARIA links for you:

```html
<ui.Form for={Form} onSubmit={Save}>
  <label for={Form.Email}>Email</label>
  <ui.Error for={Form.Email}/>
  <input model={Form.Email} class="input"/>

  <ui.Field for={Form.Password}/>

  <label for={Form.Plan}>Plan</label>
  <select model={Form.Plan}></select>

  <button>Create account</button>
</ui.Form>
```

- `<ui.Field for={Form.Email}/>` is one whole field: label, hint, error and
  control. Its attributes (`label`, `help`, `placeholder`, `class`) override
  the tags.
- `<ui.Error for={Form.Email}/>` is the field's message, rendered only while
  it shows one.
- `<ui.Fieldset legend="…">` groups fields; `<ui.Fieldset for={Form.Address}/>`
  renders a struct field's fields under its label.
- `<ui.Summary/>` puts the error summary somewhere other than the top of the
  form: among the fields, or inside an element of your own. The summary
  appearing or going never moves what was typed into another field.
- `<ui.Choices for={Form.Plan}/>` is a field's radios (or, for a slice, its
  checkboxes), each with its label, from its `<Field>Choices`, for a field
  you lay out yourself; a `<select model>` with no options gets them the
  same way.
- `<ui.Field for={Form.Email} ok/>` shows a tick once the visitor used the
  field and it passes every check. Ticks are off by default.

A misspelt field is a compile error naming the one it most likely meant:
`model={Form.Emial}: Signup has no field Emial; did you mean Email?`
([E0159](/errors/E0159)).

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

`model` takes modifiers, which change what the server binds, not only the
browser:

- `model.lazy` sends the field's value when it changes, not as the visitor
  types;
- `model.raw` keeps leading and trailing whitespace, which every other text
  field loses (passwords and codes always keep it);
- `model.number` takes only a number in a text field.

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
  <ui.Field for={Form.Value.Name}/>
  <ui.Field for={Form.Value.Email}/>
  <button>Save changes</button>
</ui.Form>
```

Embedding a database model in a form instead makes every column of it a
field a submit may set; `vuka vet` warns about it ([W0311](/errors/W0311)).

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

- On a live page, choosing a country renders the form again: the city's
  choices follow, and a city that isn't among them any more is cleared. An
  empty required city shows its error on submit, not before.
- A submitted value that isn't among the field's choices is an error
  ("Select city from the list"), so a post can't choose what the form never
  offered.
- `depends:"Country"` names the field the choices follow. Without script,
  that field gets an Update button, inside `<noscript>`: it sends the form,
  which renders again with the new choices, and saves nothing (a plain post
  answers 200).
- A field's `Choices` method runs once per change, however often the
  render reads it. `vuka vet` warns about a `depends:` naming no field
  ([W0313](/errors/W0313)).

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
  <ui.Field for={Form.Name}/>
  <ui.Field for={Form.Email} ok/>
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

Two forms of one type need a key each: `<ui.Form for={Billing}
key="billing"/>` and `<ui.Form for={Shipping} key="shipping"/>`.

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
