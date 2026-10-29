# Enums

[[toc]]

## Introduction

In Go, a set of named values is a defined type, a `const` block with `iota`,
and the methods around it — `String`, parsing, JSON, the database — written by
hand.

Vuka adds `enum`: the variants, and everything around them, from one line. You
use one for a fixed set of states or kinds, either plain named values or
variants that carry their own fields.

## Declaring an Enum

This sample declares a numbered enum and a string enum, then uses them:

```vuka
// Status is where an order is.
enum Status { Draft, Sent, Paid }

// Role is what a member of staff may do.
enum Role string {
	Admin = "admin"
	Clerk = "clerk"
}

s := Status.Sent
fmt.Println(s)                 // Sent
fmt.Println(Status.Values())   // [Draft Sent Paid]
r := Role.Parse("clerk")?      // Role.Clerk
```

Variants are reached through the enum's name, `Status.Draft`, like
[statics](/features/statics), and complete in the editor after `Status.`.

## Value Enums

`enum Name { … }` numbers its variants from 0 in the order they are written.
A type after the name — `string`, or an integer type such as `uint8` — makes it
an enum of that type; then every variant has a value, or none. Here are a
`uint8` enum with values and a string enum without:

```vuka
enum Priority uint8 {
	Low  = 1
	High = 5
}

enum Visibility string { Public, Private }   // "Public", "Private"
```

A string enum's variants without values are their own names. Variants are
separated by commas or new lines, start with a capital letter, and may have doc
comments.

### Generated Methods

Every value enum gets:

| | |
|---|---|
| `Status.Values()` | every variant, in order: `[]Status` |
| `Status.Parse(text)` | `Result[Status]` from a variant's name, or a string enum's value |
| `s.String()` | the variant's name; a string enum's value |
| `s.Valid()` | whether `s` is one of the variants |
| `MarshalText`, `UnmarshalText` | JSON and other text formats use the `String` form; an unknown one is an error |
| `Scan`, `Value` | `database/sql`: stored as the number, or the string enum's value |
### Enums in JSON and the Database

So an enum field needs nothing more to be read from JSON, written to JSON,
stored by the [ORM](/orm/models) or scanned from a row:

```vuka
type Order struct {
	ID     int64
	Status Status
}

json.Marshal(Order{ID: 7, Status: Status.Paid})   // {"ID":7,"Status":"Paid"}
```

`Scan` also accepts the variant's name from a text column. A value that isn't
a variant — `Status(9)`, or `""` in a string enum without an empty variant — is
an error to marshal or store.

> [!NOTE]
> Give a string enum an empty variant (`None = ""`) for a field that may be
> unset.

## Enums With Data

Variants that carry fields make an enum whose values are different shapes:

```vuka
// Shape is a parcel's footprint.
enum Shape {
	Circle{Radius float64}
	Rect{W, H float64}
	Unknown
}

s := Shape.Circle{Radius: 2}
var shapes = []Shape{s, Shape.Rect{W: 1, H: 3}, Shape.Unknown}
```

A variant with fields is built like a struct, `Shape.Circle{Radius: 2}`; one
without fields is a value, `Shape.Unknown`. `Shape.Circle` is also a type:
`func grow(c Shape.Circle) Shape.Circle`.

`Shape` is an interface that only its variants satisfy, so it has no methods
of its own: write functions that take a `Shape`, or methods on the variants,
`func (c Shape.Circle) Diameter() float64`.

Its zero value is `nil`, which is none of the variants.

## Methods on Variants

A method can be declared on a variant, on either kind of enum. Each variant
answers in its own words, and the enum, which has the method on every
variant, satisfies interfaces such as `error`:

```vuka
// PasswordError is why a password change was refused.
enum PasswordError {
	CurrentRequired
	Mismatch
	ChangeFailed
}

func (PasswordError.CurrentRequired) Error() string { return "Current password is required" }
func (PasswordError.Mismatch) Error() string        { return "Passwords do not match" }
func (PasswordError.ChangeFailed) Error() string    { return "Failed to change password. Please try again." }

func change(current, next, again string) error {
	if current == "" {
		return PasswordError.CurrentRequired
	}
	if next != again {
		return PasswordError.Mismatch
	}
	return nil
}
```

`PasswordError.Mismatch` is an `error`, and `errors.Is(err,
PasswordError.Mismatch)` works, since the values compare. The enum stays a
number: JSON, the database and `match` see it as before.

On a value enum the variants are values, not types, so the methods of one
name become one method on the enum that switches on the value; `vuka eject`
writes it out as Go:

