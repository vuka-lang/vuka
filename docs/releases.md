# Releases

Install a release with your own toolchain:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

Each release states the **runtime** its generated code needs — the
`github.com/vuka-lang/vuka` package your module requires. `vuka fix runtime`
upgrades it.

## v0.5.0 — 2026-10-10

- **Components and JSX.** Markup is an expression in `.vuka` files; components
  are functions, with attributes bound to parameters by name or to a props
  struct, `{for}`, `{if}` and `{match}` blocks, and errors at the tag.
  [Components and JSX](/features/components).
- **Rendered by templ.** `vuka.Node` is `templ.Component`: Vuka components and
  templ components call each other, and `.templ` files compile with the rest of
  the package (no `templ generate`), with editor support mapped into them.
- `vuka.Handler`, `vuka.Write`, `vuka.String`; a walkable node tree
  (`vuka.Walk`, `vuka.Renderer`) and a terminal renderer, `vuka/term`.
- VS Code extension 0.3.0: JSX highlighting.

**Breaking:** the decorator helper `vuka.Attr[T](c)` is now `vuka.AttrOf[T](c)`
(`vuka.Attr` is JSX's attribute). `vuka fix attr-of` rewrites it. Go 1.25 or
newer is required (templ needs it).

Runtime: v0.5.0.

## v0.4.1 — 2026-10-10

- **`vuka mod`**: `go mod` that sees the imports of `.vuka` files. `vuka mod tidy`
  keeps requirements plain `go mod tidy` would drop.
- Editor: a hover or go-to-definition gopls can't answer (a broken import, code
  mid-edit) is now no answer instead of an error, and VS Code no longer opens
  the output panel on errors.
- An import nothing provides that names a folder of your module gets a hint:
  *it's in this module: import "hello/data"*.

Runtime: v0.3.0.

## v0.4.0 — 2026-10-10

- **One gopls.** Run as `gopls`, vuka is a drop-in gopls, so `.go` files in a
  package with `.vuka` files see their code; every editor session shares one
  gopls daemon.
- **`vuka explain`** shows each line beside the Go it becomes.
- **`vuka fix`**: `runtime`, `static-names`, `orphans`.
- Faster builds: one `go list` per build, export data cached across builds.
- Workspaces reached through symlinks.
- This documentation site.

Runtime: v0.3.0.

## v0.3.1 — 2026-10-09

- Typing `@` lists the project's decorators, narrowing as you type.

## v0.3.0 — 2026-10-09

- **Dependency injection**: a type decorator on a struct gets `t.New`, a
  constructor taking its dependency fields (`inject:"-"` opts out).
- Picking a decorator after `@` inserts its name; a factory gets a call.

Runtime: v0.3.0.

## v0.2.1 — 2026-10-09

- Completion keeps working while an `@` line is being typed; code actions over
  attributes no longer fail; a clear message when the runtime is too old.

## v0.2.0 — 2026-10-09

- **Python-style decorators**: `decorator name(c) { … }`, `c.Next`, `c.Return`,
  `c.Err`, `c.Context`, `c.Attr`; type decorators.
- **Statics** and static methods, statics of generic types and through
  embedding; **Self** inference; **`self` methods**.

Runtime: v0.2.0.

## v0.1.1 — 2026-10-09

- The language server works beside VS Code's Go extension.

## v0.1.0 — 2026-10-09

- Result and Option, `?`, `match`, overloading, attributes, `@export`.
- `vuka build`, `run`, `test`, `gen`, `new`; the `build/` module.
- `vuka lsp` and the VS Code extension.
