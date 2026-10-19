# Dependency injection

**Go:** dependencies are passed to constructors by hand, or wired by a container
such as [fx](https://github.com/uber-go/fx) from constructor functions you write.

**Vuka adds** those constructors for you. Mark a struct with a type decorator;
its fields are its dependencies — no annotations on fields.

<!-- go:context
type DB struct{}

type UserService struct{ db *DB }

func NewDB() *DB { return &DB{} }

func NewUserService(db *DB) *UserService { return &UserService{db: db} }
-->

::: vuka
```vuka
@di.Component
type OrderService struct {
	db    *DB
	users *UserService
	mu    sync.Mutex `inject:"-"`   // not a dependency
}
```

The decorator receives `t.New`, a generated constructor
`func(db *DB, users *UserService) *OrderService` — the shape `fx.Provide`,
`dig.Provide` and the [web framework](/web/dependency-injection)'s container
take — so a container wires the struct from parameter types, unexported fields
included, with no reflection on fields.

The whole integration with a container is a few lines:

```vuka
package di

var providers []any

func Component(t *vuka.Type) { providers = append(providers, t.New) }

func Module() fx.Option { return fx.Provide(providers...) }
```

## Rules

- Every field is injected except `_`, fields tagged `inject:"-"`, and embedded
  values (embedded pointers and interfaces are injected).
- A struct that declares its own static `New` is built with that one.
- Otherwise the generated constructor is the struct's static `New`, handy in
  tests: `OrderService.New(fakeDB, fakeUsers)`.
- `t.Fields` lists the fields, their tags, their
  [attributes](/features/fields#field-attributes), and whether each is
  injected.
:::

::: go
In Go the constructor is yours: its parameters are the dependencies, and it
is the shape `fx.Provide`, `dig.Provide` and the
[web framework](/web/dependency-injection)'s container take, so the
container wires the struct from parameter types, unexported fields
included. A field the constructor doesn't take, such as a mutex, isn't a
dependency.

```go
type OrderService struct {
	db    *DB
	users *UserService
	mu    sync.Mutex
}

func NewOrderService(db *DB, users *UserService) *OrderService {
	return &OrderService{db: db, users: users}
}

func init() {
	web.Routes.Service(NewOrderService) // a lazy singleton, for every app
}
```

Or, for one app, give the constructors to it:

<!-- go:fragment -->
```go
app := web.New(web.Provide(NewDB, NewUserService, NewOrderService))
```

In tests, call the constructor with fakes: `NewOrderService(fakeDB, fakeUsers)`.
:::
