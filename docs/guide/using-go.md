# Using Go Code

[[toc]]

## Introduction

Vuka is Go with additions, so **every Go function, type and library works in
Vuka exactly as in Go** — no bindings, no wrappers, no special imports.

This page shows the common cases: your own `.go` files, the standard library,
third-party modules, and converting between `Result` and `(T, error)`. Every
example on it is a program that runs.

## Calling Functions From Your .go Files

### Calling Code in the Same Package

`.go` and `.vuka` files share packages. A function in a `.go` file is called
from a `.vuka` file like any other — and the other way round.

Here `util.go` declares a function in plain Go:

```go
// util.go — plain Go
package main

import "strings"

func slug(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "-")) }
```

And `main.vuka` calls it:

```vuka
// main.vuka
package main

import "fmt"

func main() {
	fmt.Println(slug("Hello World"))   // hello-world
}
```

### Importing Another Package of Your Project

A package written in plain Go is imported by its path, as in Go: the
**module path** from `go.mod` followed by the **directory**. In a module named
`hello`, the package in `hello/data/` is imported as `"hello/data"` — not
`"data"`, which Go would look for among your dependencies.

This package, `textutil/textutil.go`, is plain Go with a function, an error
value and a type with a method:

```go
// textutil/textutil.go — plain Go
package textutil

import (
	"errors"
	"strings"
)

var ErrEmpty = errors.New("empty text")

func Title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type Counter struct{ total int }

// Add counts the words of s and returns the total so far.
func (c *Counter) Add(s string) int {
	c.total += len(strings.Fields(s))
	return c.total
}

// First is the first word of s, or ErrEmpty.
func First(s string) (string, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return "", ErrEmpty
	}
	return f[0], nil
}
```

The file `words.vuka` imports it by its full path and uses each piece:

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

## Using the Standard Library

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

## Using Third-Party Libraries

Add a module with the go command:

```sh
go get github.com/google/uuid
```

Then import it from a `.vuka` file:

```vuka
import "github.com/google/uuid"

func newID() string { return uuid.NewString() }
```

`go.mod` and `go.sum` are the usual ones; Vuka has no package manager of its
own.

### The vuka mod tidy Command

Plain `go mod tidy` reads only `.go` files, so it drops
requirements that only `.vuka` files use. Use **`vuka mod tidy`** instead: it
shows the go command the imports of your `.vuka` files (and the Vuka runtime,
when your code uses it), then runs `go mod tidy`. Every `go mod` subcommand
works the same way — `vuka mod why`, `vuka mod vendor`, …

## Converting Between Result and (T, error)

`vuka.Of` turns a `(T, error)` into a [Result](/features/result-option), and
`Get` turns it back:

```vuka
r := vuka.Of(os.ReadFile(path))     // (T, error) → Result[T]
data, err := r.Get()                // Result[T] → (T, error)
```

<!-- go:context
var path = "app.conf"
-->

Errors keep their identity both ways: `errors.Is(err, fs.ErrNotExist)` is still
true after a round trip.

## Decorating Code That Calls a Library

A [decorator](/features/decorators) applies to a function you declare, so wrap the library call in one:

```vuka
@timed
func newID() string { return uuid.NewString() }
```

<!-- go:context
func timed(c *vuka.Call) { c.Next() }
-->

## Everything Else Is Go

Generics, goroutines, channels, `select`, `defer`, interfaces, embedding and
struct tags are Go's, unchanged. This sample sorts a slice and fans work out
to goroutines exactly as Go does:

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

## Calling Vuka From Go

Go code calls functions and types declared in `.vuka` files directly. A few
Vuka-only constructs have generated Go names — see
[Working With Go](/guide/go-interop#calling-vuka-from-go).