<!-- go:context
type PasswordError int

const (
	PasswordErrorCurrentRequired PasswordError = iota
	PasswordErrorMismatch
	PasswordErrorChangeFailed
)

func (p PasswordError) String() string { return "" }
func passwordErrorCurrentRequiredError() string { return "" }
func passwordErrorMismatchError() string { return "" }
func passwordErrorChangeFailedError() string { return "" }
-->

```go
func (p PasswordError) Error() string {
	switch p {
	case PasswordErrorCurrentRequired:
		return passwordErrorCurrentRequiredError()
	case PasswordErrorMismatch:
		return passwordErrorMismatchError()
	case PasswordErrorChangeFailed:
		return passwordErrorChangeFailedError()
	}
	panic("vuka: PasswordError.Error called on an invalid PasswordError: " + p.String())
}
```

The rules follow from that one method:

- **Every variant, or a default.** A method declared on some variants needs
  the rest too, or a default declared on the enum itself, which answers for
  the variants without their own and for a value that is no variant
  (`PasswordError(9)`); without a default, such a value panics.
- **One signature.** Every variant's method of a name, and the default,
  take and return the same types.
- **A receiver name gives the value:** in `func (e PasswordError.Mismatch)
  Error() string`, `e` is the enum value, always `PasswordError.Mismatch`.
- **Value receivers.** The variants are constants, so a pointer receiver is
  an error.
- **Not the generated methods.** `String`, `Valid`, `MarshalText`,
  `UnmarshalText`, `Scan` and `Value` are generated for every value enum; a
  variant declaring one is an error. For another text per variant, name the
  method differently: `Label`, `Message`.

```vuka
enum Role string {
	Admin = "admin"
	Staff = "staff"
	Guest = "guest"
}

func (Role.Admin) Label() string { return "Administrator" }

// Label is the role's value, for the roles without their own.
func (r Role) Label() string { return string(r) }
```

On an enum with data each variant is a type, so its methods are that
type's: `func (c Shape.Circle) Diameter() float64` is a method of
`Shape.Circle` alone, and nothing has to declare it on every variant. To
call a method on any `Shape`, declare it on every variant and put it in an
interface, or `match` on the variants.

The editor's hover on a variant's method says which method it is part of,
and definition from a call such as `err.Error()` offers each declaration.

## Matching on Enums

A [`match`](/features/match) over an enum takes its variants as patterns, and fields apart the
way [struct patterns](/features/match#patterns) do:

```vuka
func area(s Shape) float64 {
	match s {
	case Shape.Circle{Radius: r}:
		return math.Pi * r * r
	case Shape.Rect{W: w, H: h} if w == h:
		return w * w
	case Shape.Rect{W: w, H: h}:
		return w * h
	case Shape.Unknown:
		return 0
	}
}

func label(s Status) string {
	match s {
	case Status.Draft:
		return "draft"
	case Status.Sent, Status.Paid:
		return "out"
	}
}
```

A match must cover every variant, or have a `case _:`. One that doesn't is an
error at the `match`, in the editor and from `vuka build`, naming what's left:

```text
orders.vuka:12:2: match on Status isn't exhaustive: missing Status.Sent and Status.Paid; add the missing cases or case _:
```

So adding a variant points at every match that has to handle it.

`Shape.Rect`
alone (or `Shape.Rect{}`) matches any `Rect`; guarded cases cover nothing, and
`Shape.Circle{Radius: 0}` covers only that circle.
## Using Enums From Go

An enum is plain Go underneath, named the way Go code names enums, so `.go`
files and other modules use it directly. The table below shows the Go names:

| Vuka | Go |
|---|---|
| `enum Status { Draft, … }` | `type Status int` and `const ( StatusDraft Status = iota; … )` |
| `Status.Draft` | `StatusDraft` |
| `Status.Values()` | `StatusValues()` |
| `Status.Parse(text)` | `ParseStatus(text)`, returning `vuka.Result[Status]` (`.Get()` gives `(Status, error)`) |
| `enum Shape { Circle{…}; … }` | `type Shape interface{ isShape() }` and `type ShapeCircle struct{ … }` |
| `Shape.Circle{Radius: 2}` | `ShapeCircle{Radius: 2}` |
| `Shape.Unknown` | `ShapeUnknown{}` |

An unexported enum's names are unexported too: `enum status` gives
`statusDraft`, `statusValues` and `parseStatus`.

`vuka explain` shows the
whole expansion.
