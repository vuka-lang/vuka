# Statics and Self

**Go:** package-level variables and functions (`NewUser`, `DefaultClient`) do
what statics do elsewhere.

**Vuka adds** statics that belong to a type, as in Kotlin or Java:

::: vuka
```vuka
type User struct {
	Name string

	static Table   = "users"
	static created int            // private: lowercase, as in Go
	static const Max = 100
}

func User.New(name string) *User {
	User.created++
	return &User{Name: name}
}

u := User.New("ada")
fmt.Println(User.Table, User.Max)
```

They become package-level Go — `UserTable`, `userCreated`, and for the
constructor `func NewUser`, Go's convention — which is also how Go code
reaches them.
:::

::: go
In Go they are package-level declarations named after the type — the names a
`.vuka` file's statics compile to, too:

```go
type User struct {
	Name string
}

var UserTable = "users"

var userCreated int

const UserMax = 100

func NewUser(name string) *User {
	userCreated++
	return &User{Name: name}
}
```

<!-- go:fragment -->
```go
u := NewUser("ada")
fmt.Println(u.Name, UserTable, UserMax)
```
:::

::: vuka
An initializer is Vuka code like any other: it may use the type's other
statics and static methods, those it reaches through embedding, and
[field references](/features/fields):

```vuka
type Post struct {
	Title string
	Views int

	static Limit   = 10
	static Twice   = Post.Limit * 2
	static ByViews = Post.Views.Desc()           // a vuka.Order[Post]
	static Popular = Post.Views.Gt(Post.Limit)   // a vuka.Pred[Post]
}

vuka.SortBy(posts, Post.ByViews)
Post.Popular.Match(posts[0])
```

Statics are initialized in dependency order, whatever order they are written
in, as Go initializes package-level variables; a generic type's statics are
initialized on first use. A static that depends on itself — directly, through
other statics, or through the functions and methods its initializer calls —
is an error:

```text
main.vuka:6:9: initialization cycle: static Post.A depends on itself: Post.A → Post.B → Post.A
```
:::

::: go
An initializer may use the other package-level values, and
[field references](/features/fields) made with `vuka.OrderedRefTo` and its
kin:

```go
type Post struct {
	Title string
	Views int
}

var (
	PostLimit   = 10
	PostTwice   = PostLimit * 2
	postViews   = vuka.OrderedRefTo(func(p *Post) *int { return &p.Views })
	PostByViews = postViews.Desc()         // a vuka.Order[Post]
	PostPopular = postViews.Gt(PostLimit) // a vuka.Pred[Post]
)

func popularFirst(posts []Post) bool {
	vuka.SortBy(posts, PostByViews)
	return len(posts) > 0 && PostPopular.Match(posts[0])
}
```

Go initializes package-level variables in dependency order, whatever order
they are written in, and reports one that depends on itself as an
initialization cycle.
:::

## Through embedding, with Self

::: vuka
A static on a generic type has one value per instantiation, and a type reaches
the statics of the types it embeds. With a base type whose type parameter is
named `Self`, Vuka fills in the embedding type, and methods taking `self *Self`
get the whole outer value:

```vuka
type Store[T any] struct{ rows []*T }

func (s *Store[T]) Add(x *T)  { s.rows = append(s.rows, x) }
func (s *Store[T]) All() []*T { return s.rows }

// a reusable base
type Model[Self any] struct {
	static Objects = Store[Self]{}
}

func (m *Model[Self]) Save(self *Self) { Model[Self].Objects.Add(self) }

// a type using it
type User struct {
	Model               // = Model[User]
	Name string
}

u := &User{Name: "ada"}
u.Save()                    // Save gets u itself as self
users := User.Objects.All() // User's own store, from Model
```

Plain Go underneath: `User.Objects` becomes `ModelObjects[User]().V`, and
`u.Save()` becomes `u.Save(u)`. `Model[User]` written out works the same;
`Self` is filled in only for a type parameter of that name.

A generic type of another package may also give a static as a generic
function of the static's own name, called with the embedding type: the
[ORM](/orm/models)'s `orm.Base` has no `Objects` field, so `Post.Objects`
is `orm.Objects[Post]()`, the model's manager — what Go code writes too.

A generic type's static takes its type from its initializer, like any other:
`&Store[Self]{}`, `new(Store[Self])`, `[]Self{}`, `map[string]*Self{}`,
`NewStore[Self]()` or `len(Model[Self].Objects.All())` all work, with `Self`
standing for each instantiation. An initializer with no type of its own needs
one written out:

```vuka
type Model[Self any] struct {
	static Objects = NewStore[Self]()       // a *Store[Self]
	static Cache   map[string]*Self = nil   // nil alone has no type
}
```

Written as `static Cache = nil`, it is an error:

```text
main.vuka:3:9: can't infer the type of static Model.Cache from nil; write it: static Cache T = nil
```

A static can't share a name with a field or method (in Go, `User.Save` already
means a method).
:::

::: go
Go has no package-level variable per instantiation of a generic type, so a
value per type is a generic function: `vuka.StaticOf` keeps one value for
each type argument, built on first use. A method of the embedded base that
needs the outer value takes it as a parameter:

```go
type Store[T any] struct{ rows []*T }

func (s *Store[T]) Add(x *T)  { s.rows = append(s.rows, x) }
func (s *Store[T]) All() []*T { return s.rows }

// a reusable base
type Model[T any] struct{}

var modelObjects vuka.Statics

// ModelObjects is T's own store.
func ModelObjects[T any]() *Store[T] {
	return vuka.StaticOf[Model[T]](&modelObjects, func() *Store[T] { return &Store[T]{} }).V
}

func (m *Model[T]) Save(x *T) { ModelObjects[T]().Add(x) }

// a type using it
type Member struct {
	Model[Member]
	Name string
}
```

<!-- go:fragment -->
```go
m := &Member{Name: "ada"}
m.Save(m)
members := ModelObjects[Member]().All()
fmt.Println(len(members))
```

This is the shape of the [ORM](/orm/models)'s managers: a model embeds
`orm.Model[Post]`, and `orm.Objects[Post]()` is its manager.
:::
