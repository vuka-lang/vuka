# Field attributes and references

**Go:** a struct field carries metadata only as a string tag
(`` `json:"title"` ``), read by reflection and never checked; and there is no
way to name a field as a value — code that filters, sorts or queries by a field
passes its name as a string (`"Title"`, `"author.name"`), which the compiler
can't check either.

**Vuka adds** typed attributes on fields, and `Type.Field` as a typed
reference to a field:

<!-- go:context
type User struct{ Name string }

type FK[T any] struct {
	ID  int64
	obj *T
}

func (k FK[T]) Related() *T { return k.obj }

type Timestamps struct{ Created time.Time }

type Address struct{ City string }

type PK struct{}
type Char struct{ Max int }
type Index struct{}
type Rel struct{ OnDelete string }
type DefaultValue struct{ V any }

const Cascade = "cascade"

func Default(v any) DefaultValue { return DefaultValue{v} }

var posts []Post
-->

::: vuka
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
:::

::: go
In Go, a struct's field attributes are registered by field name at init, and
a reference is made from the field's address — the compiler checks both
fields:

```go
type Post struct {
	Timestamps
	ID     int64
	Title  string `json:"title"`
	Author FK[User]
	Views  int
	Home   *Address
}

func init() {
	vuka.RegisterFields[Post](map[string][]any{
		"ID":     {PK{}},
		"Title":  {Char{Max: 200}},
		"Author": {Rel{OnDelete: Cascade}},
		"Views":  {Default(0)},
	})
}

var (
	PostTitle      = vuka.StringRefTo(func(p *Post) *string { return &p.Title })
	PostViews      = vuka.OrderedRefTo(func(p *Post) *int { return &p.Views })
	PostAuthorName = vuka.StringRefTo(func(p *Post) *string { return &p.Author.Related().Name })
	PostHome       = vuka.NullableRefTo(func(p *Post) **Address { return &p.Home })
)
```

<!-- go:fragment -->
```go
popular := vuka.And(PostTitle.Contains("go"), PostViews.Gt(100))
byAda := PostAuthorName.Eq("ada")
vuka.SortBy(posts, PostViews.Desc())
```
:::

Both are general-purpose — forms, validation, sorting, JSON, in-memory
filters, query builders — and plain Go underneath. See also the
[ORM](/orm/models), which declares its models and queries with them.

## Field attributes

::: vuka
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
:::

::: go
An attribute is any Go value, listed by field name in
`vuka.RegisterFields[T]` at init; any number per field:

<!-- go:fragment -->
```go
vuka.RegisterFields[Post](map[string][]any{
	"Title": {Char{Max: 200}, Index{}},
})
```

Code reading them finds them by type:
:::

