# Using Go code

Vuka is Go with additions, so **every Go function, type and library works in
Vuka exactly as in Go** — no bindings, no wrappers, no special imports. This
page shows the common cases; every example on it is a program that runs.

## Functions from your .go files

### In the same package

`.go` and `.vuka` files share packages. A function in a `.go` file is called
from a `.vuka` file like any other — and the other way round.

```go
// util.go — plain Go
package main

import "strings"

func slug(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "-")) }
```

```vuka
// main.vuka
package main

import "fmt"

func main() {
	fmt.Println(slug("Hello World"))   // hello-world
}
```

### In another package of your project

A package written in plain Go is imported by its path, as in Go:

```go
// textutil/textutil.go — plain Go
package textutil

var ErrEmpty = errors.New("empty text")

func Title(s string) string { … }

type Counter struct{ total int }

func (c *Counter) Add(s string) int { … }

func First(s string) (string, error) { … }  // the first word, or ErrEmpty
```

```vuka
// words.vuka
package main

import (
	"fmt"

	"example.com/usego/textutil"
)

func firstTitle(s string) Result[string] {
	w := textutil.First(s)?             // a Go function, with ?
	return Ok(textutil.Title(w))
}

func words() {
	var c textutil.Counter              // a Go type and its methods
	c.Add("hello there")
	fmt.Println(c.Add("general kenobi"))   // 4

	match firstTitle("") {
	case Ok(w):
		fmt.Println(w)
	case Err(textutil.ErrEmpty):        // match a Go package's error value
		fmt.Println("nothing to title")
	case Err(e):
		fmt.Println(e)
	}
}
```

Nothing marks the Go package as special: Vuka passes the import to Go, and
Go's type checker reads it.

## The standard library

Import and call it as in Go. Functions returning `(T, error)` work with
[`?`](/features/try), and library error types work in [`match`](/features/match)
patterns:

```vuka
import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

func readConfig(path string) Result[string] {
	data := os.ReadFile(path)?
	return Ok(strings.TrimSpace(string(data)))
}

func main() {
	match readConfig("missing.conf") {
	case Ok(text):
		fmt.Println(text)
	case Err(&fs.PathError{Op: op}):    // a library's error type, taken apart
		fmt.Println("couldn't", op, "the config")   // couldn't open the config
	case Err(e):
		fmt.Println("error:", e)
	}
}
```

`?` works just as well in functions written Go's way:

```vuka
func port(s string) (int, error) {
	n := strconv.Atoi(s)?
	return n, nil
}
```

## Third-party libraries

Add a module with the go command, then import it:

```sh
go get github.com/google/uuid
```

```vuka
import "github.com/google/uuid"

func newID() string { return uuid.NewString() }
```

`go.mod` and `go.sum` are the usual ones; Vuka has no package manager of its
own.

::: warning go mod tidy
Plain `go mod tidy` reads only `.go` files, so it drops a requirement that only
`.vuka` files use. Either don't run it, or keep such imports in a `.go` file as
well — for example a `deps.go` with blank imports:

```go
package main

import _ "github.com/google/uuid"
```

`vuka fix runtime` puts the Vuka runtime back if it was dropped.
:::

## Converting between Result and (T, error)

```vuka
r := vuka.Of(os.ReadFile(path))     // (T, error) → Result[T]
data, err := r.Get()                // Result[T] → (T, error)
```

Errors keep their identity both ways: `errors.Is(err, fs.ErrNotExist)` is still
true after a round trip.

## Decorating code that calls a library

A decorator applies to a function you declare, so wrap the library call in one:

```vuka
@timed
func newID() string { return uuid.NewString() }
```

## Everything else is Go

Generics, goroutines, channels, `select`, `defer`, interfaces, embedding and
struct tags are Go's, unchanged:

```vuka
names := []string{"linus", "ada", "grace"}
slices.Sort(names)                  // [ada grace linus]

var wg sync.WaitGroup
results := make(chan int, 3)
for i := range 3 {
	wg.Add(1)
	go func() { defer wg.Done(); results <- i * i }()
}
wg.Wait()
```

## The other way: Go calling Vuka

Go code calls functions and types declared in `.vuka` files directly. A few
Vuka-only constructs have generated Go names — see
[Working with Go](/guide/go-interop#calling-vuka-from-go).
