# Source Files

[[toc]]

## Introduction

A Vuka package can hold three kinds of source file, and you use one file
type for each job. All three share package scope, so code in one kind of
file calls code in another directly.

## Choosing a File Type

The table below shows which file type does which job:

| File | Job |
|---|---|
| `.vuka.html` | Pages, layouts, components and email markup; the struct it renders, and any helper structs, in a `<script lang="go">`, then markup |
| `.go` | Ordinary Go logic, services, models, API handlers and startup |
| `.vuka` | Logic with no markup that uses Result, Option, match, enums, overloading or decorators |

## Shape Files

Rendering lives in shape files. A `<script lang="go">` holds the Go, and the
markup follows it.

In the script's struct, exported fields are props, unexported fields are
server state or dependencies, and `ui.State[T]` is browser state written by
assignment in event attributes. See [Templates](/ui/templates).

A script can declare helper structs beside it. A file declaring several renders
the one named after the file: `orders.vuka.html` renders `Orders`, and
`order-row.vuka.html` `OrderRow`. Case and separators don't count, and
`[id].vuka.html` renders `Id` or `ID`. If none has the name, it is
[E0147](/errors/E0147), naming the struct expected. A file declaring one struct
renders it, whatever it is called. Helpers and their methods are ordinary types of
the package, not components, routes or live.

The script is `.vuka` source. Everything a `.vuka` file allows works in it: `?`,
match, Result, Option, enums, overloading, decorators, attributes and statics.
So a component's Go can stay in one file, and errors, `vuka vet`, `vuka fmt` and
the editor all work at their place in the `.vuka.html` file. Markup is the one
exception: it follows `</script>` ([E0151](/errors/E0151)).

```html
<!-- orders.vuka.html -->
<script lang="go">
package pages

import "strconv"

type Row struct {
	Qty int
}

func parse(s string) Result[Row] {
	n := strconv.Atoi(s)?
	return Ok(Row{Qty: n})
}

type Orders struct {
	Input string
}

func (p Orders) Label() string {
	match parse(p.Input) {
	case Ok(r):
		return strconv.Itoa(r.Qty) + " items"
	case Err(e):
		return e.Error()
	}
}
</script>

<p>{Label()}</p>
```

A `.vuka` file is for a package's Go that isn't one component's, and a pair
`card.vuka` and `card.vuka.html` stays valid.

## Page Routes

Page routes come from `pages/`, bracket names and `_layout.vuka.html`.
`pages/route.go` is generated; main constructs `web.New(pages.Routes(), …)`.
See [Getting started](/web/getting-started).
