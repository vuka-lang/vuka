# GraphQL

[[toc]]

## Introduction

A GraphQL API lets a client — a mobile app, a partner's integration, an
island fetching what it shows — ask for exactly the fields it needs, in one
request, and learn from the schema what it may ask.

In Vuka the schema is your Go. Types are structs, fields are their fields
and methods, and operations are functions marked `@graphql.Query` or
`@graphql.Mutation`. The compiler reads them and generates the schema and
a resolver for every field: plain Go calling your functions and reading
your fields, with no reflection when a request resolves. Validation, auth
and dependency injection work as they do for routes and forms, so one input
struct serves an HTML form and a mutation alike.

## Your First Schema

Types can live in any package, in `.go` or `.vuka` files. These are plain
Go:

```vuka
// shop/types.go
package shop

// Pet is an animal for sale.
type Pet struct {
	ID    int64 `graphql:",id"`
	Name  string
	Kind  Kind
	Tag   *string
	Born  time.Time
	notes string // unexported: not in the schema
}

type Kind string

const (
	KindDog Kind = "dog"
	KindCat Kind = "cat"
)

// Age is the pet's age in whole years at a date.
func (p Pet) Age(at string) (int, error) { … }
```

Operations are functions in a `.vuka` file, marked with an attribute:

```vuka
// shop/pets.vuka
package shop

// Pets lists the pets of a kind, at most first.
@graphql.Query
func Pets(ctx context.Context, kind *Kind, first int, store *Store) ([]Pet, error) {
	return store.Pets(kind, first), nil
}

@graphql.Mutation
@auth.Requires("pets.add")
func AddPet(ctx context.Context, in NewPet, store *Store) (*Pet, error) { … }
```

`main` mounts the endpoint, with the app's other options:

<!-- go:fragment -->
```go
app := web.New(web.GraphQL("/graphql"))
_ = app
```

The schema it serves, as `vuka graphql schema` prints it:

```graphql
type Query {
  """Pets lists the pets of a kind, at most first."""
  pets(kind: Kind, first: Int): [Pet!]!
}

type Mutation {
  addPet(in: NewPet): Pet
}

"""Pet is an animal for sale."""
type Pet {
  id: ID!
  name: String!
  kind: Kind!
  tag: String
  born: DateTime!
  """Age is the pet's age in whole years at a date."""
  age(at: String): Int!
}

enum Kind {
  DOG
  CAT
}
```

And a request:

```sh
curl localhost:8080/graphql -H 'Content-Type: application/json' \
  -d '{"query": "{ pets(first: 2) { name age(at: \"2026-01-31\") } }"}'
```

```json
{"data":{"pets":[{"name":"Rex","age":5},{"name":"Tom","age":4}]}}
```

## Types {#types}

Every Go type an operation reaches — its parameters and its result, their
fields and methods, and theirs — becomes a type of the schema:

