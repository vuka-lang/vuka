# Legacy Schemas

[[toc]]

## Introduction

A program often reads tables it didn't design: a database it shares with an
older system, which names its tables and columns its own way, keeps a status
as a number, or a time as text. The ORM serves such tables with the same Go
models the program's own tables use. A *names set* says what the models are
called there; a *schema* is a database and the names set its tables have.

The same models then move a table from one database to another: writes go
to both while reads stay on one, and a route switches them over without a
code change.

<!-- go:context
func useCustomer(c Customer) {}

var dbs *orm.Databases
-->

## Declaring a Schema {#declaring}

An `orm.DBSchema` names a database and a names set:

```go
var Legacy = orm.DBSchema{DB: "legacy", Names: "legacy", Unmanaged: true}
```

| `orm.DBSchema` field | |
|---|---|
| `DB` | the database added under that name; `""` the default |
| `Names` | the names set; `""` the default naming |
| `Unmanaged` | the tables are the database's own: never created or migrated |

## Naming Fields There {#field-names}

A names set's names are struct tags keyed by the set's name: under
`Names: "legacy"`, a field's `legacy:"…"` tag, and the type's
`LegacyTableName()` method for its table. A field without a tag keeps its
default column there. These models are a customer, its profile and its
groups, on tables that the older system named apart:

```go
type Group struct {
	orm.Model[Group]
	ID   int64
	Name string `legacy:"title"`
}

func (Group) LegacyTableName() string { return "crm_groups" }

type Profile struct {
	orm.Model[Profile]
	ID       int64
	Customer orm.FK[Customer] `legacy:"owner_id"`
	City     string
}

func (p *Profile) Schema(s *orm.Schema) {
	s.Field(&p.Customer, orm.Rel{OnDelete: orm.Cascade, Related: "profile"}, orm.Unique{})
}

func (Profile) LegacyTableName() string { return "crm_profiles" }

type Customer struct {
	orm.Model[Customer]
	ID      int64
	Email   string         `legacy:"mail"`
	Phone   string         `legacy:"tel_no"`
	Notes   string         `legacy:"-"`
	City    string         `legacy:"profile__city"`
	Code    string         `legacy:"code"`
	Profile *Profile
	Groups  orm.M2M[Group] `legacy:"many2many:crm_customer_groups;joinReferences:group_id"`
}

func (c *Customer) Schema(s *orm.Schema) {
	s.Field(&c.Code, orm.Skip{}) // a column on the legacy table only
}

func (Customer) LegacyTableName() string { return "crm_customers" }
```

A tag takes these forms:

| Tag | |
|---|---|
| `legacy:"mail"` | the column there |
| `legacy:"column:state;type:integer"` | the column and its SQL type |
| `legacy:"-"` | no such column there |
| `legacy:"profile__city"` | the column read through a relation there: the profile's `city` |
| `legacy:"many2many:crm_customer_groups;joinReferences:group_id"` | a many-to-many's table between, and its columns (`joinForeignKey` for this row's key, `joinReferences` for the related row's) |
| `legacy:"foreignKey:…"`, `fk:…`, `related:…`, `elements:…` | a relation's keys there |

A field the default naming skips (`orm.Skip`) and the set's tag names is a
column of that schema only, as `Code` is.

## Querying a Schema {#querying}

`orm.Of[T](s)` is the model's manager on schema `s`, made once per model and
schema; `orm.Objects[T](s)` is the same. Queries name Go fields, default
names and relation paths whatever the schema, and the ORM writes them in
the schema's names:

<!-- go:fragment -->
```go
customers, err := orm.Of[Customer](Legacy).Filter(orm.Q{"groups__name": "vip", "city": "Arusha"}).All(ctx)
if err != nil {
	return err
}
for _, c := range customers {
	useCustomer(c)
}
```

```sql
SELECT crm_customers.id, crm_customers.mail, crm_customers.tel_no, crm_customers.code, profile.city
FROM crm_customers LEFT JOIN crm_profiles AS profile ON profile.owner_id = crm_customers.id
WHERE profile.city = ? AND EXISTS (
  SELECT 1 FROM crm_customer_groups JOIN crm_groups ON crm_groups.id = crm_customer_groups.group_id
  WHERE crm_customer_groups.customer_id = crm_customers.id AND crm_groups.title = ?)
```

A row remembers the schema it was read from, so its `Save`, `Delete` and
`Load` go back there. A field read through a relation is read-only on that
schema: `Create` and `Save` skip it, and `Update` refuses it, naming the
model to write instead.

### Writing a Row to Another Schema {#using}

`row.Using(s)` binds a row to schema `s` and returns it. A row loaded from
another schema is new there, so `Save` inserts a copy:

```go
func copyToLegacy(ctx context.Context, c Customer) error {
	return c.Using(Legacy).Save(ctx)
}
```

### Raw SQL on a Schema {#raw}

