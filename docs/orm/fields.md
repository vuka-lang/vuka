# Fields

A model's field says what its column is, what a row gets when the field is
left empty, and what a write may put in it — with attributes after the field:


```go
type Order struct {
	orm.Model[Order]
	ID     int64
	Ref    string
	Status string
	Qty    int
	Email  string
	Phone  Phone
	Placed time.Time
	Due    time.Time
}

func (o *Order) Schema(s *orm.Schema) {
	s.Field(&o.Ref, orm.Char{Max: 12, Min: 3, Pattern: `^[A-Z0-9-]+$`, Trim: true, Label: "Reference"})
	s.Field(&o.Status, orm.Char{Max: 20, Default: "draft", Choices: []string{"draft", "paid", "void"}})
	s.Field(&o.Qty, orm.Int{Min: 1, Max: 100, Size: orm.Small, Default: 1})
	s.Field(&o.Email, Email{Unique: true})
	s.Field(&o.Placed, orm.Datetime{DefaultNow: true, Precision: 3, UTC: true})
	s.Field(&o.Due, orm.DateField{Help: "the day payment is due"})
}
```

`Status` is a `VARCHAR(20)` with `DEFAULT 'draft'` in the table, an order
created without one is a draft, and a write giving it `"lost"` is refused
before it reaches the database. `Email` and `Phone` use an attribute and a
type of the program's own (below).

## Column attributes

`@orm.PK`, `@orm.Char`, `@orm.Int`, `@orm.DateField` and `@orm.Datetime` each
take these, all optional:

