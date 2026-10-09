# Examples

Each is in the repository's
[`examples`](https://github.com/vuka-lang/vuka/tree/main/examples) directory.
Run one with `vuka run ./examples/<name>`.

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

## More

The transpiler's [test cases](https://github.com/vuka-lang/vuka/tree/main/transpile/testdata/golden)
are small programs with their expected output: decorators, statics, match,
dependency injection, and the errors Vuka reports.
