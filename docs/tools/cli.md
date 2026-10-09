# The vuka command

```text
vuka new <dir> [module path]
vuka build|run|test|vet|install [go flags] [packages]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka explain [-full] file.vuka
vuka fix [-n] [fixer…]
vuka lsp [-gopls path] [-log file] [-shared=false]
vuka version
```

## new

Starts a project: a Go module with a `main.vuka`, the runtime as a dependency,
and `/build/` in `.gitignore`.

## build, run, test, vet, install

The go command's, with every `.vuka` file in the module transpiled into an
overlay (`go … -overlay`): nothing is written among your sources. `vuka build`
also refreshes [`build/`](/guide/how-it-works#the-build-module). Flags and
packages are passed to go as they are.

## gen

Writes `build/`, a plain Go module (`-o` picks another directory). With
`-inplace`, writes each generated file beside its `.vuka` file instead — for
[libraries](/guide/go-interop#publishing-a-library). `-check` writes nothing and
fails when the output is out of date.

## explain

Shows what Vuka makes of a file: each rewritten line beside the Go it becomes,
then the code added after the source (decorator wrappers, statics), each
labelled with the line it comes from. `-full` prints the whole generated file.

## fix

Repairs what tooling can. `-n` only reports; name fixers to run only those.

| Fixer | |
|---|---|
| `runtime` | upgrade the module's Vuka runtime when generated code needs a newer one |
| `static-names` | write statics as `User.Table` in `.vuka` files, not by their Go names |
| `orphans` | remove files `vuka gen -inplace` wrote for `.vuka` files that are gone |

## lsp

The language server. See [Editors](/tools/editors).