| | |
|---|---|
| `vuka.FieldAttrs[T]()` | `map[string][]any`, by field name |
| `vuka.FieldsOf[T]()` | `[]vuka.Field`: name, type, tag, `Attrs` |
| `f.Attr(&x)` | fills `x` with the field's attribute of `x`'s type |
| `t.Fields[i].Attrs` | the same, in a [type decorator](/features/decorators#types)'s `*vuka.Type` |

<span class="vuka-only">A type decorator runs after its type's attributes are recorded.</span>

::: vuka
A struct embedding a type that asks for a schema method — `orm.Base`
(`Schema`), `ui.Live` (`LiveSchema`) — gets one instead: each field's
attributes become a call naming the field by address, `s.Field(&p.Title,
orm.Char{Max: 200})`, those on the embedded field `s.Model(…)`, so the Go
is what a person writes by hand ([models](/orm/models),
[live pages](/ui/stateful#hooks)).
:::

::: go
A struct embedding a type that asks for a schema method — `orm.Model`
(`Schema`), `ui.Live` (`LiveSchema`) — takes its field attributes there
instead, each a call naming the field by address
([models](/orm/models), [live pages](/ui/stateful#hooks)):

<!-- go:context
type Article struct {
	orm.Model[Article]
	Title string
}
-->

```go
func (a *Article) Schema(s *orm.Schema) {
	s.Field(&a.Title, orm.Char{Max: 200})
}
```
:::

## Field references

::: vuka
`Post.Title`, where `Post` is a struct type and `Title` one of its fields, is a
value: a typed reference to the field. Go has no meaning for it (only
`Type.Method` is a method expression), so Vuka can give it one without
ambiguity. Statics and method expressions keep theirs; a name that is both a
field and a static reached through an embedded type is an error.
:::

::: go
`vuka.RefTo(func(p *Post) *V { return &p.Field })` is a typed reference to
the field whose address the function returns: the compiler checks the
fields, and the editor's hover, go to definition and rename land on them.
`RefTo` runs the function once per instantiation, on a value of `Post` made
for it (nil pointers filled in), and finds the field the address is of; the
function must only select fields. A package-level variable per reference,
named after the type and field, reads well: `PostTitle`.
:::

A reference goes on through the field's type:

- into a struct, or a pointer to one: <span class="vuka-only">`Order.Customer.Name`</span><span class="go-only">`&o.Customer.Name`</span>;
- through a type implementing `vuka.Related[U]` — `Related() *U` — into `U`,
  which is how a foreign-key type such as `FK[User]` lets <span class="vuka-only">`Post.Author.Name`</span><span class="go-only">`&p.Author.Related().Name`</span>
  reach the user's fields<span class="vuka-only"> (Related wins over the type's own fields)</span>;
- through embedded structs, as Go promotes fields: <span class="vuka-only">`Post.Created`</span><span class="go-only">`&p.Created`</span> from an
  embedded `Timestamps`.

The reference's type depends on the last field's type, so only the
predicates that make sense type-check<span class="go-only">; pick the constructor for it</span>:

| Field type | Reference | Adds |
|---|---|---|
| any | `vuka.Ref[T, V]` (`RefTo`) | `Eq`, `Ne`, `In`, `Asc`, `Desc`, `Get`, `Lookup`, `Name`, `Path`, `Attrs`, `Attr` |
| numbers | `vuka.OrderedRef[T, V]` (`OrderedRefTo`) | `Lt`, `Le`, `Gt`, `Ge` |
| strings | `vuka.StringRef[T, V]` (`StringRefTo`) | those, and `Contains`, `StartsWith`, `EndsWith` |
| a `Compare(V) int` method (`time.Time`) | `vuka.CompareRef[T, V]` (`CompareRefTo`) | `Lt`, `Le`, `Gt`, `Ge` |
| pointers, slices, maps, interfaces, `Option` | `vuka.NullableRef[T, V]` (`NullableRefTo`) | `IsNil`, `NotNil` |

`T` is the type the reference starts from, also after a relation:
<span class="vuka-only">`Post.Author.Name`</span><span class="go-only">`PostAuthorName`</span> is a `vuka.StringRef[Post, string]`.

An [ORM model](/orm/queries#conditions) is different: its fields are the typed
fields `orm generate` writes, <span class="vuka-only">so `Post.Title` is</span><span class="go-only">such as</span> `PostFields.Title`, an
`orm.TextField[string]`, and its conditions are the engine's.

### Predicates and orderings

A predicate is data — a `vuka.Pred[T]` with an `Op` (`vuka.OpEq`,
`vuka.OpContains`, …), the field's `Path` and the `Value` — combined with
`vuka.And`, `vuka.Or` and `vuka.Not`. A query builder translates it;
`p.Match(t)` evaluates it in memory, and `p.String()` writes it:

::: vuka
```vuka
q := vuka.Or(vuka.And(Post.Title.Contains("go"), Post.Views.Gt(100)), Post.Home.IsNil())
fmt.Println(q)          // ((Title contains "go" AND Views > 100) OR Home IS NIL)
q.Match(post)           // true or false
```
:::

::: go
<!-- go:fragment -->
```go
post := Post{Title: "go generics", Views: 120}
q := vuka.Or(vuka.And(PostTitle.Contains("go"), PostViews.Gt(100)), PostHome.IsNil())
fmt.Println(q)          // ((Title contains "go" AND Views > 100) OR Home IS NIL)
fmt.Println(q.Match(post)) // true
```
:::

Because a predicate is typed by `T`, an API taking `vuka.Pred[Post]` rejects
a predicate on `User` at compile time. A comparison is false when a nil pointer
or relation is on the way to its field (`IsNil` is true).

`Asc()` and `Desc()` give a `vuka.Order[T]`; `vuka.SortBy(xs, orders…)` sorts a
slice by them, stably, the first order deciding.

`p.Path` (and `ref.FieldPath()`) is a `vuka.FieldPath`: `Names()` (`["Author",
"Name"]`), `Fields()` (each step's `reflect.StructField`), `Index()`, `Attrs()`
of the last field, and `Value(root)`.

### What it becomes

::: vuka
```go
vuka.StringRefTo(func(p *Post) *string { return &p.Author.Related().Name })
```

The field by address, the call plain Go writes: the compiler checks the
fields and the editor's hover, go to definition and rename land on them.
`RefTo` runs the function once per instantiation, on a value of `Post` made
for it, and finds the field the address is of; `OrderedRefTo`,
`StringRefTo`, `CompareRefTo` and `NullableRefTo` are the other kinds. An unknown field is an error
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
:::

::: go
A `.vuka` file's `Post.Author.Name` is this same call; Go code and `.vuka`
code share the references and predicates. A function that returns the
address of something other than a field of its argument panics when the
reference is made, at init for a package-level variable.
:::