| Field | |
|---|---|
| `Column` | the column's name; the field's in snake_case otherwise |
| `Null` | the column takes NULL (a pointer or `Option` field does already) |
| `Unique`, `Index` | a unique constraint, an index |
| `Default` | the column default, a Go value (nil: none) — see [Defaults](#defaults) |
| `Comment` | the column's comment in the table (Postgres, MySQL; SQLite keeps none) |
| `Label`, `Help` | what forms and tools show for the field (see [Field info](#field-info)) |

`@orm.PK` takes `Column`, `Comment`, `Label`, `Help` and `Default`: a key is
never NULL, and is unique and indexed already. The bare forms, `@orm.PK` and
`@orm.Char{Max: 200}`, are what they always were.

| Attribute | Its own fields |
|---|---|
| `@orm.PK` | `Manual`: an integer key the program sets, not one the database counts (0 is then a key like any). `UUID`: a string key the ORM sets to a random UUID on create when it is empty — `uuid` on Postgres, `CHAR(36)` on MySQL and SQLite |
| `@orm.Char` | `Max`: a `VARCHAR(Max)`, and the most characters a write may hold. `Min`: the fewest. `Choices`: the values it may take. `Pattern`: a regular expression (Go's syntax) it matches. `Trim`, `Lower`: its surrounding spaces removed, lower case, as it is written. `Collation`: the column's (`utf8mb4_bin`, `"C"`, `NOCASE`) |
| `@orm.Int` | `Min`, `Max`: bounds, an integer each, nil for none — `Min: 0` is a bound. `Size`: `orm.Small` (SMALLINT), `orm.Medium` (INTEGER, 32 bits), `orm.Big` (BIGINT), over the Go type's. `Unsigned`: `UNSIGNED` on MySQL, and negative values refused on every database. `Choices`: the values it may take |
| `@orm.DateField` | a `DATE` column of a `time.Time` field: the day alone. `AutoNow`, `AutoNowAdd`: today on every save, on create. `DefaultNow`: today on create when it is empty, and `DEFAULT CURRENT_DATE` in the table |
| `@orm.Datetime` | a timestamp of a `time.Time` field. `AutoNow`, `AutoNowAdd`, `DefaultNow` as above, with `CURRENT_TIMESTAMP`. `Precision`: its fractional digits, 1–6 (`DATETIME(n)` on MySQL, `timestamp(n)` on Postgres; 0 leaves the database's, microseconds) — times are cut to it as they are written. `UTC`: written and read in UTC |

The date is `@orm.DateField` because `orm.Date` is the lookup transform.

The attributes of earlier versions stay as they were, and mix with these:
`@orm.Column`, `@orm.Null`, `@orm.Unique`, `@orm.Index`, `@orm.AutoNow`,
`@orm.AutoNowAdd`, `@orm.DefaultTo`, `@orm.DefaultSQL`, `@orm.DBType` (see
[Models](/orm/models#field-attributes)). A field's attributes apply left to
right, each over the ones before it, and a member left empty never undoes an
earlier attribute: in `@orm.Column("ref_no") @orm.Char{Max: 20}` the column
is `ref_no`; in `@orm.Char{Column: "a"} @orm.Column("b")` it is `b`.

## Defaults

A `Default` (or `@orm.DefaultTo(v)`) is two things at once:

- the column's `DEFAULT` in the table migrations make, so a row SQL inserts
  without the column takes it;
- the value `Create` (and `BulkCreate`, `GetOrCreate`) gives the field when it
  is left at its zero value.


<!-- go:fragment -->
```go
o := Order{Ref: "A-1", Email: "ada@example.com", Phone: "+255 712 000"}
if err := o.Save(ctx); err != nil { // o.Status == "draft", o.Qty == 1, o.Placed is now
	return err
}
```

The zero value is what counts as empty, so a `bool` with `Default: true` is
always true on create: give it an `Option[bool]` (None is empty) to create it
false. `@orm.DefaultSQL("…")` is the table's default alone, an expression the
ORM doesn't evaluate. `DefaultNow` is the database's current time in the
table, and the program's clock (`orm.Clock`) on create.

## Validation

A write checks each value against its field before it reaches the database:
`Create`, `BulkCreate` (every row), `GetOrCreate`, `Save`, `UpdateOrCreate`
and a query's `Update` (each value it sets that isn't an expression). What
fails is an `orm.Invalid` error naming the columns, the shape a foreign key
violation has:


<!-- go:fragment -->
```go
err := orm.Objects[Order]().Create(ctx, &Order{Ref: "a", Status: "lost", Qty: 500})
fmt.Println(errors.Is(err, orm.ErrInvalid)) // true
var e *orm.Error
if errors.As(err, &e) {
	fmt.Println(e.Fields) // map[qty:[must be at most 100] ref:[must be at least 3 characters must match ^[A-Z0-9-]+$] status:[must be one of draft, paid, void] …]
}
```

| Check | Message |
|---|---|
| `Char`'s `Max` (and a tag's size) | `must be at most n characters` |
| `Char`'s `Min` | `must be at least n characters` |
| `Pattern` | `must match <pattern>` |
| `Choices` | `must be one of a, b, c` |
| `Int`'s `Min`, `Max`; `Unsigned` | `must be at least n`, `must be at most n` |
| the type's `Validate()`, an attribute's `Validate(v)` | the error's text |

A NULL (a nil pointer, `None`) passes every check. `Trim` and `Lower` run
first, so `" Ada@X.org "` is checked, and stored, as `"ada@x.org"`; the row
you passed holds what was written, the stamps of a `UTC` or `Precision`
field included. A web app answers `Invalid` with a 422 and the fields'
messages.

## Value types

A field's type can say how its values reach the database and come back, with
any of four methods — each optional, each independent of the others:


```go
type Phone string

// DBValue is what's written, and what conditions compare the column with.
func (p Phone) DBValue(d orm.Dialect) (any, error) {
	return strings.ReplaceAll(string(p), " ", ""), nil
}

// FromDB reads the column's value, as the driver gives it.
func (p *Phone) FromDB(src any) error {
	*p = Phone(fmt.Sprint(src))
	return nil
}

// ColumnType is the column's type in migrations.
func (Phone) ColumnType(d orm.Dialect) string { return "VARCHAR(20)" }

// Validate checks each value a write gives the field.
func (p Phone) Validate() error {
	if !strings.HasPrefix(string(p), "+") {
		return errors.New("must start with a country code")
	}
	return nil
}
```

| Method | |
|---|---|
| `DBValue(d orm.Dialect) (any, error)` | the value written on `Create`, `Save` and `Update`, and the value lookups compare: `Order.Phone.Eq("+255 712 000")` finds `+255712000` |
| `FromDB(src any) error` (pointer receiver) | the value read; `src` is nil for NULL unless the field is a pointer or an `Option`, which are nil and None then |
| `ColumnType(d orm.Dialect) string` | the column's type on each dialect, `""` for the ORM's choice; `@orm.DBType` on the field wins over it |
| `Validate() error` | a write check, as above |

`d.Name()` is `"postgres"`, `"mysql"` or `"sqlite"`. A type may also be a
`sql.Scanner` and `driver.Valuer`: then the ORM uses `DBValue` and `FromDB`
for the model's column, and database/sql's methods stay for code handing the
type to the driver itself (a raw query's arguments). Without `ColumnType`,
the column follows the type's kind: a `Phone` string is `TEXT`, or a
`VARCHAR` with `@orm.Char{Max: n}`. `orm.JSON[T]` is such a type, through
`Scan` and `Value`.

## Your own attributes

An attribute is any value with an `Apply(*orm.FieldOptions)` method; written
after a field, it is applied in its turn like the built-in ones, which are
written the same way. A `Validate(v any) error` method adds a write check:


```go
type Email struct{ Unique bool }

func (e Email) Apply(o *orm.FieldOptions) {
	o.MaxLength, o.Lower, o.Trim = 254, true, true
	o.Unique = o.Unique || e.Unique
}

func (Email) Validate(v any) error {
	if !strings.Contains(v.(string), "@") {
		return errors.New("is not an email address")
	}
	return nil
}

// vukaTargets says where Email may be written in .vuka files: on fields.
func (Email) vukaTargets() [vuka.OnField]struct{} { return [vuka.OnField]struct{}{} }

type User struct {
	orm.Model[User]
	ID    int64
	Email string
}

func (u *User) Schema(s *orm.Schema) { s.Field(&u.Email, Email{Unique: true}) }
```

`Validate` gets the value without its pointer or `Option`, and isn't called
for NULL. `orm.FieldOptions` has every setting the built-ins use:

| `FieldOptions` | |
|---|---|
| `Column`, `Skip`, `PK`, `Unique`, `Index`, `UniqueIndex`, `Null`, `Type` | the column and its keys, `DBType`'s type |
| `MaxLength`, `MinLength`, `Pattern`, `Choices`, `Min`, `Max`, `Checks` | the write checks (`Min`, `Max` are `vuka.Option[int64]`; `Checks` functions of the value) |
| `Trim`, `Lower` | text normalized as it is written |
| `Default`, `Initial` | the table's default (SQL), the value `Create` gives an empty field |
| `AutoNow`, `AutoNowAdd`, `Date`, `Precision`, `UTC` | times |
| `Size`, `Unsigned` | integers |
| `Manual`, `UUID` | keys |
| `Comment`, `Collation`, `Label`, `Help` | the column's details, the field's description |
| `OnDelete`, `Related`, `FK`, `Through`, `CrossDB`, `Dims` | relations, vectors |

Set what the attribute means, and leave the rest: a member left at its zero
value keeps what an earlier attribute set.

What only the running program knows — the columns a legacy table it read at
start lacks — a model attribute decides as the ORM maps the model: its
`ApplyModel` sets `ModelConfig.Fields`, a function giving a field its
`FieldOptions` over its attributes (`func(owner reflect.Type, f
reflect.StructField) (orm.FieldOptions, bool)`, false for a field it says
nothing of), asked once per field before the model's first query.
`orm.SetFieldOptions` (deprecated) does the same for every model.

The ORM's attribute types declare where they go (`vuka.OnField`; `orm.Meta`
on the embedded `orm.Base` or the type, `orm.TxOn` and `orm.Retry` on
functions), so `@orm.Char` above a function is a compile error, and an
editor completing `@orm.` after a field offers the field attributes.
In Go a type declares them with a `vukaTargets` method, as `Email` does
above; it matters only to `.vuka` files using the attribute.

## Field info

`Fields()` on a model's objects lists its columns as forms, admin pages and
tools need them:


<!-- go:fragment -->
```go
for _, f := range orm.Objects[Order]().Fields() {
	fmt.Println(f.Column, f.Label, f.Help, f.MaxLength, f.Choices, f.Null)
}
```

Each `orm.FieldInfo` has the field's `Name`, `Column`, `Type`, `PK`, `Null`,
`Unique`, `Index`, `MaxLength`, `MinLength`, `Min`, `Max`, `Choices`,
`Pattern`, `Default`, `Initial`, `Label`, `Help`, `Comment`, `AutoNow` and
`AutoNowAdd`.

## In migrations

<!-- go:context
import m "github.com/vuka-lang/orm/v2/migration"

var _ = m.F
-->

`orm makemigrations` writes every column property the attributes set, and
diffs them when they change:

```go
var orderFields = []m.NamedField{
	m.F("ref", m.Varchar(12).Collation("utf8mb4_bin").Comment("the customer's reference")),
	m.F("status", m.Varchar(20).DefaultSQL("'draft'")),
	m.F("qty", m.SmallInt().Unsigned().DefaultSQL("1")),
	m.F("placed", m.Time().Precision(3).DefaultSQL("CURRENT_TIMESTAMP")),
	m.F("due", m.Date()),
	m.F("phone", m.Custom().Types(m.Dialects{Postgres: "VARCHAR(20)", MySQL: "VARCHAR(20)", SQLite: "VARCHAR(20)"})),
	m.F("id", m.UUID().PK()),
}
```

| Property | Postgres | MySQL | SQLite |
|---|---|---|---|
| `Comment` | `COMMENT ON COLUMN` | `COMMENT '…'` | — |
| `Collation` | `COLLATE "name"` | `COLLATE name` | `COLLATE name` |
| `Precision` | `TIMESTAMP(n)` | `DATETIME(n)` (6 by default) | — |
| `Size` | `SMALLINT`, `INTEGER`, `BIGINT` | the same | the same |
| `Unsigned` | — (checked on writes) | `UNSIGNED` | — (checked on writes) |
| `DefaultNow` | `CURRENT_TIMESTAMP`, `CURRENT_DATE` | `CURRENT_TIMESTAMP(n)`, `(CURRENT_DATE)` | `CURRENT_TIMESTAMP`, `CURRENT_DATE` |
| a `DateField` | `DATE` | `DATE` | `DATE` |
| a `UUID` key | `uuid` | `CHAR(36)` | `CHAR(36)` |

A changed default, comment, collation, precision, size, nullability, unique
constraint or index is an `m.AlterField`; `orm migrate` applies it (SQLite by
rebuilding the table, as for any change). A field with no default of its own
keeps the one a migration gave it (an `AddField`'s value for the rows that
existed). `Choices`, `Min`, `Max`, `Pattern`, `Label` and `Help` are the
program's alone and never reach the table. [Unmanaged](/orm/migrations)
models are never migrated.
