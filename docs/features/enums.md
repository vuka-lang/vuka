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

A value enum's variants are values, not types: `func ship(s Status.Sent)`
is an error saying so. Take the enum, `s Status`, and compare or `match` the
value.

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

::: code-group

```vuka [Vuka]
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

```go [Generated Go]
type PasswordError int

const (
	PasswordErrorCurrentRequired PasswordError = iota
	PasswordErrorMismatch
	PasswordErrorChangeFailed
)

func passwordErrorCurrentRequiredError() string { return "Current password is required" }
func passwordErrorMismatchError() string        { return "Passwords do not match" }
func passwordErrorChangeFailedError() string    { return "Failed to change password. Please try again." }

// …

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

:::

`PasswordError.Mismatch` is an `error`, and `errors.Is(err,
PasswordError.Mismatch)` works, since the values compare. The enum stays a
number: JSON, the database and `match` see it as before.

On a value enum the variants are values, not types, so the methods of one
name become one method on the enum that switches on the value, as the Go
tab shows: each variant's body is a function of its own, and the enum's
`Error` calls the one for its value.

The rules follow from that one method:

- **Every variant, or a default.** A method declared on some variants needs
  the rest too, or a default declared on the enum itself, which answers for
  the variants without their own and for a value that is no variant
  (`PasswordError(9)`); without a default, such a value panics. A variant
  left without the method and no default is [E0164](/errors/E0164).
- **One signature.** Every variant's method of a name, and the default,
  take and return the same types ([E0165](/errors/E0165)).
- **A receiver name gives the value:** in `func (e PasswordError.Mismatch)
  Error() string`, `e` is the enum value, always `PasswordError.Mismatch`.
- **Value receivers.** The variants are constants, so a pointer receiver is
  an error ([E0166](/errors/E0166)).
- **Not the generated methods.** `String`, `Valid`, `MarshalText`,
  `UnmarshalText`, `Scan` and `Value` are generated for every value enum; a
  variant declaring one is [E0167](/errors/E0167). For another text per variant, name the
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

## Errors With a Cause

An enum whose variants implement `error` is a set of errors a caller can
tell apart. A variant's field of type `error` is its cause, the error it
wraps:

::: code-group

```vuka [Vuka]
enum RegisterError {
	EmailTaken
	WeakPassword{Reason string}
	Database{Err error}
}

func (RegisterError.EmailTaken) Error() string     { return "email already registered" }
func (e RegisterError.WeakPassword) Error() string { return "weak password: " + e.Reason }
func (e RegisterError.Database) Error() string     { return "database: " + e.Err.Error() }
```

```go [Generated Go]
type RegisterError interface{ isRegisterError() }
type (
	RegisterErrorEmailTaken struct{}
	RegisterErrorWeakPassword struct{ Reason string }
	RegisterErrorDatabase     struct{ Err error }
)

func (RegisterErrorEmailTaken) Error() string     { return "email already registered" }
func (e RegisterErrorWeakPassword) Error() string { return "weak password: " + e.Reason }
func (e RegisterErrorDatabase) Error() string     { return "database: " + e.Err.Error() }

func (RegisterErrorEmailTaken) isRegisterError() {}

func (RegisterErrorWeakPassword) isRegisterError() {}

func (RegisterErrorDatabase) isRegisterError() {}

// Unwrap returns the cause of r.
func (r RegisterErrorDatabase) Unwrap() error { return r.Err }
```

:::

`RegisterError.Database` gets a generated `Unwrap() error` returning `Err`,
so `errors.Is(err, sql.ErrNoRows)` and `errors.As` see through it; the
other variants have none.

A variant declaring its own `Unwrap` keeps it. One with two fields of type
`error` and no `Unwrap` of its own is [E0175](/errors/E0175): declare
`Unwrap` to say which field is the cause.

Callers test a variant without fields with `errors.Is`, since it is a
value, and take the enum out of an error with `errors.As` on the enum's
type, then `match` it:

::: code-group

```vuka [Vuka]
func message(err error) string {
	if errors.Is(err, RegisterError.EmailTaken) {
		return "taken"
	}
	var e RegisterError
	if errors.As(err, &e) {
		match e {
		case RegisterError.WeakPassword{Reason: r}:
			return "weak: " + r
		case _:
			return e.Error()
		}
	}
	return "try again later"
}

enum RegisterError {
	EmailTaken
	WeakPassword{Reason string}
	Database{Err error}
}

func (RegisterError.EmailTaken) Error() string     { return "email already registered" }
func (e RegisterError.WeakPassword) Error() string { return "weak password: " + e.Reason }
func (e RegisterError.Database) Error() string     { return "database: " + e.Err.Error() }
```

```go [Generated Go]
func message(err error) string {
	if errors.Is(err, RegisterErrorEmailTaken{}) {
		return "taken"
	}
	var e RegisterError
	if errors.As(err, &e) {
		if m1 := e; false {
			panic(m1)
		} else if r, ok := func() (r string, ok bool) {
			p1, p1ok := m1.(RegisterErrorWeakPassword)
			if !p1ok {
				return
			}
			r = p1.Reason
			ok = true
			return
		}(); ok {
			return "weak: " + r
		} else if true {
			return e.Error()
		} else {
			panic("vuka: no case matched")
		}
	}
	return "try again later"
}

// …
```

:::

`errors.As` targets the enum, not a variant: any of its variants in the
chain is found. A `match` on an error does the same with the variant's type
as its pattern, `case Err(RegisterError.Database{Err: cause}):` (see
[Matching Errors](/features/match#matching-errors)).

## Matching on Enums

A [`match`](/features/match) over an enum takes its variants as patterns, and fields apart the
way [struct patterns](/features/match#patterns) do:

::: code-group

```vuka [Vuka]
enum Shape {
	Circle{Radius float64}
	Rect{W, H float64}
	Unknown
}

enum Status { Draft, Sent, Paid }

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

```go [Generated Go]
func area(s Shape) float64 {
	if m1 := s; false {
		panic(m1)
	} else if r, ok := func() (r float64, ok bool) {
		p1, p1ok := m1.(ShapeCircle)
		if !p1ok {
			return
		}
		r = p1.Radius
		ok = true
		return
	}(); ok {
		return math.Pi * r * r
	} else if w, h, ok := func() (w float64, h float64, ok bool) {
		p1, p1ok := m1.(ShapeRect)
		if !p1ok {
			return
		}
		w = p1.W
		h = p1.H
		ok = true
		return
	}(); ok && (w == h) {
		_ = h
		return w * w
	} else if w, h, ok := func() (w float64, h float64, ok bool) {
		p1, p1ok := m1.(ShapeRect)
		if !p1ok {
			return
		}
		w = p1.W
		h = p1.H
		ok = true
		return
	}(); ok {
		return w * h
	} else if func() (ok bool) {
		_, p1ok := m1.(ShapeUnknown)
		if !p1ok {
			return
		}
		ok = true
		return
	}() {
		return 0
	} else {
		panic("vuka: no case matched")
	}
}

func label(s Status) string {
	switch s {
	case StatusDraft:
		return "draft"
	case StatusSent, StatusPaid:
		return "out"
	default:
		panic("vuka: no case matched")
	}
}

// …
```

:::

A match must cover every variant, or have a `case _:`. One that doesn't is an
error at the `match`, in the editor and from `vuka build`, naming what's left:

```text
orders.vuka:12:2: match on Status isn't exhaustive: missing Status.Sent and Status.Paid; add case Status.Sent: …, case Status.Paid: …, or case _:
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