| Go | GraphQL |
|---|---|
| a struct | an object type (an input type where an argument takes it), named after the struct |
| an exported field | a field; unexported fields and fields tagged `graphql:"-"` are left out |
| an embedded struct | its fields, in its place, as Go promotes them |
| a method | a field with arguments (see [Field Methods](#field-methods)) |
| `string`, `bool`, `float64`, the integer types | `String`, `Boolean`, `Float`, `Int` |
| a field tagged `graphql:",id"` (a string or an integer) | `ID` |
| `time.Time` | `DateTime`, written in RFC 3339 |
| a type with `MarshalText` and `UnmarshalText` | a custom scalar named after the type |
| a vuka `enum`, or a type with constants | an enum, its values the constants |
| a slice | a list |
| a pointer | nullable |

Outputs are non-null unless they are pointers or tagged
`graphql:",omitempty"` (which gives null for a zero value or an empty
list). Inputs are always nullable: an argument or input field the request
leaves out, or gives null, is its type's zero value — nil for a pointer,
which tells "not given" from zero.

Integers are `Int`, which GraphQL limits to 32 bits; a larger value is an
error at its field. Use `ID` for identifiers. A map, a channel, a function,
an unnamed struct and `time.Duration` have no GraphQL type: an operation
reaching one is a compile error ([E0802](/errors/E0802)) naming the path to
it. Tag such a field `graphql:"-"`.

### Field Methods {#field-methods}

An exported method declared on the type (not promoted from another
package's) is a field when its signature fits: a value, or a value and an
error (or a `vuka.Result[T]`), from parameters that are

- the context, `context.Context`;
- arguments: scalars, enums, input structs and slices of them, named by
  the parameter's name; a pointer to a scalar is optional;
- dependencies: pointers and interfaces, filled by the app's container as
  a handler's are (see [Dependency Injection](/web/dependency-injection)).

```vuka
// Orders are the user's orders, the newest first.
func (u User) Orders(ctx context.Context, first int, db *DB) ([]Order, error) { … }
```

is `orders(first: Int): [Order!]!` on `User`. Methods that don't fit —
`Save(ctx) error`, `String() string`, `MarshalText`, `Clean` — aren't
fields; `@graphql.Field` on one says it is, and makes a signature that
doesn't fit an error.

## Operations {#operations}

`@graphql.Query` makes a function an operation of the `Query` type,
`@graphql.Mutation` of `Mutation`: named after the function in lower camel
case (`AddPet` is `addPet`), its parameters as a field method's are, its
result the field's type. A request's mutations run one after the other, in
the order the document writes them; a query's fields at one level resolve
concurrently.

Operations are declared on functions in `.vuka` files — any package the
program imports; the `vuka` command links a package declaring operations
into the program by itself. A page's script and a method can't declare one
([E0805](/errors/E0805)). The compiler appends the package's registration
to its Go: `graphql.Register(graphql.Package{…})`, the operations and every
type they reach (see [The Generated Go](#the-generated-go)). Plain Go
declares the attributes the way generated code does,
`vuka.RegisterFunc(Pets, graphql.Query(""))`, and the registration by
hand.

Decorators stack top down, one per line, above the function:

```vuka
@graphql.Mutation("addPet")
@graphql.Arg("in", "pet")
@auth.Requires("pets.add")
func CreatePet(ctx context.Context, in NewPet, store *Store) (*Pet, error) { … }
```

`@graphql.Subscription` comes in a later version and is an error for now
([E0806](/errors/E0806)).

### Mounting {#mounting}

`web.GraphQL(path, options…)` serves the app's one schema at `path`, by GET
and POST:

| Option | |
|---|---|
| `graphql.Naming{Fields: …}` | the names fields and arguments take by default ([Naming](#naming)) |
| `graphql.Limits{…}` | the limits on a request ([Limits](#limits)) |
| `graphql.Introspection(true)` | allows `__schema` and `__type` outside development |

The route runs the app's middleware, CSRF protection, request context and
toolbar like any route. The app fails to build when nothing is linked to
serve ([E0801](/errors/E0801)), and when a dependency an operation or field
takes isn't provided ([E0407](/errors/E0407)), as a handler's would.

## Naming {#naming}

Every name has a default from Go and an override for a specific wire name:

| What | Default | Override |
|---|---|---|
| Query, mutation | the function's name, lower camel: `CreateUser` is `createUser` | `@graphql.Query("usersList")`, `@graphql.Mutation("…")` |
| Object type | the Go type's name | `@graphql.Type("Person")` on the type |
| Input type | the struct's name | `@graphql.Input("CreateUserInput")` on the type |
| Field from a struct field | lower camel: `CreatedAt` is `createdAt` | a `graphql:"joinedOn"` tag |
| Field from a method | lower camel: `FullName` is `fullName` | `@graphql.Field("displayName")` on the method |
| Argument | the Go parameter's name | `@graphql.Arg("after", "cursor")` on the function or method; a tag on an input struct's field |
| Enum value | the constant's name in SCREAMING_SNAKE case, its type's name dropped: `StatusPastDue` is `PAST_DUE` | `@graphql.Value("BEHIND")` on the constant |
| Generic instantiation | its type arguments' names first: `Page[User]` is `UserPage` | `@graphql.Type` on a named instantiation |

`graphql.Naming{Fields: graphql.SnakeCase}` names every field and argument
without a written name in snake case (`created_at`); a written name always
wins. `@graphql.Deprecated("use displayName")` on a field, a method, an
operation or a constant marks it deprecated without renaming it.

Two declarations arriving at one name — `User` types of two packages, two
packages' `Pets` — are an error naming both, with the override to use
([E0803](/errors/E0803)): at compile time for what one package declares,
when the app builds for names meeting across packages. A name starting with
`__` is refused, as the specification reserves those
([E0804](/errors/E0804)). A rename is a breaking change; `vuka graphql
schema -check` reports it.

## Inputs and Errors {#inputs}

An argument of a struct type is an input object, and a form: the same
struct a page's `<ui.Form>` submits, checked the same way — its `validate:`
tags, then each field's `Clean<Field>` method, then `Clean`, their
dependencies injected — before the operation runs (see
[Forms](/ui/forms)).

```vuka
type NewPet struct {
	Name  string  `validate:"required,max=12"`
	Price float64 `validate:"min=1"`
}

func (p *NewPet) CleanName(ctx context.Context, store *Store) error {
	if store.Named(p.Name) {
		return forms.Errorf("A pet is called %s already", p.Name)
	}
	return nil
}
```

Each error of the form is one of the response's, its message the one the
form shows, its `extensions` the form error as data — the field's `path` in
the form, the `rule`, the message `key`, the `argument`:

```json
{
  "errors": [{
    "message": "Price must be 1 or more",
    "path": ["addPet"],
    "extensions": {"code": "INVALID_INPUT", "argument": "in", "path": "price",
                   "rule": "min", "key": "validate.min.number", "params": {"n": "1"}}
  }],
  "data": {"addPet": null}
}
```

What a resolver's error becomes:

| The error | Message | `extensions.code` |
|---|---|---|
| a form's checks, `forms.Error`, `web.Invalid` | as written | `INVALID_INPUT` |
| `web.Unauthorized`, `web.Forbidden`, `web.NotFound`, `web.Conflict` (or any `*web.Error`) | as written | `UNAUTHENTICATED`, `FORBIDDEN`, `NOT_FOUND`, `CONFLICT` (`BAD_REQUEST` for another status) |
| any other error, a panic | "internal error"; the full text in the log, and in the response in development | `INTERNAL_SERVER_ERROR` |

The engine's own errors carry `GRAPHQL_PARSE_FAILED`,
`GRAPHQL_VALIDATION_FAILED`, `BAD_USER_INPUT` (a variable or argument of
the wrong type), `BAD_REQUEST`, `TIMEOUT` and the limits' codes. A field's
error makes it null, with the error at its path; a non-null field's null
moves to the nearest nullable field above it, as the specification has it.

## Auth {#auth}

The route decorators gate operations and fields: `@auth.Required`,
`@auth.Requires("pets.add")`, `@auth.RequiresAny`, `@auth.Public`. An
operation without one needs a sign-in, as a route does, unless the app is
public by default (`auth.Config{PublicByDefault: true}`). A field without
one is open to whoever may run its operation.

```vuka
type User struct {
	Name  string
	Email string @auth.Requires("users.email")
}

@auth.Requires("users.admin")
func (u User) Sessions(ctx context.Context, store *Store) ([]Session, error) { … }
```

A refused operation or field is null, with the error at its path:
`UNAUTHENTICATED` for a visitor not signed in, `FORBIDDEN` for one without
the permission. The route itself stays open, so one request may mix public
and gated fields. A gate in an app without `auth.Module` fails its build
([E0404](/errors/E0404)).

## Limits {#limits}

Every request is measured before anything runs, and refused with an error
naming the limit when it goes over one. The limits are on by default:

| `graphql.Limits` | Default | |
|---|---|---|
| `Depth` | 10 | how deeply selections nest |
| `Complexity` | 1000 | each field costs 1; a list field's subtree is multiplied by its `first`, `last` or `limit` argument, else by `DefaultListSize` |
| `DefaultListSize` | 10 | |
| `Aliases` | 30 | aliased fields in a document |
| `Fields` | 500 | field selections in a document |
| `Bytes` | 100000 | the document's size |
| `Timeout` | 10s | the request's execution |

A zero keeps the default, a negative value turns a limit off:

<!-- go:fragment -->
```go
app := web.New(web.GraphQL("/graphql", graphql.Limits{Depth: 6, Complexity: -1}))
_ = app
```

Parsed and validated documents are cached by their hash, so a client
sending the same query again skips both.

Introspection (`__schema`, `__type`) is allowed in development and refused
otherwise unless `graphql.Introspection(true)`; `__typename` always
answers.

## The Transport {#transport}

The endpoint follows GraphQL over HTTP. POST takes a JSON body
(`{"query", "operationName", "variables"}`, `Content-Type:
application/json`); GET takes them in the URL and runs queries only — a
mutation by GET answers 405. The response is
`application/graphql-response+json` when the `Accept` header prefers it,
`application/json` otherwise:

| | `application/json` | `application/graphql-response+json` |
|---|---|---|
| a request executed (data, maybe with errors) | 200 | 200 |
| a document that doesn't parse or validate, bad variables, a limit | 200, errors only | 400, errors only |
| a malformed request: no query, invalid JSON | 400 | 400 |
| another content type | 415 | 415 |
| an `Accept` naming neither | 406 | 406 |

A POST from another site's page is refused with the app's other
cross-origin requests ([CSRF](/web/auth#csrf)); list the origins of a
frontend served elsewhere with `web.CSRF("https://app.example.com")`.

## The Schema Command {#schema-command}

`vuka graphql schema` prints the SDL the app serves: it builds the app's
main package and runs it until the schema is built, nothing served and no
service started. Commit it, and check it in CI:

```sh
vuka graphql schema -o schema.graphql        # write it
vuka graphql schema -check schema.graphql    # compare; fail on a breaking change
```

`-check` prints every change and fails on one that breaks clients: a type,
field, argument or enum value removed or renamed, a field's type changed,
an output made nullable, an argument or input field made non-null, a
required argument added.

```text
BREAKING Field Pet.price changed type from Float! to Float.
```

In development each GraphQL request a page sends is a call in the
[toolbar](/web/toolbar#every-call-the-page-makes)'s list: its operation and
timing, its errors with their paths, its statements and timeline.

## The Generated Go {#the-generated-go}

The registration the compiler appends is readable Go, and what an ejected
package keeps:

```vuka
func init() {
	graphql.Register(graphql.Package{
		Path: "example.com/app/shop",
		Operations: []graphql.Operation{{
			Go: "Pets", Func: Pets,
			Doc:    "Pets lists the pets of a kind, at most first.",
			Params: []graphql.Param{{Name: "ctx"}, {Name: "kind", Type: graphql.EnumOf[Kind]()}, {Name: "first", Type: graphql.Int}, {Name: "store", Dep: reflect.TypeFor[*Store]()}},
			Type:   &graphql.NonNull{Of: graphql.ListOf[Pet](&graphql.NonNull{Of: graphql.ObjectOf[Pet]()})},
			Resolve: func(ctx context.Context, c *graphql.Call) (any, error) {
				return Pets(ctx, graphql.GetPtr[Kind](c, 1), graphql.Get[int](c, 2), graphql.Get[*Store](c, 3))
			},
		}},
		Types: []graphql.Binding{
			graphql.Struct[Pet]{Doc: "Pet is an animal for sale.", Fields: []graphql.Member[Pet]{
				{Go: "ID", Type: &graphql.NonNull{Of: graphql.IntIDOf[int64]()}, Get: func(p *Pet) any { return p.ID }},
				{Go: "Name", Type: &graphql.NonNull{Of: graphql.String}, Get: func(p *Pet) any { return p.Name }},
				…
			}},
			graphql.Consts[Kind]{Values: []graphql.Const[Kind]{{Go: "KindDog", Value: KindDog}, {Go: "KindCat", Value: KindCat}}},
		},
	})
}
```

Names aren't in it: the app decides them when it builds, from its
`graphql.Naming` and the attributes on each declaration.
