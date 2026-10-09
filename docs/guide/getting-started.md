# Getting started

Vuka needs [Go](https://go.dev/dl/) 1.25 or newer.

## Install

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

Installing with your own toolchain matters: Vuka parses Go with the standard
library of the Go that built it, so it understands the same Go you write.

## A first project

```sh
vuka new hello
cd hello
vuka run .
```

`vuka new` makes an ordinary Go module — a `go.mod`, a `main.vuka`, and the
Vuka runtime (the small package holding `Result` and `Option`) as a dependency:

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

## Everyday commands

| | |
|---|---|
| `vuka run .` | run, like `go run` |
| `vuka build` | build, like `go build`, and refresh [`build/`](/guide/how-it-works#the-build-module) |
| `vuka test ./...` | test, like `go test` |
| `vuka mod tidy` | `go mod tidy`, seeing `.vuka` imports too |
| `vuka explain main.vuka` | show what each line becomes in Go |
| `vuka fix` | repair what tooling can, such as an outdated runtime |

`.vuka` and `.go` files live side by side in the same packages and call each
other. Dependencies are Go's: `go get` a module and import it from any `.vuka`
file. See [Using Go code](/guide/using-go).

## Editor

Install the VS Code extension from [`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode)
and [gopls](https://go.dev/gopls). See [Editors](/tools/editors).
