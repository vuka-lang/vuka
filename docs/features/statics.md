# Statics and Self

**Go:** package-level variables and functions (`NewUser`, `DefaultClient`) do
what statics do elsewhere.

**Vuka adds** statics that belong to a type, as in Kotlin or Java:

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

They become package-level Go — `User_Table`, `_User_created`, `func User_New` —
which is also how Go code reaches them.

An initializer is Vuka code like any other: it may use the type's other
statics and static methods, those it reaches through embedding, and
[field references](/features/fields):

```vuka
type Post struct {
	orm.Model
	Title string
	Views int

	static Limit   = 10
	static Twice   = Post.Limit * 2
	static ByViews = Post.Views.Desc()
	static Popular = Post.Objects.Filter(Post.Views.Gt(Post.Limit))
}
```

Statics are initialized in dependency order, whatever order they are written
in, as Go initializes package-level variables; a generic type's statics are
initialized on first use. A static that depends on itself — directly, through
other statics, or through the functions and methods its initializer calls —
is an error:

```text
main.vuka:6:9: initialization cycle: static Post.A depends on itself: Post.A → Post.B → Post.A
```

## Through embedding, with Self

A static on a generic type has one value per instantiation, and a type reaches
the statics of the types it embeds. With a base type whose type parameter is
named `Self`, Vuka fills in the embedding type, and methods taking `self *Self`
get the whole outer value:

```vuka
// package orm
type Model[Self any] struct {
	static Objects = Manager[Self]{}
}

func (m *Model[Self]) Save(self *Self, ctx context.Context) error { … }

// your models
type User struct {
	orm.Model               // = orm.Model[User]
	Name string
}

users := User.Objects.All(ctx)   // the User manager, from orm.Model
err := u.Save(ctx)               // Save gets u itself as self
```

A Django-style model API, in plain Go underneath: `User.Objects` becomes
`orm.Model_Objects[User]().V`, and `u.Save(ctx)` becomes `u.Save(u, ctx)`.

A static can't share a name with a field or method (in Go, `User.Save` already
means a method).
