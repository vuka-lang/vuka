# Examples

The language examples are in Vuka's
[`examples`](https://github.com/vuka-lang/vuka/tree/main/examples) directory;
run one with `vuka run ./examples/<name>`. The [library](#libraries) examples
are in the [web](https://github.com/vuka-lang/web) and
[orm](https://github.com/vuka-lang/orm) repositories; run one with
`vuka run .` in its directory.

::: go
The examples are written in Vuka; each sample below shows its Go form.
Two are whole apps in plain Go, built and run with `go run .`:
[`examples/conformance/library-users`](https://github.com/vuka-lang/vuka/tree/main/examples/conformance/library-users)
(routes, a service, live pages, an island and the ORM; `decorated/` is the
same app in Vuka) and
[`examples/islands-go`](https://github.com/vuka-lang/vuka/tree/main/examples/islands-go).
:::

## users — Result, ?, match

[`examples/users`](https://github.com/vuka-lang/vuka/tree/main/examples/users)

::: vuka
```vuka
func lookup(arg string) Result[User] {
	id := strconv.Atoi(arg)?
	u := find(id)?
	return Ok(u)
}

func main() {
	for _, arg := range []string{"1", "2", "3", "x"} {
		match lookup(arg) {
		case Ok(u):
			match nickname(u) {
			case Some(n):
				fmt.Printf("%s aka %s\n", u.Name, n)
			case None:
				fmt.Println(u.Name)
			}
		case Err(&NotFound{ID: id}):
			fmt.Println("missing user", id)
		case Err(e) if errors.Is(e, strconv.ErrSyntax):
			fmt.Println("not a number:", arg)
		case Err(e):
			fmt.Println("error:", e)
		}
	}
}
```
:::

<!-- go:context
type User struct{ ID int; Name, Nick string }
type NotFound struct{ ID int }

func (e *NotFound) Error() string { return fmt.Sprint("no user ", e.ID) }

func find(id int) (User, error)           { return User{}, &NotFound{ID: id} }
func nickname(u User) (string, bool)      { return u.Nick, u.Nick != "" }
-->

::: go
```go
func lookup(arg string) (User, error) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return User{}, err
	}
	return find(id)
}

func main() {
	for _, arg := range []string{"1", "2", "3", "x"} {
		u, err := lookup(arg)
		var missing *NotFound
		switch {
		case err == nil:
			if n, ok := nickname(u); ok {
				fmt.Printf("%s aka %s\n", u.Name, n)
			} else {
				fmt.Println(u.Name)
			}
		case errors.As(err, &missing):
			fmt.Println("missing user", missing.ID)
		case errors.Is(err, strconv.ErrSyntax):
			fmt.Println("not a number:", arg)
		default:
			fmt.Println("error:", err)
		}
	}
}
```
:::

```text
ada
linus aka torvalds
missing user 3
not a number: x
```

## shapes — overloading and attributes

[`examples/shapes`](https://github.com/vuka-lang/vuka/tree/main/examples/shapes)

::: vuka
```vuka
@doc("area of a circle")
func area(c Circle) float64 { return math.Pi * c.R * c.R }

@export("RectArea")
func area(r Rect) float64 { return r.W * r.H }

func scale(x int) string     { return fmt.Sprint("int ", x*2) }
func scale(x float64) string { return fmt.Sprint("float ", x*2) }

fmt.Println(scale(2), "|", scale(2.5))   // int 4 | float 5
```
:::

<!-- go:context
type Circle struct{ R float64 }
type Rect struct{ W, H float64 }
-->

::: go
Each overload has a name of its own in Go — the one `@export` gives, or
one made from its parameters' types:

```go
// areaCircle is the area of a circle.
func areaCircle(c Circle) float64 { return math.Pi * c.R * c.R }

func RectArea(r Rect) float64 { return r.W * r.H }

func scaleInt(x int) string         { return fmt.Sprint("int ", x*2) }
func scaleFloat64(x float64) string { return fmt.Sprint("float ", x*2) }

func show() {
	fmt.Println(scaleInt(2), "|", scaleFloat64(2.5)) // int 4 | float 5
}
```
:::

## More

The transpiler's [test cases](https://github.com/vuka-lang/vuka/tree/main/transpile/testdata/golden)
are small programs with their expected output: decorators, statics, match,
dependency injection, and the errors Vuka reports.

## Libraries

Programs built with the [web framework](/web/) and the [ORM](/orm/), from
their repositories.

### pets — a web app {#pets}

[`examples/pets`](https://github.com/vuka-lang/web/tree/main/examples/pets) in
the [web](/web/) repository: pages with templ and `html/template` views, a
JSON API, a controller, a service, middleware and a call decorator.

::: vuka
```vuka
@web.Get("/pets")
@web.Template("views/pets.templ")
func ListPets(store *PetStore) Result[PetList] {
	pets := store.All()?
	return Ok(PetList{Title: "All pets", Pets: pets})
}

@web.Post("/pets")
func AddPet(in model.NewPet, store *PetStore) (web.Redirect, error) {
	p := store.Add(in)?
	return web.Redirect("/pets/" + itoa(p.ID)), nil
}

@web.Controller("/admin/pets")
type PetAdmin struct {
	store *PetStore
}

@web.Delete("/{id}")
@web.Use(requireToken)
@audited
func (a *PetAdmin) Remove(ctx context.Context, id int) error { return a.store.Remove(id) }
```
:::

<!-- go:context
type Pet struct{ ID int; Name string }
type NewPet struct{ Name string `json:"name" validate:"required"` }
type PetList struct{ Title string; Pets []Pet }
type PetStore struct{}

func (s *PetStore) All() ([]Pet, error)        { return nil, nil }
func (s *PetStore) Add(in NewPet) (Pet, error) { return Pet{}, nil }
func (s *PetStore) Remove(id int) error         { return nil }
func requireToken(next http.Handler) http.Handler { return next }
func audited(c *vuka.Call)                       { c.Next() }
-->

::: go
```go
func ListPets(store *PetStore) (PetList, error) {
	pets, err := store.All()
	if err != nil {
		return PetList{}, err
	}
	return PetList{Title: "All pets", Pets: pets}, nil
}

func AddPet(in NewPet, store *PetStore) (web.Redirect, error) {
	p, err := store.Add(in)
	if err != nil {
		return "", err
	}
	return web.Redirect("/pets/" + strconv.Itoa(p.ID)), nil
}

type PetAdmin struct {
	store *PetStore
}

func NewPetAdmin(store *PetStore) *PetAdmin { return &PetAdmin{store: store} }

// Remove runs through the audited decorator.
var petAdminRemove vuka.Decorated[func(*PetAdmin, context.Context, int) error]

func (a *PetAdmin) Remove(ctx context.Context, id int) error {
	return petAdminRemove.Get(func() func(*PetAdmin, context.Context, int) error {
		return vuka.Wrap((*PetAdmin).remove, vuka.FuncInfo{Name: "PetAdmin.Remove", Method: true}, audited)
	})(a, ctx, id)
}

func (a *PetAdmin) remove(ctx context.Context, id int) error { return a.store.Remove(id) }

func init() {
	web.Routes.Get("/pets", ListPets, web.Template("views/pets.templ"))
	web.Routes.Post("/pets", AddPet)
	web.Routes.Controller("/admin/pets", NewPetAdmin)
	web.Routes.Delete("/{id}", (*PetAdmin).Remove, web.Params("ctx", "id"), web.Use(requireToken))
}
```
:::

### live — live components {#live}

[`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live):
a counter page, a counter inside an ordinary page, a todo list, and a chat
whose tabs see each other's messages — [live components](/web/live) over a
WebSocket, with no JavaScript written.

::: vuka
```vuka
@web.Live("/chat/{room}")
type Chat struct {
	ui.Live
	Room  string
	Log   *ChatLog
	lines []string
	name  string
}

@ui.Mount
func (c *Chat) mount(ctx context.Context) error {
	c.Subscribe(c.topic())
	c.lines = c.Log.History(c.Room)
	return nil
}

func (c *Chat) Update(m Said)   { c.lines = append(c.lines, m.Who+": "+m.Text) }
func (c *Chat) Update(j Joined) { c.lines = append(c.lines, j.Who+" joined") }

func (c *Chat) Send(ctx context.Context, f ChatForm) error {
	c.Log.Add(c.Room, c.name+": "+f.Text)
	return web.Broadcast(ctx, c.topic(), Said{c.name, f.Text})
}
```
:::

<!-- go:context
type Said struct{ Who, Text string }
type Joined struct{ Who string }
type ChatForm struct{ Text string `form:"text"` }
type ChatLog struct{}

func NewChatLog() *ChatLog                       { return &ChatLog{} }
func (l *ChatLog) Add(room, line string)         {}
func (l *ChatLog) History(room string) []string  { return nil }
func (c *Chat) topic() string                    { return "chat:" + c.Room }
func (c *Chat) Render() ui.Node                  { return ui.HTML(`<ul></ul>`) }
-->

::: go
```go
type Chat struct {
	ui.Live
	Room  string
	Log   *ChatLog
	lines []string
	name  string
}

func NewChat(log *ChatLog) *Chat { return &Chat{Log: log} }

func (c *Chat) LiveSchema(l *ui.LiveSchema) { l.Mount(c.mount) }

func (c *Chat) mount(ctx context.Context) error {
	c.Subscribe(c.topic())
	c.lines = c.Log.History(c.Room)
	return nil
}

func (c *Chat) UpdateSaid(m Said)     { c.lines = append(c.lines, m.Who+": "+m.Text) }
func (c *Chat) UpdateJoined(j Joined) { c.lines = append(c.lines, j.Who+" joined") }

func (c *Chat) Send(ctx context.Context, f ChatForm) error {
	c.Log.Add(c.Room, c.name+": "+f.Text)
	return web.Broadcast(ctx, c.topic(), Said{c.name, f.Text})
}

func init() {
	web.Routes.Service(NewChatLog)
	web.Routes.Live("/chat/{room}", NewChat)
}
```
:::

### auth — signing in {#auth}

[`examples/auth`](https://github.com/vuka-lang/web/tree/main/examples/auth):
a login page, a live notice board that shows who is signed in, a Clear
button only admins may use, and a JSON API that takes bearer tokens —
[authentication](/web/auth) with one `Users` service written by the app.

::: vuka
```vuka
@web.Live("/board")
type Board struct {
	ui.Live
	Notices *Notices
	me      *auth.Identity
}

@ui.Mount
func (b *Board) mount(ctx context.Context) error { b.me = auth.Current(ctx); return nil }

@auth.Requires("notices.delete")
func (b *Board) Clear(ctx context.Context) error {
	b.Notices.Clear()
	return web.Broadcast(ctx, "notices", changed{})
}
```
:::

<!-- go:context
type changed struct{}
type Notices struct{}

func (n *Notices) Clear()        {}
func (b *Board) Render() ui.Node { return ui.HTML(`<main></main>`) }
-->

::: go
A page's gate is an option of its route; an event checks its own perm:

```go
type Board struct {
	ui.Live
	Notices *Notices
	me      *auth.Identity
}

func NewBoard(n *Notices) *Board { return &Board{Notices: n} }

func (b *Board) LiveSchema(l *ui.LiveSchema) { l.Mount(b.mount) }

func (b *Board) mount(ctx context.Context) error { b.me = auth.Current(ctx); return nil }

func (b *Board) Clear(ctx context.Context) error {
	if !auth.Can(ctx, "notices.delete") {
		return web.Forbidden
	}
	b.Notices.Clear()
	return web.Broadcast(ctx, "notices", changed{})
}

func init() {
	web.Routes.Live("/board", NewBoard, auth.Required())
}
```
:::

### blog — the ORM {#blog}

[`examples/blog`](https://github.com/vuka-lang/orm/tree/main/examples/blog) in
the [ORM](/orm/) repository: models with field attributes, migrations written
by `orm makemigrations`, queries with field references, `match` on `Get`,
`First`'s Option, `@orm.Transaction`, a typed SQLite function in Go, and
text, trigram and vector search. `cd examples/blog && vuka run .`

::: vuka
```vuka
posts := Post.Objects.Filter(Post.Title.Contains("Go"), Post.Author.Name.Eq("ada")).OrderBy(Post.Views.Desc()).All(ctx)?

match Post.Objects.Get(ctx, Post.Slug.Eq(slug)) {
case Ok(p):
	fmt.Printf("get %q: %s (%d views)\n", slug, p.Title, p.Views)
case Err(orm.NotFound):
	fmt.Printf("get %q: no such post\n", slug)
case Err(e):
	return e
}

doc := orm.Search(Post.Title, Post.Body)
found := Post.Objects.Filter(doc.Matches("go command")).OrderBy(doc.Rank("go command").Desc()).All(ctx)?
near := Post.Objects.Nearest(Post.Embedding, orm.Vector{1, 0, 0}, orm.Cosine).Limit(2).All(ctx)?
```
:::

<!-- go:context
var slug = "generics"

type Member struct {
	orm.Model[Member]
	ID   int64
	Name string
}

type Post struct {
	orm.Model[Post]
	ID        int64
	Title     string
	Slug      string
	Body      string
	Author    orm.FK[Member]
	Views     int
	Embedding orm.Vector
}

func (p *Post) Schema(s *orm.Schema) { s.Field(&p.Embedding, orm.Dims(3)) }
-->

::: go
<!-- go:fragment -->
```go
posts, err := orm.Objects[Post]().
	Filter(PostFields.Title.Contains("Go"), PostFields.Author.Name.Eq("ada")).
	OrderBy(PostFields.Views.Desc()).
	All(ctx)
if err != nil {
	return err
}

p, err := orm.Objects[Post]().Get(ctx, PostFields.Slug.Eq(slug))
switch {
case err == nil:
	fmt.Printf("get %q: %s (%d views)\n", slug, p.Title, p.Views)
case errors.Is(err, orm.ErrNotFound):
	fmt.Printf("get %q: no such post\n", slug)
default:
	return err
}

doc, q := orm.SearchVector(PostFields.Title.Name(), PostFields.Body.Name()), orm.SearchQuery("go command")
found, err := orm.Objects[Post]().Filter(orm.Match(doc, q)).Order(orm.SearchRank(doc, q).Desc()).All(ctx)
if err != nil {
	return err
}
near, err := orm.Objects[Post]().Nearest(PostFields.Embedding.Name(), orm.Vector{1, 0, 0}, orm.Cosine).Limit(2).All(ctx)
if err != nil {
	return err
}
```
:::

### multidb — web and the ORM, several databases {#multidb}

[`examples/multidb`](https://github.com/vuka-lang/orm/tree/main/examples/multidb):
products on main read from a replica, sales on analytics holding their
product by a CrossDB key, a raw report through an injected `*Analytics`, a
tenant's database per request, and `/healthz` — see
[Databases](/orm/databases#web-integration).

::: vuka
```vuka
type Sale struct {
	orm.Base @orm.Meta{DB: "analytics"}
	ID      int64
	Product orm.FK[Product] @orm.Rel{CrossDB: true, Related: "sales"}
	Amount  int
	At      time.Time @orm.AutoNowAdd
}

@orm.Transaction
@orm.Retry(3)
func sell(ctx context.Context, product int64, amount int) Result[Sale] {
	s := Sale{Product: orm.FKID[Product](product), Amount: amount}
	s.Save(ctx)?
	return Ok(s)
}

func newApp(dir string) *web.App {
	return web.New(
		ormweb.Databases(
			orm.Declare("main", "sqlite:"+dir+"/main.db?_pragma=journal_mode(WAL)", orm.AsDefault(),
				orm.Replicas("sqlite:"+dir+"/main.db?mode=ro"), orm.Pool{MaxOpen: 8, Acquire: 2 * time.Second}),
			orm.Declare("analytics", "sqlite:"+dir+"/analytics.db", orm.SlowQuery(200 * time.Millisecond)),
			orm.Declare("acme", "sqlite:"+dir+"/acme.db"),
			orm.OnOpen(setup),
		),
		web.Use(ormweb.UsingFrom(tenant)),
	)
}
```
:::

<!-- go:context
type Product struct {
	orm.Model[Product]
	ID   int64
	Name string
}

func setup(ctx context.Context, dbs *orm.Databases) error { return nil }
func tenant(r *http.Request) string                      { return r.Header.Get("X-Tenant") }
-->

::: go
```go
type Sale struct {
	orm.Model[Sale]
	ID      int64
	Product orm.FK[Product]
	Amount  int
	At      time.Time
}

func (x *Sale) Schema(s *orm.Schema) {
	s.Model(orm.Meta{DB: "analytics"})
	s.Field(&x.Product, orm.Rel{CrossDB: true, Related: "sales"})
	s.Field(&x.At, orm.AutoNowAdd{})
}

func sell(ctx context.Context, product int64, amount int) (Sale, error) {
	s := Sale{Product: orm.FKID[Product](product), Amount: amount}
	err := orm.Transaction(ctx, func(ctx context.Context) error {
		return s.Save(ctx)
	}, orm.Retry(3))
	return s, err
}

func newApp(dir string) *web.App {
	return web.New(
		ormweb.Databases(
			orm.Declare("main", "sqlite:"+dir+"/main.db?_pragma=journal_mode(WAL)", orm.AsDefault(),
				orm.Replicas("sqlite:"+dir+"/main.db?mode=ro"), orm.Pool{MaxOpen: 8, Acquire: 2 * time.Second}),
			orm.Declare("analytics", "sqlite:"+dir+"/analytics.db", orm.SlowQuery(200*time.Millisecond)),
			orm.Declare("acme", "sqlite:"+dir+"/acme.db"),
			orm.OnOpen(setup),
		),
		web.Use(ormweb.UsingFrom(tenant)),
	)
}
```
:::
