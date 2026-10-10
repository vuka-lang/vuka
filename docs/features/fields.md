# Field attributes and references

**Go:** a struct field carries metadata only as a string tag
(`` `json:"title"` ``), read by reflection and never checked; and there is no
way to name a field as a value — code that filters, sorts or queries by a field
passes its name as a string (`"Title"`, `"author.name"`), which the compiler
can't check either.

**Vuka adds** typed attributes on fields, and `Type.Field` as a typed
reference to a field:

```vuka
type Post struct {
	Timestamps
	ID     int64    @PK
	Title  string   `json:"title"` @Char{Max: 200}
	Author FK[User] @Rel{OnDelete: Cascade}
	Views  int      @Default(0)
}

popular := vuka.And(Post.Title.Contains("go"), Post.Views.Gt(100))
byAda := Post.Author.Name.Eq("ada")
vuka.SortBy(posts, Post.Views.Desc())
```

Both are general-purpose — forms, validation, sorting, JSON, in-memory
filters, query builders — and plain Go underneath. The [ORM](/orm/) is built on
them: see [Models](/orm/models) and [Queries](/orm/queries).

## Field attributes

An attribute goes at the end of its field, after the tag if there is one:

```vuka
Title string `json:"title"` @Char{Max: 200} @Index
```

One form only: an attribute before the tag, or on a line of its own inside the
struct, is an error that shows the right place. Any number may follow a field,
an embedded field included; `A, B int @X` gives both fields the attribute.

An attribute is a Go value, type-checked like a
[declaration's attribute](/features/attributes): `@T{…}` (a composite literal),
`@T` (T's zero value), or `@f(…)` (a conversion or any call, such as
`@Default(0)`). A wrong field or type is a compile error on the attribute. The
built-ins (`@doc`, `@deprecated`, `@export`) are for declarations, and the
fields of generic types can't carry attributes.

Attributes leave the struct — the Go type is exactly the fields and tags — and
are recorded at init, for any struct that has them, with no type decorator
needed:

| | |
|---|---|
| `vuka.FieldAttrs[T]()` | `map[string][]any`, by field name |
| `vuka.FieldsOf[T]()` | `[]vuka.Field`: name, type, tag, `Attrs` |
| `f.Attr(&x)` | fills `x` with the field's attribute of `x`'s type |
| `t.Fields[i].Attrs` | the same, in a [type decorator](/features/decorators#types)'s `*vuka.Type` |

A type decorator runs after its type's attributes are recorded.

## Field references

`Post.Title`, where `Post` is a struct type and `Title` one of its fields, is a
value: a typed reference to the field. Go has no meaning for it (only
`Type.Method` is a method expression), so Vuka can give it one without
ambiguity. Statics and method expressions keep theirs; a name that is both a
field and a static reached through an embedded type is an error.

A reference goes on through the field's type:

- into a struct, or a pointer to one: `Order.Customer.Name`;
- through a type implementing `vuka.Related[U]` — `Related() *U` — into `U`,
  which is how a foreign-key type such as `FK[User]` lets `Post.Author.Name`
  reach the user's fields (Related wins over the type's own fields);
- through embedded structs, as Go promotes fields: `Post.Created` from an
  embedded `Timestamps`.

The reference's type depends on the last field's type, so only the
predicates that make sense type-check:

| Field type | Reference | Adds |
|---|---|---|
| any | `vuka.Ref[T, V]` | `Eq`, `Ne`, `In`, `Asc`, `Desc`, `Get`, `Lookup`, `Name`, `Path`, `Attrs`, `Attr` |
| numbers | `vuka.OrderedRef[T, V]` | `Lt`, `Le`, `Gt`, `Ge` |
| strings | `vuka.StringRef[T, V]` | those, and `Contains`, `StartsWith`, `EndsWith` |
| a `Compare(V) int` method (`time.Time`) | `vuka.CompareRef[T, V]` | `Lt`, `Le`, `Gt`, `Ge` |
| pointers, slices, maps, interfaces, `Option` | `vuka.NullableRef[T, V]` | `IsNil`, `NotNil` |

`T` is the type the reference starts from, also after a relation:
`Post.Author.Name` is a `vuka.StringRef[Post, string]`.

### Predicates and orderings

A predicate is data — a `vuka.Pred[T]` with an `Op` (`vuka.OpEq`,
`vuka.OpContains`, …), the field's `Path` and the `Value` — combined with
`vuka.And`, `vuka.Or` and `vuka.Not`. A query builder translates it;
`p.Match(t)` evaluates it in memory, and `p.String()` writes it:

```vuka
q := vuka.Or(vuka.And(Post.Title.Contains("go"), Post.Views.Gt(100)), Post.Home.IsNil())
fmt.Println(q)          // ((Title contains "go" AND Views > 100) OR Home IS NIL)
q.Match(post)           // true or false
```

Because a predicate is typed by `T`, an API taking `vuka.Pred[Post]` rejects
`User.Name.Eq("x")` at compile time. A comparison is false when a nil pointer
or relation is on the way to its field (`IsNil` is true).

`Asc()` and `Desc()` give a `vuka.Order[T]`; `vuka.SortBy(xs, orders…)` sorts a
slice by them, stably, the first order deciding.

`p.Path` (and `ref.FieldPath()`) is a `vuka.FieldPath`: `Names()` (`["Author",
"Name"]`), `Fields()` (each step's `reflect.StructField`), `Index()`, `Attrs()`
of the last field, and `Value(root)`.

### What it becomes

```go
vuka.StringRefOf[Post, string]([][]int{{3}, {1}}, func(__x *Post) { _ = __x.Author.Related().Name })
```

The index path of each field, and a function that is never called but selects
the same fields — so the compiler checks them and the editor's hover, go to
definition and rename land on the struct's fields. An unknown field is an error
with a suggestion (`Post has no field Titel; did you mean Title?`), as is an
unexported field of another package's type, or a name more than one embedded
struct supplies.

Compile errors — from `vuka build`, the editor or Vuka itself — quote a
reference as the source writes it: `cannot use Post.Views (value of struct type
vuka.OrderedRef[Post, int]) as int value`, as they quote statics as
`Post.Limit`.

References to the fields of standard-library types (`http.Cookie.Name`) are
found only when the package is type-checked for another reason; references
inside an attribute's arguments aren't lowered.
