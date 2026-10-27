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

import (
	"context"
	"time"
)

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

import (
	"context"

	"github.com/vuka-lang/web/v2/auth"
	"github.com/vuka-lang/web/v2/graphql"
)

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
| an integer or a string named `id`, or ending in `ID` or `Id` (`OwnerID`, `userId`, `tagIDs`); a field tagged `graphql:",id"` | `ID` (see [IDs](#ids)) |
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
error at its field; identifiers are `ID`s ([IDs](#ids)). A map, a channel, a function,
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
import (
	"github.com/vuka-lang/web/v2/auth"
	"github.com/vuka-lang/web/v2/graphql"
)

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

### IDs {#ids}

An id travels as an `ID`: written as a string, `"7"`, and read back as the
string or the integer, `"7"` or `7`, so a client sends back exactly what it
received. An integer or string argument, input field or object field is an
`ID` when its Go name says it is an id: `id`, `ID`, or a name ending in `ID`
or `Id` (and their plurals for a slice, `tagIDs`). Its resolver gets the Go integer;
a value that isn't a whole number in range is refused, as a variable's or
an argument's value of the wrong type. Overrides go both ways:

| | Force `ID` | Keep `Int` |
|---|---|---|
| a parameter | `@graphql.AsID("owner")` on the function or method | `@graphql.AsInt("lastSeenID")`, `@graphql.AsString("trackingID")` |
| a struct field (input or output) | `graphql:",id"` | `graphql:",int"`, `graphql:",string"` |

```vuka
import "github.com/vuka-lang/web/v2/graphql"

// pet(id: ID): the id a query gave, sent back.
@graphql.Query("pet")
func FindPet(id int64, store *Store) (*Pet, error) { … }

// owned(owner: ID, minVisitID: Int)
@graphql.Query
@graphql.AsID("owner")
@graphql.AsInt("minVisitID")
func Owned(owner int64, minVisitID int, store *Store) ([]Pet, error) { … }

type Visit struct {
	ID       int64                    // ID!
	PetID    int64                    // ID!
	ClinicID int   `graphql:",int"`   // Int!: a clinic's number, not a reference
}
```

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
import "github.com/vuka-lang/web/v2/auth"

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

## Loaders {#loaders}

A field method resolves once for each object it is a field of, so a list
of a hundred users whose `orders` field queries the database runs a hundred
queries: an N+1. A loader turns them into one. It is a function from keys
to values, marked `@graphql.Loader`:

```vuka
// shop/orders.vuka
package shop

import (
	"context"

	"github.com/vuka-lang/orm/v2"
	"github.com/vuka-lang/web/v2/graphql"
)

// OrdersByUser is each user's orders.
@graphql.Loader
func OrdersByUser(ctx context.Context, userIDs []int64, db *DB) (map[int64][]Order, error) {
	rows := Order.Objects.Filter(orm.Q{"user__in": userIDs}).All(ctx)?
	out := map[int64][]Order{}
	for _, o := range rows {
		out[o.User.ID()] = append(out[o.User.ID()], o)
	}
	return out, nil
}

// Orders are the user's orders.
func (u User) Orders(ctx context.Context, orders OrdersByUser) ([]Order, error) {
	return orders.Load(ctx, u.ID)
}
```

`OrdersByUser` is then a type — the loader — and the field method takes it
as a dependency, as it takes a service. A request makes one of each loader
type its fields and operations take, with the function's dependencies, the
first time one does. What it does with the keys:

- **One call a level.** A key asked for waits until every field that can
  ask for one is waiting too, then the function is called once with every
  key asked for meanwhile: `users { orders { … } }` runs the users' query
  and one orders query, however many users. Its keys come in the order of
  the fields asking for them in the response.
- **Kept for the request.** A key loaded once — by another field, at
  another level — is answered from what was loaded.
- **Missing keys.** A key the map leaves out loads as the value type's zero
  value: nil for a pointer or a slice (an empty list), `""` for a string.
- **Chunks.** More than 1000 keys are split into calls of 1000;
  `@graphql.Loader{Chunk: 200}` sets the size.
- **Errors.** The function's error is each waiting field's, at its own
  path; a panic is an internal error, as a resolver's is.

`orders.LoadMany(ctx, ids)` loads several keys together, in their order.
Outside GraphQL a loader is made with its constructor, `NewOrdersByUser(db)`
(the compiler writes it beside the type): a live page's `Mount` or a route
can use one, where there is no level to wait for — `Load` calls the
function for its key at once, `LoadMany` for its keys together — and a
loader keeps what it loads for as long as you keep it.

In Go the function keeps its body as `fetchOrdersByUser`, and the type is

```vuka
type OrdersByUser struct {
	*graphql.LoaderOf[int64, []Order]
}
```

listed in the package's registration (`graphql.LoaderDef`). A loader's
function takes the context, a slice of keys of a comparable type, and
dependencies (pointers and interfaces), and returns a map from those keys
and an error ([E0805](/errors/E0805) otherwise).

`vuka vet` warns about a field method of a type a list reaches that queries
the orm itself, without a loader ([W0801](/errors/W0801)).

## Relations {#relations}

A field of an orm model holding a relation needs no loader of yours: it
loads through one already. The relation is loaded into every object of a
level that needs it at once (`orm.LoadRelation`), one `IN (…)` query per
level, and its rows are the field's value:

| The model's field | The GraphQL field |
|---|---|
| `Author orm.FK[User]` | `author: User`, null when the key isn't set |
| `Tags orm.M2M[Tag]` | `tags: [Tag!]!` |
| a Go model's `Author *User` (a foreign key) | `author: User` |
| a Go model's `Books []Book` (rows held by many) | `books: [Book!]!` |

```vuka
type Post struct {
	orm.Base
	ID     int64
	Author orm.FK[User] @orm.Rel{Related: "posts"}
	Tags   orm.M2M[Tag]
	Title  string
}
```

`{ posts(first: 50) { title author { name } tags { name } } }` runs three
queries: the posts, their authors, their tags. A relation the query loaded
itself (`SelectRelated`, `PrefetchRelated`) is used as it is. A field tagged
`graphql:",noload"` reads only that: null or an empty list when the query
didn't load it.

Reverse relations — the posts of a user — have no field type on a Vuka
model: give the user a field method taking a loader, as above.
`orm.LoadRelated(ctx, rows, "author", "tags")` loads relations into rows
read already, in Go code of your own.

## Pagination {#pagination}

A list too long for one response is a connection: a page of edges, each a
node and its cursor, and what lies around the page. Return a
`graphql.Connection[T]` and take a `graphql.Page`:

```vuka
import "github.com/vuka-lang/web/v2/graphql"

// Users pages the users, by name.
@graphql.Query
func Users(ctx context.Context, page graphql.Page) (graphql.Connection[User], error) {
	return graphql.Paginate(ctx, User.Objects.OrderBy("name"), page)
}
```

```graphql
type Query {
  users(first: Int, after: String, last: Int, before: String): UserConnection!
}

type UserConnection {
  edges: [UserEdge!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

type UserEdge {
  cursor: String!
  node: User!
}

type PageInfo {
  hasNextPage: Boolean!
  hasPreviousPage: Boolean!
  startCursor: String
  endCursor: String
}
```

A client reads forwards with `first` and `after` (the last page's
`endCursor`), backwards with `last` and `before`. Neither reads the first
10 (`graphql.DefaultPageSize`); more than 100 (`graphql.MaxPageSize`), a
negative size, `first` and `last` together, or a cursor of another list are
the client's errors (`BAD_USER_INPUT`). `totalCount` counts the list only
when a query asks for it. `hasPreviousPage`, reading forwards, is whether
the page starts after a cursor, and `hasNextPage`, reading backwards,
whether it ends before one: neither is a query of its own.

`graphql.Paginate` takes an orm query set (or anything with its
`CursorPage` and `Count`). Its cursors are opaque, and work by the query's
ordering — `Meta`'s `Ordering` when it sets none:

- **Keyset cursors** when the ordering is the model's own non-null columns:
  the primary key is added as the last column unless one of them is unique
  (a query without an ordering pages by the primary key), and a page is the
  rows past the cursor's values in that order — `WHERE (name, id) > (…)`,
  written out — so rows written meanwhile don't shift it, and it costs the
  same on page 1 and page 1000.
- **Offset cursors** otherwise — an ordering by an expression, an
  annotation, a related model's field or a nullable column: a cursor is the
  row's position in the result (`LIMIT … OFFSET …`).

A cursor made for another ordering is refused. `graphql.Slice(rows)` pages
a slice in memory. In the query's complexity a connection's edges count as
many as its `first` or `last` asks for.

`vuka vet` warns about an operation or field method returning a list
without a `first`, `last` or `limit` argument or a page
([W0802](/errors/W0802)).

## Limits {#limits}

Every request is measured before anything runs, and refused with an error
naming the limit when it goes over one. The limits are on by default:

| `graphql.Limits` | Default | |
|---|---|---|
| `Depth` | 10 | how deeply selections nest |
| `Complexity` | 1000 | each field costs 1; a list field's subtree is multiplied by its `first`, `last` or `limit` argument, else by `DefaultListSize`; a connection's edges by the connection's `first` or `last` |
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

## In the Toolbar {#toolbar}

In development each GraphQL request a page sends is a call in the
[toolbar](/web/toolbar#every-call-the-page-makes)'s list: its operation and
timing, its errors with their paths, its statements and timeline, and a
**GraphQL** panel — every field that resolved through a resolver of its
own, with its path, when it started and how long it took, and how many
statements it ran; every loader batch with its keys. The SQL panel names
the field (or loader) behind each statement, and an N+1 the field
repeating it.

The **GraphiQL** panel runs queries against the app, in an editor that
knows its schema (read by introspection, once per page, and again when the
app rebuilds):

- **Completion at the cursor**, as you type and on Ctrl+Space: the fields
  of the type at the cursor (in nested selections, fragments and inline
  fragments on interfaces and unions), arguments with their types and
  required ones marked, enum values, an input object's fields inside its
  `{ }`, the operation's variables of the type wanted (`$`), directives,
  fragment names, `on` and the operation keywords. Each item shows its type
  and description; matching is fuzzy (`usr` finds `user` and `users`); the
  arrow keys move, Enter or Tab accepts, Esc closes. A field with required
  arguments comes with `()` and the cursor inside, offering them; an object
  field with `{ }` on lines of its own.
- **Highlighting** of keywords, fields, aliases, arguments, types,
  variables, strings, numbers, enum values, directives and comments;
  matching brackets; brackets and quotes closed as they open; indentation
  kept and deepened after `{`; Ctrl/Cmd+/ comments lines in or out.
- **The app's own checks as you type**: the endpoint's parser, validation,
  limits and variable coercion run on each pause (nothing executes, nothing
  is recorded), their errors underlined where they are and listed under the
  editor — click one to go there, and an unknown name's "Did you mean"
  becomes a button putting it right.
- **Hover** on a field, argument, type, enum value, directive or variable
  shows its type and description; Ctrl/Cmd+click opens it in the docs.
- **Formatting**: the Format button, Shift+Ctrl/Cmd+P or Shift+Alt+F
  formats the document — or only the definitions the selection touches —
  in the canonical style: two-space indentation, a field a line, arguments,
  variables, input objects and lists on one line while it fits in 80
  columns and one a line past that; comments and blank lines kept, strings
  and block strings unchanged. Formatting twice changes nothing, the cursor
  stays on its token, and Ctrl/Cmd+Z undoes it in one step. The variables
  and headers are formatted with it (or alone, from their editors), and a
  document that doesn't parse says where.
- **Variables and headers**, JSON editors of their own: highlighted, their
  syntax checked as you type, the variables completing the operation's
  declared variables (with their input types), an input object's fields and
  enum values, and the app's coercion errors marked on the value at fault.
- **Running**: Ctrl/Cmd+Enter runs the operation at the cursor (a document
  may hold several; the picker beside Run chooses another), with the
  headers given. The result is highlighted JSON whose objects and lists
  fold; above it the status, the time and the size, and the errors with
  their paths and locations (click one to go there). Each run is a call of
  the list, its record's GraphQL panel the run's trace ("Show its trace").
  "Copy as curl" copies the request.
- **Tabs and history**: documents are tabs, kept with their variables and
  headers in the browser's storage for the app, and the queries run are its
  history, a click away from a tab of their own.
- **The explorer** (the Explorer button): **Operations** lists every field
  of Query, Mutation and Subscription, with their counts, a filter, and
  each one's arguments, type and description; Insert puts a ready-to-run
  operation at the cursor, New tab in a tab of its own — the keyword, a
  name, a variable for each argument (`$id: ID!`) wired to it, and a
  selection of the field's scalars (and its objects' `id`), the cursor on
  that selection and the required variables in the variables. The panel
  can't run a subscription, and says so. **Docs** walks the schema from its
  roots — types, fields with their arguments, descriptions and
  deprecations, input fields, enum values, possible types — with a search
  across types and fields; `+` beside a field inserts it at the cursor.
- **Full screen**: the Full screen button, or Shift+Ctrl/Cmd+F, gives the
  panel the whole window and back, keeping the editors as they were; Esc
  leaves it. The choice is remembered for the app.

![The GraphiQL panel](/toolbar-graphiql.png)

A browser opening the endpoint itself in development
(`http://localhost:8080/graphql`) gets a page whose toolbar opens on the
panel, for an app serving no page of its own. None of it exists outside
development: the editor's script is part of the toolbar, which a production
app doesn't serve, and the toolbar loads it only when the panel first
shows.

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
