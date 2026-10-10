# Examples

The first ones are in Vuka's
[`examples`](https://github.com/vuka-lang/vuka/tree/main/examples) directory;
run one with `vuka run ./examples/<name>`. The [web](/web/) and [ORM](/orm/)
examples are in their own repositories; run one with `vuka run .` in its
directory.

## users — Result, ?, match

[`examples/users`](https://github.com/vuka-lang/vuka/tree/main/examples/users)

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

```text
ada
linus aka torvalds
missing user 3
not a number: x
```

## shapes — overloading and attributes

[`examples/shapes`](https://github.com/vuka-lang/vuka/tree/main/examples/shapes)

```vuka
@doc("area of a circle")
func area(c Circle) float64 { return math.Pi * c.R * c.R }

@export("RectArea")
func area(r Rect) float64 { return r.W * r.H }

func scale(x int) string     { return fmt.Sprint("int ", x*2) }
func scale(x float64) string { return fmt.Sprint("float ", x*2) }

fmt.Println(scale(2), "|", scale(2.5))   // int 4 | float 5
```

## nexus-di — dependency injection with nexus

[`examples/nexus-di`](https://github.com/vuka-lang/vuka/tree/main/examples/nexus-di),
a module of its own, wires `@di.Component` structs into the
[nexus](https://github.com/paulmanoni/nexus) container:

```vuka
@di.Component
type UserService struct {
	store *Store
	log   *slog.Logger // nexus provides the app's logger
}

@di.Component
type Greeter struct {
	users *UserService
}

func main() {
	_, stop, err := nexus.InProcess(config.Runtime{},
		nexus.Provide(NewStore),
		di.Module(),
		nexus.Invoke(func(g *Greeter) { fmt.Println(g.Hello(1)) }),
	)
	…
}
```

## pets — a web app {#pets}

[`examples/pets`](https://github.com/vuka-lang/web/tree/main/examples/pets) in
the [web](/web/) repository: pages with templ and `html/template` views, a
JSON API, a controller, a service, middleware and a call decorator.

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

## live — live components {#live}

[`examples/live`](https://github.com/vuka-lang/web/tree/main/examples/live):
a counter page, a counter inside an ordinary page, a todo list, and a chat
whose tabs see each other's messages — [live components](/web/live) over a
WebSocket, with no JavaScript written.

```vuka
@web.Live("/chat/{room}")
type Chat struct {
	vuka.Live
	Room  string
	Log   *ChatLog
	lines []string
	name  string
}

func (c *Chat) Mount() {
	c.Subscribe(c.topic())
	c.lines = c.Log.History(c.Room)
}

func (c *Chat) Update(m Said)   { c.lines = append(c.lines, m.Who+": "+m.Text) }
func (c *Chat) Update(j Joined) { c.lines = append(c.lines, j.Who+" joined") }

func (c *Chat) Send(ctx context.Context, f ChatForm) error {
	c.Log.Add(c.Room, c.name+": "+f.Text)
	return web.Broadcast(ctx, c.topic(), Said{c.name, f.Text})
}
```

## blog — the ORM {#blog}

[`examples/blog`](https://github.com/vuka-lang/orm/tree/main/examples/blog) in
the [ORM](/orm/) repository: models with field attributes, migrations written
by `orm makemigrations`, queries with field references, `match` on `Get`,
`First`'s Option, `@orm.Transaction`, a typed SQLite function in Go, and
text, trigram and vector search. `cd examples/blog && vuka run .`

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

## multidb — web and the ORM, several databases {#multidb}

[`examples/multidb`](https://github.com/vuka-lang/orm/tree/main/examples/multidb):
products on main read from a replica, sales on analytics holding their
product by a CrossDB key, a raw report through an injected `*Analytics`, a
tenant's database per request, and `/healthz` — see
[Databases](/orm/databases#web-integration).

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

## More

The transpiler's [test cases](https://github.com/vuka-lang/vuka/tree/main/transpile/testdata/golden)
are small programs with their expected output: decorators, statics, match,
dependency injection, and the errors Vuka reports.
