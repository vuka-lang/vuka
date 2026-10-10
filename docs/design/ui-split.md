# Design: JSX targets and the UI library split (v0.10.0)

Status: implemented on branch `ui-split`. Breaking change, released as v0.10.0
together with `github.com/vuka-lang/ui` v0.1.0.

## Why

A language defines syntax and generic checks; libraries give it meaning.
TypeScript treats JSX as syntax and lets `jsxImportSource` pick React, Preact
or Solid. Vuka hard-coded one meaning: JSX lowered to `vuka.El/Text/Child/…`
in the root runtime, `vuka.Node` was `templ.Component` (so every Vuka module
required templ, which forced Go 1.25), and stateful components, the live
session and wire protocol, the terminal renderer, the templ bridge and
`.templ` compilation all lived in the language repository, with the
compiler checking the live runtime's event payload rules.

After the split the language owns JSX as syntax plus generic, go/types-based
component resolution against whatever package the file renders with — its
**JSX target**. `github.com/vuka-lang/ui` is the default (and first) target.

## What the language keeps

- JSX syntax: elements, fragments, attributes, `{…}` children, `{for}`,
  `{if}`, `{match}` blocks (brace-less nested ones too), comments, `key`.
- Component resolution: a capitalized or qualified tag names a function,
  method value or type in scope; attributes bind to parameters or props-struct
  fields by name (first letter's case free), children to a `children`
  parameter or `Children` field; generic components infer type arguments,
  overloaded ones pick the best fit; `(Node, error)` results. All typed with
  go/types against the target's declarations.
- The formatter, the language server's markup features (HTML element and
  attribute data is editor knowledge of HTML, independent of the target), the
  grammar and highlighting.

## JSX targets

A package is a JSX target when it declares

```go
// VukaJSX marks the package as a JSX target implementing version 1 of the
// contract.
const VukaJSX = 1
```

The constant is the contract version; a compiler that doesn't know the
version says to update Vuka. No directives, no registration: the compiler
finds it with go/types in the imported package's scope.

### Contract v1

Required (the compiler emits calls of these; Go type-checks them):

| Name | Shape | Emitted for |
|---|---|---|
| `Node` | a type | every lowered expression; components must return a type assignable to it |
| `Attr` | struct `{Name string; Value any}` | `[]T.Attr{{Name: "id", Value: x}}` |
| `El` | `func(tag string, attrs []Attr, children ...Node) Node` | an element |
| `Text` | `func(v any) Node` (called with a string constant) | text between tags |
| `Child` | `func(v any) Node` | a `{expr}` child |
| `Fragment` | `func(children ...Node) Node` | `<>…</>`, a component's children |
| `Nodes` | `func(build func(add func(Node))) Node` | a `{for}`, `{if}` or `{match}` block |
| `Try` | `func(n Node, err error) Node` | a `(Node, error)` component |

Optional capabilities (absence disables the feature, with a clear error where
it is used):

| Name | Shape | Enables |
|---|---|---|
| `On` | `func(handler H) A` | `on<Letter>…={expr}` on an element is an event: `T.On(expr)`. Without `On` it is a plain attribute. |
| `Stateful` + `Component` | an interface; `func(site string, key any, props S) Node` | a tag naming a struct type is a stateful component: `T.Component("file.vuka#n", key, &Counter{…})`. `*Counter` must implement `Stateful`. |
| `WithChildren` | `func(n Node, children Node) Node` | children for a component that takes none but may read them from its context (a function of another package, a `.templ` file, a function value) and returns exactly `Node`. |

| `F` | `func(fingerprint uint64, shape string, children ...Node) Node` | render trees: every JSX tree, block body and component's children is wrapped in a frame carrying a compile-time fingerprint and shape (`transpile/jsxframe.go`). Without `F`: the plain lowering. |

### Choosing the target

Explicit and per file, like any Go import: a `.vuka` file with JSX renders
with the one JSX target it imports.

