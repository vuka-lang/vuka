# Dependency injection

**Go:** dependencies are passed to constructors by hand, or wired by a container
such as [fx](https://github.com/uber-go/fx) from constructor functions you write.

**Vuka adds** those constructors for you. Mark a struct with a type decorator;
its fields are its dependencies — no annotations on fields.

```vuka
@di.Component
type OrderService struct {
	db    *DB
	users *UserService
	mu    sync.Mutex `inject:"-"`   // not a dependency
}
```

The decorator receives `t.New`, a generated constructor
`func(db *DB, users *UserService) *OrderService` — the shape `nexus.Provide`,
`fx.Provide` and `dig.Provide` take — so a container wires the struct from
parameter types, unexported fields included, with no reflection on fields.

The whole integration with a container is a few lines:

```vuka
package di

var providers []any

func Component(t *vuka.Type) { providers = append(providers, t.New) }

func Module() nexus.Option { return nexus.Provide(providers...) }
```

## Rules

- Every field is injected except `_`, fields tagged `inject:"-"`, and embedded
  values (embedded pointers and interfaces are injected).
- A struct that declares its own static `New` is built with that one.
- Otherwise the generated constructor is the struct's static `New`, handy in
  tests: `OrderService.New(fakeDB, fakeUsers)`.
- `t.Fields` lists the fields, their tags, and whether each is injected.
