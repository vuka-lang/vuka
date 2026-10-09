# Credits

## Go

Vuka is built on [Go](https://go.dev) and would not exist without it. It parses
Go with `go/scanner` and `go/parser`, checks it with `go/types`, and builds with
the `go` command — the work of the Go Authors and the Go community. Its editor
support is [gopls](https://go.dev/gopls), Go's language server, behind a proxy.

Vuka is an independent project. It is not affiliated with or endorsed by Google
or the Go project. "Go" is a trademark of Google LLC.

## Ideas borrowed

- [Dingo](https://github.com/MadAppGang/dingo) — a Go meta-language with Result,
  Option, `?` and pattern matching, and the inspiration for Vuka's start.
- [Elixir](https://elixir-lang.org) — pattern matching, guards and the pin
  operator.
- [Rust](https://www.rust-lang.org) — Result, Option and `?`.
- [Python](https://www.python.org) — decorators.
- [Kotlin](https://kotlinlang.org) and Java — statics, and a language that shares
  a project and its tools with another.
- [Django](https://www.djangoproject.com) — `Model.objects`.
- [Zig](https://ziglang.org) — compile-time evaluation, for what comes next.
- [React](https://react.dev) — JSX and components as functions.

## Built with

- [templ](https://templ.guide) by Adrian Hesketh and contributors (MIT) — Vuka's
  components render through templ's runtime, and `.templ` files are compiled
  with templ's own parser and generator. templ's language server, which proxies
  gopls, was also the model for Vuka's.

## License

Vuka is released under the [MIT License](https://github.com/vuka-lang/vuka/blob/main/LICENSE).