- `import "github.com/vuka-lang/ui"` (any name, or `.`) selects ui; the
  lowering uses the import's name: `ui.El(…)`.
- No target imported → `JSX needs a target: import github.com/vuka-lang/ui
  (or another package declaring VukaJSX)`, at the first JSX expression.
- Two targets imported → `JSX in this file could render with A or B: a file
  imports one JSX target`.

There is no module-level default: an import is already the explicit,
file-local switch Go programmers expect, the file almost always names the
target anyway (`func Card() ui.Node`), and `vuka fix ui` adds the import.

Resolution happens while scanning, before the file can be parsed: the
file's import paths are read from its tokens and each non-standard one is
imported (the build's cached importer) to look for `VukaJSX`.

### Generic checks

- A component's result must be assignable to the target's `Node`
  (go/types' reason added: `missing method Render`).
- A type tag needs a target with stateful components; the type must be a
  struct and `*T` must implement the target's `Stateful`. The message gives
  go/types' reason, and when the missing method is unexported it names the
  exported struct of the target that provides it ("embed ui.Live").
  Embedded fields whose type the target declares are not props.
- An event attribute takes a function, `nil`, or a value of a type with
  methods (a target may accept such values, e.g. `templ.ComponentScript`);
  a string, number or plain struct is an error.

What moved out of the compiler into ui's runtime: event handler payload
rules (checked when the handler is first rendered), the `Mount` signature
(checked when the instance mounts), embedding `*ui.Live` by pointer (checked
when the component renders).

## github.com/vuka-lang/ui

A Go module (requires templ, Go 1.25), local repository `../vuka-ui`:

- `ui`: `Node` (= `templ.Component`), `NodeFunc`, `Attr`, `Style`, `El`, `Text`,
  `Child`, `Fragment`, `Nodes`, `Try`, `Safe`, `ErrorBoundary`, `Element` and
  the other node types, `Void`, `String`, `Handler`, `Write`, `Renderer`,
  `Walk`, `HTMLRenderer`; stateful components: `Live`, `Stateful`,
  `Component`, `ComponentNode`, `On`, `EventHandler`, `Event`, `EventName`,
  `LiveHost`, `LiveInstance`, `WithLiveHost`, `AttachLive`, `CopyLive`,
  `MountLive`; `WithChildren`; `const VukaJSX = 1`.
- `ui/live`: sessions and wire protocol v1, unchanged.
- `ui/term`: the terminal renderer.
- `ui/templx`: `WithChildren` (kept), and the registry of `.templ` files'
  components: `Component`, `Register`, `Components(vuka.File)` (formerly
  `vuka.TemplComponent`, `vuka.RegisterTempl`, `vuka.TemplComponents`). This
  is why ui requires `github.com/vuka-lang/vuka`.

## What the vuka module keeps

Result and Option, decorators and declarers (`Call`, `Func`, `Type`, `Decl`,
`AttrOf`, `TypeOf`, …), `File` (embedding is language: `File`, `FileOf`,
`Pkg`, `Path`, `Bytes`), statics, field references, predicates and fields.
No requirements at all: `go mod why github.com/a-h/templ` → not needed. The
`go` line is 1.23: `types.Func.Signature` (and `os.CopyFS` in tests) need it,
and it makes go/types materialize aliases (`ui.Node` stays `ui.Node` in
messages). The runtime version generated code requires is v0.10.0.

## .templ files: a toolchain plugin

templ is a tool concern. The CLI is its own module,
`github.com/vuka-lang/vuka/cmd/vuka`, which links templ's parser and
generator; the language module doesn't. `internal/load` keeps the `.templ`
file conventions (`x.templ` → `x_templ.go`, packages of `.templ` files, the
source map passed through as `any`) and exposes one hook,
`load.TemplCompiler`, which the CLI's `templplugin.go` sets. It is active
for a module whose go.mod requires `github.com/a-h/templ`
(`load.TemplActive`); elsewhere `.templ` files are ignored. A module with
`.templ` files that doesn't require templ yet runs `go get
github.com/a-h/templ` first.

A `vuka.File` naming a `.templ` file is still embedded by the compiler; when
the module requires `github.com/vuka-lang/ui` the generated code also
registers the file's components with `templx.Register` (the compiler's
`Options.TemplRegistry`, set by the plugin), read back with
`templx.Components(f)`.

Release choreography: tag `vX` (root), then set `cmd/vuka/go.mod` to require
`github.com/vuka-lang/vuka vX` without the local replace and tag
`cmd/vuka/vX`. `go install github.com/vuka-lang/vuka/cmd/vuka@latest` keeps
working (it resolves the nested module).

## Compatibility: `vuka fix ui`

Rewrites a module's `.vuka` and `.go` files:

- `vuka.<UI name>` → `ui.<name>` (`Node`, `NodeFunc`, `Live`, `Component`,
  `On`, `Event`, `El`, `Text`, …, the full list in `cmd/vuka/fixui.go`);
  `vuka.TemplComponent`/`RegisterTempl`/`TemplComponents` →
  `templx.Component`/`Register`/`Components`;
- imports `github.com/vuka-lang/vuka/{live,term,templx}` →
  `github.com/vuka-lang/ui/…`;
- adds `import "github.com/vuka-lang/ui"` to every `.vuka` file with JSX or
  a rewritten name, and drops a `vuka` import left unused;
- `go get github.com/vuka-lang/ui` when anything changed.

## Move map (old → new)

For porting work done against the old layout (e.g. the live-diff branch):

| Old (vuka repo) | New |
|---|---|
| `node.go` | ui `node.go` (package `ui`) |
| `html.go` | ui `html.go` |
| `walk.go` | ui `walk.go` |
| `http.go` | ui `http.go` |
| `live.go` (Live, Component, On, Event, LiveHost…) | ui `live.go` |
| `node_test.go`, `walk_test.go`, `live_test.go` | ui, same names |
| `live/*` | ui `live/*` (import `github.com/vuka-lang/ui/live`) |
| `term/*` | ui `term/*` |
| `templx/*` | ui `templx/*` |
| `file.go`: `TemplComponent`, `RegisterTempl`, `TemplComponents` | ui `templx/registry.go`: `Component`, `Register`, `Components` |
| `transpile/jsx.go` writer: `w.rt + ".El("` etc. | same file; qualifier is the target's, `w.q + "El("` (`w.q` ends in `.`, or is empty for a dot import) |
| `transpile/jsxcomp.go` `nodeType`, `templx.WithChildren` | same file; `e.targetObj(f, "Node")`, the target's `WithChildren` |
| `transpile/jsxlive.go` `statefulProblem`, `handlerProblem`, `payloadOK` | generic checks in `transpile/jsxlive.go`; payload rules → ui `live.go` `checkHandler`; Mount → ui `MountLive` |
| — | `transpile/target.go`: target resolution and the contract |
| `internal/load/templ.go` compile step | `cmd/vuka/templplugin.go` (hook `load.TemplCompiler`) |
| `internal/load/templ_test.go`, `files_test.go` | `cmd/vuka/templplugin_test.go` |
| `frame.go`, `tree.go`, `assign.go`, `tree_test.go` (live-diff) | ui, same names |
| `live/internal/demo` drift test (`go run ./cmd/vuka`) | ui; runs `vuka` from PATH or `$VUKA` |
| `transpile/jsxframe.go` | same file; frames only when the target declares `F` |
| `transpile/testdata/golden/*.vuka` (JSX) | same, importing ui; the golden directory is a module (`go.mod`) requiring ui |


## Testing a target-dependent compiler

The golden directory `transpile/testdata/golden` is a module requiring ui
(today through `replace => ../../../../vuka-ui`, a sibling checkout; after
ui is tagged, a version). The CLI's tests build temporary modules requiring
the sibling ui (`$VUKA_UI` overrides the path; `cmd/vuka/testmod_test.go`).
