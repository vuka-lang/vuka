# Releases

Install a release with your own toolchain:

```sh
go install github.com/vuka-lang/vuka/cmd/vuka@latest
```

Each release states the **runtime** its generated code needs — the
`github.com/vuka-lang/vuka` package your module requires. `vuka fix runtime`
upgrades it.

## v0.8.0 — 2026-10-10

Runtime: unchanged, v0.7.0.

- **Seamless JSX editing.** Completion for every HTML element (and SVG
  basics) with descriptions, each element's own attributes before global
  ones, event handlers, `aria-*`/`data-*`, enumerated attribute values,
  closing tags, and `{for}`/`{if}`/`{match}` blocks with placeholders; hover
  on elements and attributes with MDN links; linked editing renames a tag's
  closing tag as you type; JSX elements and blocks fold; at most one
  diagnostic while a tag is half-typed.
- **Statics:** a generic type's static takes any initializer —
  `&Store[Self]{}`, `NewStore[Self]()`, `new(Store[Self])`, literals — with
  its type inferred, `Self` per instantiation.
- **VS Code extension 0.4.1:** closing tags after other tags on a line and
  inside attribute braces, `{` pairs inside elements, Enter indents between
  tags, `editor.linkedEditing` on for `.vuka` files.
- Docs: the language pages are library-free; the web framework and the ORM
  have their own sections.

## v0.7.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.7.0` (new: `vuka.Live`, `vuka.Component`,
`vuka.On`, `vuka.Event`, the `live` package; field references `vuka.Ref` with
`OrderedRef`, `StringRef`, `CompareRef`, `NullableRef`, `vuka.Pred`,
`vuka.Order`, `vuka.FieldPath`, `vuka.Related`, `vuka.FieldAttrs`,
`vuka.FieldsOf`; `vuka.Field` has `Attrs`).

- **Stateful components.** A struct embedding `vuka.Live`, with `Mount`,
  `Render` and event handlers: `<Counter Start={5}/>` keeps its state on the
  server, `onClick={c.Inc}` (methods or closures, values, form structs) runs
  there, and only the components that changed are sent back. Components nest,
  `key` keeps list items attached, `Update(msg)` overloads receive broadcasts.
  Rendered without a session they are plain HTML. The `live` package is the
  transport-independent session; [vuka-lang/web](https://github.com/vuka-lang/web)
  serves it over WebSocket. [Stateful components](/features/components#stateful-components).
- **Field attributes.** Typed attributes after a struct field, after its tag:
  `` Title string `json:"title"` @Char{Max: 200} ``, recorded at init.
  [Field attributes](/features/fields#field-attributes).
- **Field references.** `Post.Title` is a typed reference to the field;
  `Post.Author.Name` follows struct, pointer and `vuka.Related` fields. They
  build typed predicates (`Post.Views.Gt(100)`) and orderings, the
  foundation of [vuka-lang/orm](https://github.com/vuka-lang/orm)'s queries.
  [Field references](/features/fields#field-references).
- **JSX:** brace-less `if`/`for`/`match` inside block bodies; JSX in
  package-level variables; props from embedded struct fields; a block header
  may hold composite literals.
- **Statics** may use other statics, field references and `Self` in their
  initializers; initialization cycles are reported with their chain.
- **match:** exhaustiveness checks nested patterns (`Ok(Some(p))`, `Ok(None)`,
  `Err(e)`), and a `match` on a value from `x := f()?` sees its unwrapped type.
- Errors quote field references and statics as the source spells them.
- **Editor:** `vuka.File` paths are links (click to open, hover lists a
  `.templ` file's components); positions stay right while a file with
  several edits doesn't compile; completion offers stateful components, their
  props and event handlers.
- **VS Code extension 0.4.0:** highlighting for field attributes, match
  patterns, JSX blocks, events and component tags; 16 snippets (components,
  routes, live pages, ORM models, transactions); auto-closing tags; the vuka
  version in the status bar.

## v0.6.0 — 2026-10-10

Runtime: `github.com/vuka-lang/vuka v0.6.0` (new: `vuka.Decl`, `vuka.File`,
`vuka.TemplComponent`).

- **Declaration decorators.** A decorator of type `func(*vuka.Decl)` — or a
  call returning one, such as `@web.Get("/pets/{id}")` — runs once at init with
  the function's name, attributes, parameter names and types, and the function
  itself: the way to register routes, commands or jobs without wrapping calls.
  [Decorators](/features/decorators#declaration-decorators).
- **`vuka.File`.** A string literal where a decorator or attribute takes a
  `vuka.File` is checked when the program compiles (it must exist, in the
  package's directory or below) and embedded in the binary. For a `.templ` file
  `vuka.TemplComponents(f)` gives its components with their parameter names; a
  `.templ` file may sit in a subdirectory that is its own package
  (`views/pets.templ`, `package views`), which Vuka compiles and imports.
  [Files](/features/decorators#files).
- The export-data cache no longer keeps modules replaced by a directory
  (`replace … => ../vuka`), which could serve a stale runtime.
- [**vuka-lang/web**](https://github.com/vuka-lang/web), a web framework built on
  declarers: routes from `@web.Get`, handler parameters bound from the path,
  query and body, dependencies injected, results rendered as JSON or through
  `@web.Template` views (`.html`, `.templ` or JSX).

## v0.5.0 — 2026-10-10

- **Components and JSX.** Markup is an expression in `.vuka` files; components
  are functions, with attributes bound to parameters by name or to a props
  struct, `{for}`, `{if}` and `{match}` blocks, and errors at the tag.
  [Components and JSX](/features/components).
- **Rendered by templ.** `vuka.Node` is `templ.Component`: Vuka components and
  templ components call each other, and `.templ` files compile with the rest of
  the package (no `templ generate`), with editor support mapped into them.
- Components can be generic (type arguments inferred, or `<List[User]>`) or
  overloaded; `?` and `match` work inside `{…}`; a Vuka component that would
  drop its children is an error.
- `vuka.Handler`, `vuka.Write`, `vuka.String`; a walkable node tree
  (`vuka.Walk`, `vuka.Renderer`) and a terminal renderer, `vuka/term`, which
  lays out templ components' HTML too.
- **`vuka fmt`**: Go as gofmt prints it, JSX Prettier-style, never changing
  what a page renders. Format on save in the editor.
- Editor: completion after `<`, in a tag's attributes and after `</`; hover,
  go to definition and rename on tags, closing tags and attributes; a tag
  being typed no longer breaks the rest of the file.
- templ's language server can use vuka as its gopls, so `.templ` files see
  code from `.vuka` files.
- VS Code extension 0.3.0: JSX highlighting, templ's icon for `.templ` files,
  format on save, and an offer to serve `.templ` files through Vuka.

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
