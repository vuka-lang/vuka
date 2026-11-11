# Data Tables

[[toc]]

## Introduction

`ui.DataTable` is the kit's list page: a table with a search box, sortable
headers, a pager and a menu of actions per row. You declare its columns
once, as [repeated slots](/ui/templates#repeated-slots), and choose where
its rows come from:

| Rows | Prop | Who searches, sorts and pages |
|---|---|---|
| a store of any size | `load={…}` | the database, through your `Load` (see [`ui.LoadPage`](#loadpage)) |
| a slice you already have | `rows={…}` | the server, from the slice |
| a short slice | `rows={…} client` | the browser, with no request |

In the first two modes the table keeps its page, size, search and sort in
the page's address, so a sorted, searched page is a link you can share, and
back and forward step through it. Each control is a link or writes the
address over the live connection: a [patch](/web/live) of the page, not a
page load.

The table looks like the kit's other components: it reads only the
[`--ui-*` tokens](/ui/classes#kit), so a theme restyles it with no CSS of its
own.

## From a Store {#load}

Give the table a `Load` method. It takes the request's `ui.TableQuery` and
returns one `ui.Page[T]`:

```html
<script lang="go">
package books

import (
	"context"

	"example.com/app/models"
	"github.com/vuka-lang/orm/v2"
	"github.com/vuka-lang/ui/v2"
)

type Index struct{}

func (p *Index) Books(ctx context.Context, q ui.TableQuery) (ui.Page[models.Book], error) {
	return ui.LoadPage(ctx, q, ui.Source[models.Book]{
		Search: func(qs orm.QuerySet[models.Book], term string) orm.QuerySet[models.Book] {
			return qs.Filter(orm.Q{"title__icontains": term})
		},
		Default: "-published",
	})
}
</script>

<ui.DataTable load={Books} searchPlaceholder="Find a book">
  <slot:column header="Title" sort="title" let={b}><b>{b.Title}</b></slot:column>
  <slot:column header="Pages" sort="pages" align="right" let={b}>{b.Pages}</slot:column>
  <slot:column header="Author" let={b}>{b.Author}</slot:column>
</ui.DataTable>
```

`ui.TableQuery` is the page the address asks for, checked against the
table:

| Field | |
|---|---|
| `Page` | from 1, as asked; a page past the last shows the last |
| `Size` | one of the table's sizes (`sizes={…}`, else 10, 25, 50 and 100) |
| `Search` | the search box's text, trimmed |
| `Sort`, `Desc` | a sort key one of the columns declares, or empty |
| `Sortable` | the sort keys the columns declare |

`Offset()` is how many rows come before the page. A `Load` returning an
error doesn't fail the page: the table shows that the rows couldn't be
loaded, and the error is logged with the request's context.

### `ui.LoadPage` {#loadpage}

`ui.LoadPage(ctx, q, src)` is a `Load` over an [orm](/orm/) `QuerySet`. It
counts the rows before the search (`Page.All`) and after it
(`Page.Total`), orders them and lets the database cut the page.
`ui.Source[T]` says how:

| Field | |
|---|---|
| `Rows` | the set to page; every row of `T` when nil |
| `Search` | narrows the set to a non-empty search term; without it the search is ignored |
| `Sorts` | maps a column's sort key to the field it orders by (`"author": "author__name"`); a key it doesn't list is ignored |
| `Default` | the order when no sort applies: a field, a leading `-` for descending |

Without `Sorts`, a sort key the table's columns declare is the field's name
itself, so `sort="title"` on the column is all you write. A key the address
names that no column declares never reaches the query.

## From a Slice {#rows}

With `rows`, the table pages the slice itself, from the address as it does
for `load`. `searchBy` is the text a row is found by (without it there is
no search box), and `sorts` gives the value a sort key orders by — numbers
and times by value, anything else as text, digit runs as numbers:

```html
<ui.DataTable rows={Members} searchBy={memberText} sorts={memberSorts}>
  <slot:column header="Name" sort="name" let={m}>{m.Name}</slot:column>
  <slot:column header="Joined" sort="joined" let={m}>{m.Joined.Format("2 Jan 2006")}</slot:column>
</ui.DataTable>
```

with `memberSorts` a `map[string]func(Member) any` such as
`{"name": func(m Member) any { return m.Name }}`. A column whose key isn't
in `sorts` keeps a plain header.

## In the Browser {#client}

`client` renders every row and lets the browser search and page them,
with no request: for a short list that is already in the page. The
runtime finds a row by `searchBy`'s text, else by what the row shows. It
keeps the page it is on when a live render reaches the table. Sorting
stays on the server: a client table's headers are plain.

```html
<ui.DataTable rows={Countries} client searchBy={countryText}>
  <slot:column header="Country" let={c}>{c.Name}</slot:column>
  <slot:column header="Code" let={c}>{c.Code}</slot:column>
</ui.DataTable>
```

## From a Live View's Events {#events}

A live view that keeps the page in its own state passes it as `page` and
handles the controls itself: `onSearch`, `onSort`, `onPage` and `onSize`
are event handlers, and a nil one drops its control. This is the table's
original form; it works as before.

```html
<ui.DataTable columns={cols} page={page} search={query} sort={sort} sortDir={dir}
  onSearch={Search} onSort={SortBy} onPage={GoTo}/>
```

## Two Tables on a Page {#key}

`key` prefixes a table's parameters in the address (`orders.page`,
`orders.q`), and `id` names its root; a second table needs both:

```html
<ui.DataTable id="open" key="open" load={OpenOrders}>…</ui.DataTable>
<ui.DataTable id="closed" key="closed" load={ClosedOrders}>…</ui.DataTable>
```

## Reloading the Rows {#reload}

The table's root carries its `id`, `records` unless set. After an event
that changed the rows outside the table — a dialog saving one — reload
just the table, and `Load` runs again:

```html
<ui.Button on={ui.JS.Push(Save).Success(ui.JS.Reload("#records"))}>Save</ui.Button>
```

## Row Actions {#actions}

`<slot:actions let={row}>` is a row's menu, behind a button at the end of
the row. Each item is a `ui.ActionMenu`: an `icon`, a label, and `on` (an
event handler or a [JS chain](/ui/js)) or `href`. `danger` marks one that
destroys. Choosing an item closes the menu.

```html
<ui.DataTable load={Books}>
  <slot:column header="Title" sort="title" let={b}>{b.Title}</slot:column>
  <slot:actions let={b}>
    <ui.ActionMenu icon="file-text" href={"/books/" + b.Slug}>Open</ui.ActionMenu>
    <ui.ActionMenu icon="trash" danger on={ui.JS.Push(Delete, ui.Value(b.ID))}>Delete</ui.ActionMenu>
  </slot:actions>
</ui.DataTable>
```

The menu is a popover: it opens over the page, never clipped by the
table, and closes on a click outside it or on Escape. Where the browser
positions popovers by an anchor it opens under the button, else in the
middle of the window.

## States {#states}

- **Empty**: `empty` (and `emptyIcon`) show when there are no rows at all;
  a search that finds none says so, with a link clearing the search.
- **Loading**: the table marks itself busy while one of its controls waits
  for the server — a bar above the rows — and `loading` sets it by hand.
- **Error**: a `Load` returning an error shows a message in place of the
  rows, not the error's text.

`toolbar`, `tabs`, `index`, `selectable`, `rowHref`, `rowKey` and `flush`
work in every mode, as on `ui.Table`.

## Styling {#styling}

The table, its search box, sort controls, pager and menu use the `vk-*`
classes and read only `--ui-*` tokens, so overriding the tokens themes
them. Its look follows shadcn's Table, Input, Pagination and Dropdown Menu:
a 1px border, `--ui-radius-sm` corners, muted header text, a subtle row
hover and a focus ring.
