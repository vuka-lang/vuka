# The vuka command

```text
vuka new <dir> [module path]
vuka build|run|test|vet|install [go flags] [packages]
vuka mod tidy|why|vendor|graph|… [args]
vuka gen [-check] [-o build]
vuka gen -inplace [-check] [dir | dir/...]
vuka explain [-full] file.vuka
vuka fix [-n] [fixer…]
vuka fmt [-l] [-w] [-d] [paths…]
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

## mod

`go mod`, seeing the imports of `.vuka` files too. Plain `go mod tidy` reads
only `.go` files and would drop requirements only Vuka code uses; for the
command's duration, each package gets a small file of blank imports standing
for its `.vuka` files' imports (and the runtime, when the code uses it), removed
when it ends. It reads imports without transpiling, so it works while a
dependency is still missing.

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
| `attr-of` | rename `vuka.Attr[T](c)` to `vuka.AttrOf[T](c)`, in `.vuka` and `.go` files (v0.5.0) |

## fmt

Formats `.vuka` files, as `gofmt` formats Go: it prints the formatted file,
or with `-w` writes it back, `-l` lists the files whose formatting differs, and
`-d` shows the diff. Paths are files or directories (all `.vuka` files below,
skipping hidden directories, `node_modules`, `vendor` and `build/` modules vuka
made); no path means the current directory. A file that doesn't parse is
reported and left alone.

- **Go** comes out exactly as gofmt would print it, alignment included. Vuka's
  own syntax is spaced the same way: `@attr(…)` arguments and static fields'
  types and values are gofmt-formatted, a static's `static name` lines up like a
  field name, `match` and its cases are indented like a `switch`, and an
  attribute always starts its own line, with the declaration below it.
- **JSX** is laid out the way Prettier lays out markup. An element that fits
  in 80 columns (a tab counts 4) stays on one line, unless its children were
  written across lines and hold an element; otherwise its children go one per
  line, a tab deeper than the line the element starts on. Attributes that
  don't fit go one per line, with `>` or `/>` on its own line under the `<`.
  `<br/>` becomes `<br />`; `<p></p>` stays as it is. `{for}`, `{if}` and
  `{match}` blocks follow the same rule, their bodies a tab deeper and `}}`
  under the `{`; a match always takes several lines, its cases under the `{`.
  Go expressions in `{…}` are gofmt-formatted (`{a+b}` becomes `{a + b}`);
  one holding a comment is left as written.
- **Text never changes what it renders.** Spaces that render (`<b>a</b> <i>b</i>`,
  `<p>  x  </p>`) are kept, so elements they join stay on one line; lines of
  text keep their breaks and are re-indented; entities are left as written.
  Each formatted file is checked against the original, and an internal error
  is reported rather than a page that would render differently.

```vuka
// before
return <section className="pets" id="main" data-count={len(pets)} title="All our pets">
<h1>Pets ({len(pets)})</h1>
	<table>{for _,p:=range pets {<PetRow pet={p}/>}}</table>
		{if admin { <button disabled={len(pets)==0} >Add</button> } else { <i>read only</i> }}
</section>

// after
return <section
	className="pets"
	id="main"
	data-count={len(pets)}
	title="All our pets"
>
	<h1>Pets ({len(pets)})</h1>
	<table>{for _, p := range pets { <PetRow pet={p} /> }}</table>
	{if admin {
		<button disabled={len(pets) == 0}>Add</button>
	} else {
		<i>read only</i>
	}}
</section>
```

## lsp

The language server. See [Editors](/tools/editors).