The raw calls take the schema, and write nothing of their own: the SQL is
the schema's.

| | |
|---|---|
| `orm.RawOn[R](ctx, s, sql, args…)` | `orm.Raw` on the schema's database |
| `orm.RawIterOn[R](ctx, s, sql, args…)` | the same, one row at a time |
| `orm.ExecOn(ctx, s, sql, args…)` | `orm.Exec` on the schema's database |
| `orm.Of[T](s).Raw(sql, args…)` | the model's rows, each column filling the field of that column under the schema's names |

<!-- go:fragment -->
```go
n, err := orm.RawOn[int64](ctx, Legacy, "SELECT COUNT(*) FROM crm_customers WHERE tel_no LIKE ?", "255%")
if err != nil {
	return err
}
local, err := orm.Of[Customer](Legacy).Raw("SELECT * FROM crm_customers WHERE tel_no LIKE ?", "255%").All(ctx)
```

## Verifying a Schema {#verifying}

`s.Verify(ctx, models…)` checks that the models map onto the schema: no tag
naming no field, no relation to a model by no field of it, no column read
through no relation, no two relations claiming one inverse name. When the
context has the schema's database it also waits for it to answer, as
`Migrate` does, and checks that the database can make the models' columns,
indexes and generated columns and has their extensions. Run it at start-up,
after `Migrate`:

```go
func checkLegacy(ctx context.Context) error {
	return Legacy.Verify(ctx, Profile{}, Group{}, Customer{})
}
```

`orm.DBSchema{}.Verify` checks the default naming on the default database.

## Values That Differ by Schema {#values}

A field may keep one Go type while its value is stored differently on a
schema: a status kept as a code, a time kept as text.

### Codecs

An `orm.Codec[G, DB]` converts a field's Go value `G` to its database form
`DB` (`To`) and back (`From`). Two are ready-made:

| | |
|---|---|
| `orm.CodeMap(codes)` | a string value kept as an integer code; a value or code not in the map is an error |
| `orm.TimeAsText(layout)` | a `time.Time` kept as text in `layout` |

A codec rides the field for one names set, through `FieldOptions.Schemas`:
an `orm.SchemaField{Column, Type, Skip, Codec}` per set's name, set by a
field attribute of your own. This attribute gives a field its column and
codec on the `legacy` set:

```go
type legacyColumn struct {
	column string
	codec  orm.SchemaCodec
}

func (a legacyColumn) Apply(o *orm.FieldOptions) {
	if o.Schemas == nil {
		o.Schemas = map[string]orm.SchemaField{}
	}
	o.Schemas["legacy"] = orm.SchemaField{Column: a.column, Codec: a.codec}
}

type Shipment struct {
	orm.Model[Shipment]
	ID      int64
	Ref     string
	Status  string
	Shipped time.Time
}

func (x *Shipment) Schema(s *orm.Schema) {
	s.Field(&x.Status, orm.Char{Max: 20}, legacyColumn{"state", orm.CodeMap(map[string]int64{"open": 1, "shipped": 2})})
	s.Field(&x.Shipped, legacyColumn{"shipped_text", orm.TimeAsText("2006-01-02 15:04:05")})
}

func (Shipment) LegacyTableName() string { return "tbl_shipments" }
```

Under the `legacy` set `Status` is the `state` column holding 1 or 2, and
conditions on it are converted too: `orm.Q{"status": "open"}` sends 1. The
default naming is untouched. An empty `Column` or `Type` keeps the field's
own; `Skip` drops the column there. `orm.SchemaCodec` is the interface a
codec satisfies, and `orm.Codec` its only implementation.

### Types That Convert Themselves {#schema-valuer}

A type of the program's own may convert itself per schema instead.
`orm.SchemaValuer`'s `ValueFor(names)` is its value written under a names
set (`""` the default), for every value of the type the ORM sends —
conditions, `Update`, `Save`, `Create`. `orm.SchemaScanner`'s
`ScanFor(names, src)` reads it back; without it the type's `Scan` reads
either form:

```go
type OrderStatus string

var statusCodes = map[OrderStatus]int64{"open": 1, "paid": 2}

func (s OrderStatus) ValueFor(names string) (driver.Value, error) {
	if names == "legacy" {
		return statusCodes[s], nil
	}
	return string(s), nil
}

func (s *OrderStatus) ScanFor(names string, src any) error {
	if names != "legacy" {
		switch v := src.(type) {
		case string:
			*s = OrderStatus(v)
		case []byte:
			*s = OrderStatus(v)
		}
		return nil
	}
	for status, code := range statusCodes {
		if code == src {
			*s = status
			return nil
		}
	}
	return fmt.Errorf("no order status %v", src)
}
```

A value of another type sent to such a column — an `int64` where the field
is an `OrderStatus` — is refused: it would be right on one schema only. A
field's codec wins over its type's `ValueFor` and `ScanFor`.

## Mirrored Writes {#mirrored-writes}

