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

## Through embedding, with Self

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

Plain Go underneath: `User.Objects` becomes `Model_Objects[User]().V`, and
`u.Save()` becomes `u.Save(u)`. `Model[User]` written out works the same;
`Self` is filled in only for a type parameter of that name. The
[ORM](/orm/models)'s `orm.Base` is built this way.

A static can't share a name with a field or method (in Go, `User.Save` already
means a method).
