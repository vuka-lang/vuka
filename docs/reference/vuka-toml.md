# vuka.toml

[[toc]]

## Introduction

`vuka.toml` holds a project's settings for the `vuka` command: how
`vuka fmt` lays files out, what the compiler and `vuka vet` check, and
where `vuka ui` writes components. It sits next to `go.mod`. Every table
and key is optional; a project without the file gets the defaults below.

The app's own runtime settings are environment variables, not this file:
see [Configuration](/web/configuration).

A project that uses all four tables:

```toml
[fmt]
width = 100
tailwind = true

[compile]
implicit_ctx = false

[vet]
wrap_errors = true

[ui]
style = "default"
components = "components/ui"
package = "design"
css = "styles/app.css"
```

## The File {#file}

The file is tables of `key = value` lines and `#` comments. Booleans are
`true` or `false`, numbers are bare, and the `[ui]` table's values are
quoted strings.

It is strict: a table other than `[fmt]`, `[compile]`, `[vet]`, `[ui]` and
`[ui]`'s own (`[ui.sources]`, `[ui.libraries.<name>]`),
a key a table doesn't have, a key outside a table and a value of the wrong
kind are errors naming the file and line, often with the nearest key:

```text
/home/ana/shop/vuka.toml:2: unknown key widht in [fmt]; did you mean width?
```

`vuka fmt` and the compiler read the nearest `vuka.toml` between a file's
directory and the module's root, so a directory can carry its own; `vuka
ui` reads the one at the module's root. The editor reads the same files.

## [fmt] {#fmt}

How [`vuka fmt`](/tools/cli#fmt) and the editor's format-on-save lay out
`.vuka.html` files.

| Key | Default | |
|---|---|---|
| `width` | `100` | the column a tag's attributes wrap at; 40 or more |
| `tailwind` | `false` | sort each static `class="…"` list in Tailwind's order |

Turning `tailwind` on reorders every class list the next time the module is
formatted: one large diff, best made on its own. See
[Formatting Shape Files](/tools/cli#fmt-shapes).

## [compile] {#compile}

What the compiler accepts. `vuka build`, `run`, `test`, `vet`, `gen`,
`explain` and the editor read it.

| Key | Default | |
|---|---|---|
| `implicit_ctx` | `true` | a call in markup one `context.Context` short gets the render's `ctx` |

With `implicit_ctx = false`, every `ctx` a template passes is written in its
source: `{len(Salaries())}` is [E0162](/errors/E0162), "write ctx
explicitly", and the markup reads as the Go it becomes:

<!-- vuka:context
type Salary struct{ Amount int }
-->

::: code-group

```vuka-html [Vuka]
<script lang="go">
package pages

import "context"

type Payroll struct{}

func (p Payroll) Salaries(ctx context.Context) ([]Salary, error) { return nil, nil }
</script>

<p>{len(Salaries(ctx))} people</p>
```

```go [Generated Go]
func (p Payroll) View() ui.Node {
	return ui.Ctx(func(ctx context.Context) ui.Node {
		return ui.HTML(`<p>{len(ui.Must(p.Salaries(ctx)))} people</p>`, len(ui.Must(p.Salaries(ctx))))
	})
}

// …
```

:::

Turn it off in a module whose reviews search the source for what reaches the
request's context. See [The Render's Context](/ui/templates#implicit-ctx) and
[Reviewing Authority in Markup](/ui/templates#reviewing-authority).

## [vet] {#vet}

Opt-in checks of [`vuka vet`](/tools/cli#build-run-test-vet-install).

| Key | Default | |
|---|---|---|
| `wrap_errors` | `false` | warn on a bare `?` passing on another package's error ([W0111](/errors/W0111)) |

With `wrap_errors = true`, a bare `?` on a call into another package,
`json.Unmarshal(body, &order)?`, is W0111. Context says what the function
was doing when it failed:

<!-- vuka:context
type Order struct{ ID int }
-->

::: code-group

```vuka [Vuka]
func decodeOrder(id int, body []byte) (Order, error) {
	var order Order
	json.Unmarshal(body, &order)?("decoding order %d", id)
	return order, nil
}
```

```go [Generated Go]
func decodeOrder(id int, body []byte) (Order, error) {
	var order Order
	if err := json.Unmarshal(body, &order); err != nil {
		return Order{}, fmt.Errorf("decoding order %d: %w", id, err)
	}
	return order, nil
}
```

:::

A `?` on a call into the same package passes the error on as it is. See
[Adding Context](/features/try#adding-context).

## [ui] {#ui}

Where [`vuka ui`](/ui/vendored) writes the app's components. `vuka ui init`
writes the table; `vuka ui add` and `vuka ui diff` refuse to run without it
(`vuka ui add library/name` runs `vuka ui init` first).

| Key | Default | |
|---|---|---|
| `components` | (required) | the components' directory, relative to the module: `components/ui` |
| `package` | (required) | their Go package's name: `design`, so `<design.Button>` sits beside `<ui.Link>` |
| `css` | the app's stylesheet | the stylesheet holding the design tokens: `styles/app.css` |
| `style` | `"default"` | the registry's style the tokens and components come in |
| `registry` | vuka's | another registry: an `https://` URL or a directory relative to the module, holding an `index.json` |

`vuka ui init --dir`, `--package` and `--css` choose the first three and
record them here; `--registry` on any `vuka ui` command overrides `registry`
for that run. See [The Registry](/ui/vendored#registry).

The editor reads the table too: completing a component tag offers the
components' package by its `package` name and imports it under that name
([Writing Component Tags](/tools/editors#component-tags)).

## [ui.sources] {#ui-sources}

Where each directory of the components directory came from, written by
`vuka ui add library/name`: the directory's name, and the library and the
version of its module it was read at. `vuka ui diff` compares each with
that version.

```toml
[ui.sources]
button = "shadcn-templ@v2.0.0-beta.16"
dialog = "shadcn-templ@v2.0.0-beta.16"
```

## [ui.libraries.&lt;name&gt;] {#ui-libraries}

A templ component library `vuka ui add <name>/<component>` reads, or a
change to one vuka knows (`shadcn-templ`): where its parts are in its Go
module. See [Other Libraries](/ui/vendored#other-libraries).

| Key | Default | |
|---|---|---|
| `module` | (required, or `dir`) | the library's Go module: `github.com/acme/templ-ui` |
| `version` | (required with `module`) | the version `vuka ui add` reads: `v1.4.0` |
| `dir` | | a directory holding the module's source in place of `module` and `version`, relative to the app's module |
| `components` | `"components"` | the module's directory holding a package per component |
| `scripts` | | directories of `components` holding scripts the components share, bundled before theirs: `["shared"]` |
| `theme` | | a stylesheet whose `:root`, `.dark` and `@theme inline` tokens the app gets the ones it lacks of |
| `css` | | stylesheets the components' classes need, copied beside the app's and imported: `["assets/css/animations.css"]` |
| `style` | | a stylesheet styling the components' own classes; the app gets the rules for the classes its components use |
