# Getting started

Vuka needs [Go](https://go.dev/dl/) 1.25 or newer.

## Install

::: vuka
```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

Installing with your own toolchain matters: Vuka parses Go with the standard
library of the Go that built it, so it understands the same Go you write.
:::

::: go
Vuka's libraries are Go modules; a Go app needs nothing else:

```sh
go get github.com/vuka-lang/web github.com/vuka-lang/ui github.com/vuka-lang/orm
```

The `vuka` command is optional for a Go app — a watcher, scaffolding,
upgrades — and [`vukavet`](/tools/vukavet) adds the framework's checks to
`go vet`:

```sh
go get -tool github.com/vuka-lang/vuka/cmd/vuka github.com/vuka-lang/vuka/cmd/vukavet
```
:::

## A first project

A Vuka project is a Go module:

```sh
mkdir hello && cd hello
go mod init hello
```

::: vuka
Save this as `main.vuka`, then `vuka mod tidy` (it adds the Vuka runtime,
the small package holding `Result` and `Option`) and `vuka run .`:
:::

::: go
Save this as `main.go`, then `go mod tidy` and `go run .`, and open
`http://localhost:8080/?name=Ada`:
:::

::: vuka
```vuka
package main

import (
	"errors"
	"fmt"
	"os"
)

func greeting(name string) Result[string] {
	if name == "" {
		return Err(errors.New("who should I greet?"))
	}
	return Ok("Hello, " + name + "!")
}

func main() {
	name := "Vuka"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	match greeting(name) {
	case Ok(text):
		fmt.Println(text)
	case Err(e):
		fmt.Println("error:", e)
	}
}
```
:::

::: go
```go
package main

import (
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

var hello = ui.Template(`<html><body><h1>Hello, {{0}}!</h1></body></html>`)

// Hello answers GET /: name comes from ?name=.
func Hello(name string) ui.Node {
	if name == "" {
		name = "Vuka"
	}
	return hello.Render(name)
}

func main() {
	app := web.New()
	app.Get("/", Hello, web.Params("name"))
	log.Fatal(app.Run(""))
}
```
:::

For a web app — pages, sign-in, a database with migrations, tests —
`vuka new shop` makes one that runs; see [Getting started with web](/web/getting-started).

## Everyday commands

::: vuka
| | |
|---|---|
| `vuka run .` | run, like `go run` |
| `vuka build` | build, like `go build`, and refresh [`build/`](/guide/how-it-works#the-build-module) |
| `vuka test ./...` | test, like `go test` |
| `vuka mod tidy` | `go mod tidy`, seeing `.vuka` imports too |
| `vuka explain main.vuka` | show what each line becomes in Go |
| `vuka fix` | repair what tooling can, such as an outdated runtime |
:::

::: go
| | |
|---|---|
| `go run .` | run; with `web.Dev()` among `web.New`'s options, `VUKA_DEV=1 go run .` runs Tailwind and [Vite](/web/frontend#while-you-work) beside the app and turns the toolbar on |
| `go build`, `go test ./...` | build and test, as any Go module |
| `go vet -vettool=$(go tool -n vukavet) ./...` | `go vet` with the framework's [checks](/tools/vukavet): routes, live pages, templates, schemas |
| `go generate ./...` | the optional generated code: the ORM's typed fields and scanners |
| `go tool vuka run -watch .` | rebuild and restart on save, keeping the old process serving until the new one compiles |
:::

`.vuka` and `.go` files live side by side in the same packages and call each
other — see [Two ways to write Vuka](/guide/two-ways). Dependencies are Go's: `go get` a module and import it from any `.vuka`
file. See [Using Go code](/guide/using-go).

## Editor

Install the VS Code extension from [`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode)
and [gopls](https://go.dev/gopls). See [Editors](/tools/editors).
