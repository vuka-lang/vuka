# Enums

**Go:** a set of named values is a defined type, a `const` block with `iota`,
and the methods around it — `String`, parsing, JSON, the database — written by
hand.

**Vuka adds** `enum`: the variants, and everything around them, from one line.

::: vuka
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
:::

::: go
In Go, an enum is a defined type and its constants, named after the type:

```go
// Status is where an order is.
type Status int

const (
	StatusDraft Status = iota
	StatusSent
	StatusPaid
)

// Role is what a member of staff may do.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleClerk Role = "clerk"
)
```

<!-- go:fragment -->
```go
s := StatusSent
fmt.Println(s)              // Sent
fmt.Println(StatusValues()) // [Draft Sent Paid]
r, err := ParseRole("clerk") // RoleClerk
if err != nil {
	return err
}
fmt.Println(r)
```

`String`, `StatusValues` and `ParseRole` are the methods and functions below,
written once per enum.
:::

## Value enums

::: vuka
`enum Name { … }` numbers its variants from 0 in the order they are written.
A type after the name — `string`, or an integer type such as `uint8` — makes it
an enum of that type; then every variant has a value, or none:

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

Every value enum gets:

| | |
|---|---|
| `Status.Values()` | every variant, in order: `[]Status` |
| `Status.Parse(text)` | `Result[Status]` from a variant's name, or a string enum's value |
| `s.String()` | the variant's name; a string enum's value |
| `s.Valid()` | whether `s` is one of the variants |
| `MarshalText`, `UnmarshalText` | JSON and other text formats use the `String` form; an unknown one is an error |
| `Scan`, `Value` | `database/sql`: stored as the number, or the string enum's value |
:::

::: go
The type sets what the constants are: `iota` numbers them from 0, explicit
values go on each one, and a string enum's constants are strings:

```go
type Priority uint8

const (
	PriorityLow  Priority = 1
	PriorityHigh Priority = 5
)

type Visibility string

const (
	VisibilityPublic  Visibility = "Public"
	VisibilityPrivate Visibility = "Private"
)
```

What makes the type pleasant to use is a few methods: the list of values,
parsing, the name, a validity check, and the interfaces JSON and
`database/sql` look for. For `Status`:

```go
import "database/sql/driver"

// StatusValues returns every Status, in order.
func StatusValues() []Status { return []Status{StatusDraft, StatusSent, StatusPaid} }

// ParseStatus reads a Status from its name.
func ParseStatus(text string) (Status, error) {
	for _, s := range StatusValues() {
		if s.String() == text {
			return s, nil
		}
	}
	return 0, fmt.Errorf("invalid Status %q", text)
}

func (s Status) String() string {
	switch s {
	case StatusDraft:
		return "Draft"
	case StatusSent:
		return "Sent"
	case StatusPaid:
		return "Paid"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Valid reports whether s is one of the variants.
func (s Status) Valid() bool { return s >= StatusDraft && s <= StatusPaid }

func (s Status) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid Status %d", int(s))
	}
	return []byte(s.String()), nil
}

func (s *Status) UnmarshalText(text []byte) error {
	v, err := ParseStatus(string(text))
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// Scan reads a Status from a column holding its number or its name.
func (s *Status) Scan(src any) error {
	switch src := src.(type) {
	case int64:
		if v := Status(src); v.Valid() {
			*s = v
			return nil
		}
		return fmt.Errorf("invalid Status %d", src)
	case string:
		return s.UnmarshalText([]byte(src))
	case []byte:
		return s.UnmarshalText(src)
	}
	return fmt.Errorf("can't scan a %T into a Status", src)
}

// Value stores s as its number.
func (s Status) Value() (driver.Value, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid Status %d", int(s))
	}
	return int64(s), nil
}
```

A string enum's `String` is `string(r)`, it stores as its value, and `Parse`
accepts the value (and, if you like, the name):

