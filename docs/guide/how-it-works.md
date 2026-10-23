# How It Works

[[toc]]

## Introduction

Vuka is a transpiler that owns as little as it can. It rewrites its own
additions into Go and hands everything else to Go's own tools.

Knowing this helps you read errors, inspect what your code becomes, and use
plain Go tools on a Vuka project.

## Go Does the Heavy Lifting

A `.vuka` file passes through three stages:

1. **Tokens, not a new parser.** `go/scanner` reads each `.vuka` file. Vuka looks
   only for its own additions — `@`, `?`, `match`, `decorator`, `static`,
   overloaded names — and rewrites them into Go **in place**, line for line.
2. **Go's parser and type checker.** Everything else reaches `go/parser` and
   `go/types` untouched. When an addition needs types (which overload fits, what
   a `?` returns, whether a `match` is exhaustive), Vuka asks `go/types`, in
   rounds, until everything is decided.
3. **The go command.** `vuka build`, `run` and `test` hand the generated files to
   `go build -overlay`, so nothing is written among your sources.

A file with no Vuka in it comes out byte for byte the same; Vuka's tests check
that against every package of Go's own source tree.

## Errors Point at Your Code

The generated Go carries `//line` directives, so compile errors, panics and
stack traces name the `.vuka` file, line and column.

Vuka's own errors — a non-exhaustive `match`, an ambiguous overload, a
misplaced `?` — are reported before Go sees the code.

## Seeing the Generated Go

Run `vuka explain` on a file to see what each line becomes:

```sh
vuka explain main.vuka
```

For a line using [the ? operator](/features/try), the output looks like this:

```text
main.vuka:28
  vuka │     id := strconv.Atoi(arg)?
  go   │     id, __e1 := strconv.Atoi(arg); if __e1 != nil { return vuka.Err[User](__e1) }
```

`vuka explain -full` prints the whole generated file.

<!-- go:context
type User struct{ ID int }

func find(id int) vuka.Result[User] { return vuka.Ok(User{ID: id}) }
-->

## The Build Module

`vuka build` (and `vuka gen`) keep a `build/` directory: a complete Go module,
with your module path, where every `.vuka` file is replaced by its generated Go
and everything else is copied. Inside it, plain Go tools need no Vuka:

```sh
vuka gen
cd build && go build ./... && go test ./... && go vet ./...
```

The sync is incremental, and compile errors there still point at your `.vuka`
lines. `vuka gen -check` fails in CI when it is out of date.

## Tracking Go

Because Go's syntax reaches Go's own parser, new Go features work in Vuka without
a Vuka release. If Go adds a native equivalent of a Vuka feature, Vuka can lower
to it.
