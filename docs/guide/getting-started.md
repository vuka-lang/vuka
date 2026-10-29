# Installation

[[toc]]

## Introduction

This page gets the `vuka` command onto your machine and runs a first
program. Vuka needs [Go](https://go.dev/dl/) 1.25 or newer.

An app with a frontend or framework islands (Vue, React, Svelte) also needs
[Node.js](https://nodejs.org/) 20 or newer and npm 11 or newer (Node.js 24
and newer ship it; on Node.js 20 or 22, `npm install -g npm@11`).

> [!NOTE]
> npm 10 fails installing a new app's frontend packages from scratch. The
> `package-lock.json` that `vuka new` writes lets npm 10 install them with
> `npm ci`, and vuka warns; adding packages (`vuka add island --react`)
> needs npm 11, and vuka stops with [E0610](/errors/E0610) until it's there.
> `vuka doctor` shows the npm it finds.

An app with pages is styled with [Tailwind](/web/styling), whose CLI vuka
runs: `brew install tailwindcss` on macOS; elsewhere `npm i -D
@tailwindcss/cli` in the app, or the standalone binary for your system from
[Tailwind's releases](https://github.com/tailwindlabs/tailwindcss/releases)
(`tailwindcss-linux-x64`, `tailwindcss-windows-x64.exe`) on your PATH, under
that name or as `tailwindcss`.

> [!TIP]
> To try Vuka before installing anything, open the
> [playground](/playground): it shows the Go a `.vuka` or `.vuka.html`
> file becomes as you type, with the errors `vuka build` would report.

A Vuka project is an ordinary Go module, so once the command is installed
you work with the same toolchain, dependencies and editor setup you already
use for Go.

## Installing Vuka

Install the command with your own Go toolchain:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
```

Installing with your own toolchain matters: Vuka parses Go with the standard
library of the Go that built it, so it understands the same Go you write.
It also reads what the go command compiles of your imports, which only a Go
at least as new as the one building your app can read. When an app's go.mod
asks for a newer Go (its `go` or `toolchain` line), vuka says so and stops;
reinstall it with that Go, here go1.27.2:

```sh
GOTOOLCHAIN=go1.27.2 go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
```

In PowerShell:

```powershell
$env:GOTOOLCHAIN="go1.27.2"; go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest
```

Install `github.com/vuka-lang/vuka/cmd/vukavet/v2` the same way if you use it.
`vuka version` prints the Go that built vuka and the Go that builds the app.

### On Windows

Vuka runs in PowerShell or the Command Prompt, with Go and, for a frontend,
Node.js installed for Windows. A few things work differently there:

- `vuka run -watch` looks for changed files every 200ms; Windows' file
  events aren't used yet.
- The running app is asked to stop with a Ctrl+Break event, which a Go
  program receives as `os.Interrupt`: handle `os.Interrupt` to shut down
  gracefully. It gets two seconds before it is stopped.

## Creating a First Project

A Vuka project is a Go module. Create one:

```sh
mkdir hello && cd hello
go mod init hello
```

Save this as `main.vuka`, then run `vuka mod tidy` (it adds the Vuka runtime,
the small package holding `Result` and `Option`) and `vuka run .`:

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

The program uses [Result & Option](/features/result-option) and
[match](/features/match), two of the additions Vuka makes to Go.

### Starting a Web App

For a web app — pages, sign-in, a database with migrations, tests —
`vuka new shop` makes one that runs; see [Getting started with web](/web/getting-started).

## Everyday Commands

These are the commands you use most; each mirrors a go command:

| | |
|---|---|
| `vuka run .` | run, like `go run` |
| `vuka build` | build, like `go build`, and refresh [`build/`](/guide/how-it-works#the-build-module) |
| `vuka test ./...` | test, like `go test` |
| `vuka mod tidy` | `go mod tidy`, seeing `.vuka` imports too |
| `vuka explain main.vuka` | show what each line becomes in Go |

`.vuka` and `.go` files live side by side in the same packages and call each
other — see [Source Files](/guide/two-ways). Dependencies are Go's: `go get` a
module and import it from any `.vuka` file. See [Using Go Code](/guide/using-go).

## Setting Up Your Editor

Install the VS Code extension from [`editors/vscode`](https://github.com/vuka-lang/vuka/tree/main/editors/vscode)
and [gopls](https://go.dev/gopls). See [Editors](/tools/editors).

## Rendering UI

A web page is a `.vuka.html` shape under `pages/`: package, imports, one
struct, decorated hooks and methods in its `<script lang="go">`, and below it
the markup they render.

Pages use `[id]` names and `_layout.vuka.html`, and `main.go` constructs
`web.New(pages.Routes(), …)`. See [Web getting started](/web/getting-started).