A table moving from one database to another is kept current on both: every
write of the model on its own database is repeated on a mirror. Reads never
use the mirror.

The mirror is written after the write's transaction commits (at once outside
one), with the keys the primary gave new rows. A mirror that fails doesn't
fail the write — the primary is the truth — and the failure is logged and
passed to `orm.OnMirrorError`, the place to queue the table for a re-sync:

```go
func watchMirrors() (stop func()) {
	return orm.OnMirrorError(func(ctx context.Context, e orm.MirrorError) {
		slog.ErrorContext(ctx, "mirror", "table", e.Table, "op", e.Op, "db", e.Mirror, "err", e.Err)
	})
}
```

`orm.MirrorError` has the `Table`, the `Op` (`create`, `update`,
`delete`), the `Mirror` database's name and the `Err`. Raw writes (`Exec`)
are the database's alone: no mirror repeats them.

### Declaring a Mirror

A model names its mirror with `Meta`'s `Mirror`, or a manager with
`orm.Mirror(name)`. Two more options write the mirror in another shape:

| | |
|---|---|
| `orm.Mirror(name)` | repeat every write on the database added as `name` |
| `orm.MirrorUnder(set)` | write the mirror under a names set: the columns, types and codecs the fields declare for it |
| `orm.MirrorTable(name)` | the mirror's table, over the model's |

`orm.Dual[T](primary, mirror, opts…)` routes a model for a dual write
without a change to the model: it reads and writes `primary`, and repeats
each write on `mirror`. Here new shipments are written to `main` and kept
current on the older system's `tbl_shipments`, its status as a code and its
times as text:

<!-- go:fragment -->
```go
dbs, err := orm.OpenDatabases(ctx,
	orm.Declare("main", os.Getenv("DATABASE_URL"), orm.AsDefault()),
	orm.Declare("legacy", os.Getenv("LEGACY_URL")),
	orm.Dual[Shipment]("main", "legacy", orm.MirrorUnder("legacy"), orm.MirrorTable("tbl_shipments")))
if err != nil {
	return err
}
defer dbs.Close(ctx)
```

`orm.WithMirror(ctx, db)` repeats every model's writes in `ctx` on `db`,
for tests and tools.

### Routes

`Dual` is a table's route. A route moves a table without a code change, for
models that name no databases of their own (`Using`, routers and a model's
`Meta` win over it), so a program can read its routes from configuration:

| `orm.Route` field | TOML, JSON | |
|---|---|---|
| `DB` | `db` | the database read, and written first |
| `Mirror` | `mirror` | the database each write is repeated on |
| `MirrorNames` | `mirror_names` | the names set the mirror is written under |
| `MirrorTable` | `mirror_table` | the mirror's table, over the model's |

| `*orm.Databases` method | |
|---|---|
| `Route(table, r)` | routes the table |
| `Unroute(table)` | drops its route |
| `RouteFor(table)` | its route, and whether one is set |
| `Apply(opts…)` | applies open options — `Dual`, routers — to the set |

Keep one of `Dual` or a route per table.

### Moving a Table {#cutover}

To move `shipments` from the older system's database to `main`:

1. Make the table on `main` with a migration, and copy the rows over.
2. Route the table for a dual write: `main` is read and written first, and
   every write is repeated on `legacy` in its own shape, so the older system
   keeps reading current rows.

   <!-- go:fragment -->
   ```go
   dbs.Route("shipments", orm.Route{DB: "main", Mirror: "legacy", MirrorNames: "legacy", MirrorTable: "tbl_shipments"})
   ```

3. Watch `orm.OnMirrorError`; re-copy what a failed mirror missed.
4. Once nothing reads the old table, drop the mirror —
   `dbs.Route("shipments", orm.Route{DB: "main"})`, or `dbs.Unroute` when
   `main` is the default — and the old table with it.

## Testing Mirrors {#testing}

`ormtest.Mirror(t, ctx, models…)` opens a second database with the models'
tables and has `ctx`'s writes mirrored to it. It returns the mirrored
context and one reading the mirror, to check what reached it:

```go
func TestShipmentMirrored(t *testing.T) {
	ctx := ormtest.Open(t, orm.Objects[Shipment]())
	ctx, onMirror := ormtest.Mirror(t, ctx, orm.Objects[Shipment]())
	ormtest.Seed(t, ctx, orm.Objects[Shipment](), &Shipment{Ref: "A1", Status: "open"})
	if n, err := orm.Objects[Shipment]().Count(onMirror); err != nil || n != 1 {
		t.Fatalf("mirror holds %d shipments: %v", n, err)
	}
}
```

The mirror is in-memory SQLite, or `ORMTEST_MIRROR_DRIVER`'s server at
`ORMTEST_MIRROR_DSN`, as `ORMTEST_DRIVER` and `ORMTEST_DSN` choose the
primary's ([Database Testing](/orm/testing#on-a-real-server)).