```go
func RoleValues() []Role { return []Role{RoleAdmin, RoleClerk} }

func ParseRole(text string) (Role, error) {
	for _, r := range RoleValues() {
		if string(r) == text {
			return r, nil
		}
	}
	return "", fmt.Errorf("invalid Role %q", text)
}

func (r Role) String() string { return string(r) }
```
:::

So an enum field needs nothing more to be read from JSON, written to JSON,
stored by the [ORM](/orm/models) or scanned from a row:

::: vuka
```vuka
type Order struct {
	ID     int64
	Status Status
}

json.Marshal(Order{ID: 7, Status: Status.Paid})   // {"ID":7,"Status":"Paid"}
```
:::

::: go
```go
type Order struct {
	ID     int64
	Status Status
}

func encode() ([]byte, error) {
	return json.Marshal(Order{ID: 7, Status: StatusPaid}) // {"ID":7,"Status":"Paid"}
}
```
:::

`Scan` also accepts the variant's name from a text column. A value that isn't
a variant — `Status(9)`, or `""` in a string enum without an empty variant — is
an error to marshal or store. Give a string enum an empty variant
(<span class="vuka-only">`None = ""`</span><span class="go-only">`RoleNone Role = ""`</span>)
for a field that may be unset.

## Enums with data

::: vuka
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
:::

::: go
Values of different shapes are an interface with an unexported method, which
only the variant types of the package implement:

```go
// Shape is a parcel's footprint.
type Shape interface{ isShape() }

type (
	ShapeCircle  struct{ Radius float64 }
	ShapeRect    struct{ W, H float64 }
	ShapeUnknown struct{}
)

func (ShapeCircle) isShape()  {}
func (ShapeRect) isShape()    {}
func (ShapeUnknown) isShape() {}

func (c ShapeCircle) Diameter() float64 { return 2 * c.Radius }

var shapes = []Shape{ShapeCircle{Radius: 2}, ShapeRect{W: 1, H: 3}, ShapeUnknown{}}
```

Each variant is an ordinary struct type, so it can have methods and be a
parameter: `func grow(c ShapeCircle) ShapeCircle`. A `Shape`'s zero value is
`nil`, which is none of the variants.
:::

## match

::: vuka
A `match` over an enum takes its variants as patterns, and fields apart the
way [struct patterns](/features/match) do:

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

So adding a variant points at every match that has to handle it. `Shape.Rect`
alone (or `Shape.Rect{}`) matches any `Rect`; guarded cases cover nothing, and
`Shape.Circle{Radius: 0}` covers only that circle.
:::

::: go
A type switch takes a `Shape` apart, a `switch` a value enum:

```go
func area(s Shape) float64 {
	switch s := s.(type) {
	case ShapeCircle:
		return math.Pi * s.Radius * s.Radius
	case ShapeRect:
		if s.W == s.H {
			return s.W * s.W
		}
		return s.W * s.H
	case ShapeUnknown:
		return 0
	}
	panic(fmt.Sprintf("unknown Shape %T", s))
}

func label(s Status) string {
	switch s {
	case StatusDraft:
		return "draft"
	case StatusSent, StatusPaid:
		return "out"
	}
	panic(fmt.Sprintf("unknown Status %d", int(s)))
}
```

The compiler doesn't check that every variant is handled, so end each switch
with what should happen to the rest.
:::

## From Go

::: vuka
An enum is plain Go underneath, named the way Go code names enums, so `.go`
files and other modules use it directly:

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
`statusDraft`, `statusValues` and `parseStatus`. `vuka explain` shows the
whole expansion.
:::

::: go
An enum declared in a `.vuka` file of a package is this same Go — `StatusDraft`,
`StatusValues()`, the methods above, `ShapeCircle{…}` — so Go code uses it as
if written by hand. The one difference: its `ParseStatus` returns a
`vuka.Result[Status]`, and `.Get()` gives `(Status, error)`.
:::
