---
title: Tabs
description: "Panels shown one at a time, the tab shown kept on the server."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Tabs

Panels shown one at a time, the tab shown kept on the server.

<div class="vk-ui-shots">
  <img src="/ui/components/tabs-light.png" alt="Tabs in the light theme">
  <img src="/ui/components/tabs-dark.png" alt="Tabs in the dark theme">
</div>

## Adding It

```sh
vuka ui add tabs
```

It writes `components/ui/tabs.vuka.html` and `components/ui/tabs.go`, with what it uses: [cn](/ui/vendored#cn). It runs on the server ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` v2.10.0 or newer.

## Using It

```vuka-html
<design.Tabs id="plan" default="monthly">
  <slot:tab value="monthly" label="Monthly"><p>Billed every month.</p></slot:tab>
  <slot:tab value="yearly" label="Yearly"><p>Billed once a year.</p></slot:tab>
  <slot:tab value="lifetime" label="Lifetime" disabled><p>Not offered.</p></slot:tab>
</design.Tabs>
```

A [stateful component](/ui/stateful): the tab shown is its own state on the
server, so a click switches it over the live connection and the rest of the
page keeps its state. Rendered on a page that isn't live, it shows its
first tab. The markup is `role="tablist"`, `role="tab"` buttons with
`aria-selected` and `aria-controls`, and the `role="tabpanel"` labelled by
its tab.

## Props

| Prop | Type | |
|---|---|---|
| `id` | `string` | ID prefixes the tabs' and the panels' ids; "tabs" by default. Give each Tabs on a page its own. |
| `default` | `string` | Default is the Value of the tab shown first; the first tab's by default. |
| `<slot:tab>` | `[]Tab` | Tabs are the tabs, a &lt;slot:tab&gt; each, in order. |
| `class` | `string` | Class is added to the root's classes, winning over them (see cn). |

### Tab

Tab is a tab of Tabs, a &lt;slot:tab&gt;: its Value names it, Label is what its button says, and the slot's content is its panel.

| Prop | Type | |
|---|---|---|
| `value` | `string` |  |
| `label` | `string` |  |
| `disabled` | `bool` |  |
| the content | `ui.Node` |  |

## Source

The component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.

<div class="vk-ui-source">

::: code-group

```vuka-html [tabs.vuka.html]
<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// Tabs shows one panel at a time, a tab list above them. The tab shown is
// state on the server: a click switches it over the page's live
// connection, and the page's other state stays as it is.
//
//	<design.Tabs id="settings" default="account">
//	  <slot:tab value="account" label="Account">…</slot:tab>
//	  <slot:tab value="password" label="Password">…</slot:tab>
//	</design.Tabs>
//
// Rendered without a live connection (a page that isn't live), it shows
// the Default tab.
type Tabs struct {
	ui.Live
	// ID prefixes the tabs' and the panels' ids; "tabs" by default. Give
	// each Tabs on a page its own.
	ID string
	// Default is the Value of the tab shown first; the first tab's by
	// default.
	Default string
	// Tabs are the tabs, a <slot:tab> each, in order.
	Tabs []Tab
	// Class is added to the root's classes, winning over them (see cn).
	Class string

	active string
}

// Select shows the tab whose Value is v.
func (t *Tabs) Select(v string) { t.active = v }

// current is the Value of the tab shown.
func (t *Tabs) current() string {
	switch {
	case t.active != "":
		return t.active
	case t.Default != "":
		return t.Default
	case len(t.Tabs) > 0:
		return t.Tabs[0].Value
	}
	return ""
}

func (t *Tabs) id(tab Tab, part string) string {
	id := t.ID
	if id == "" {
		id = "tabs"
	}
	return id + "-" + part + "-" + tab.Value
}

// selected is a tab's aria-selected.
func (t *Tabs) selected(tab Tab) string {
	if tab.Value == t.current() {
		return "true"
	}
	return "false"
}

const tabsList = "inline-flex h-9 w-fit items-center justify-center rounded-lg bg-muted p-[3px] text-muted-foreground"

const tabsTrigger = "inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-transparent px-2 py-1 " +
	"text-sm font-medium text-foreground transition-[color,box-shadow] disabled:pointer-events-none disabled:opacity-50 " +
	"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1 focus-visible:outline-ring " +
	"aria-selected:bg-background aria-selected:shadow-sm"
</script>

<div class={cn("flex flex-col gap-2", t.Class)}>
  <div role="tablist" class={tabsList}>
    for _, tab := range t.Tabs {
      <button
        key={tab.Value}
        type="button"
        role="tab"
        id={t.id(tab, "tab")}
        aria-controls={t.id(tab, "panel")}
        aria-selected={t.selected(tab)}
        disabled={tab.Disabled}
        class={tabsTrigger}
        onClick={Select(tab.Value)}
      >{tab.Label}</button>
    }
  </div>
  for _, tab := range t.Tabs {
    if tab.Value == t.current() {
      <div
        key={tab.Value}
        role="tabpanel"
        id={t.id(tab, "panel")}
        aria-labelledby={t.id(tab, "tab")}
        tabindex="0"
        class="flex-1 outline-none"
      >{tab.Panel}</div>
    }
  }
</div>
```

```go [tabs_html.go]
package design

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/uigen"
)

// Tabs shows one panel at a time, a tab list above them. The tab shown is
// state on the server: a click switches it over the page's live
// connection, and the page's other state stays as it is.
//
//	<design.Tabs id="settings" default="account">
//	  <slot:tab value="account" label="Account">…</slot:tab>
//	  <slot:tab value="password" label="Password">…</slot:tab>
//	</design.Tabs>
//
// Rendered without a live connection (a page that isn't live), it shows
// the Default tab.
type Tabs struct {
	ui.Live
	// ID prefixes the tabs' and the panels' ids; "tabs" by default. Give
	// each Tabs on a page its own.
	ID string
	// Default is the Value of the tab shown first; the first tab's by
	// default.
	Default string
	// Tabs are the tabs, a <slot:tab> each, in order.
	Tabs []Tab
	// Class is added to the root's classes, winning over them (see cn).
	Class string

	active uigen.Assign[string]
}

// Select shows the tab whose Value is v.
func (t *Tabs) Select(v string) { t.active.Set(v) }

// current is the Value of the tab shown.
func (t *Tabs) current() string {
	switch {
	case t.active.Get() != "":
		return t.active.Get()
	case t.Default != "":
		return t.Default
	case len(t.Tabs) > 0:
		return t.Tabs[0].Value
	}
	return ""
}

func (t *Tabs) id(tab Tab, part string) string {
	id := t.ID
	if id == "" {
		id = "tabs"
	}
	return id + "-" + part + "-" + tab.Value
}

// selected is a tab's aria-selected.
func (t *Tabs) selected(tab Tab) string {
	if tab.Value == t.current() {
		return "true"
	}
	return "false"
}

const tabsList = "inline-flex h-9 w-fit items-center justify-center rounded-lg bg-muted p-[3px] text-muted-foreground"

const tabsTrigger = "inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-transparent px-2 py-1 " +
	"text-sm font-medium text-foreground transition-[color,box-shadow] disabled:pointer-events-none disabled:opacity-50 " +
	"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1 focus-visible:outline-ring " +
	"aria-selected:bg-background aria-selected:shadow-sm"

func (t *Tabs) Render() ui.Node {
	var buttons []ui.Node
	for _, tab := range t.Tabs {
		buttons = append(buttons, ui.HTML(`<button key={tab.Value} type="button" role="tab" id={t.id(tab, "tab")} aria-controls={t.id(tab, "panel")} aria-selected={t.selected(tab)} disabled={tab.Disabled} class={tabsTrigger} onClick={func() { t.Select(tab.Value) }}>{tab.Label}</button>`,
			tab.Value,
			t.id(tab, "tab"),
			t.id(tab, "panel"),
			t.selected(tab),
			tab.Disabled,
			tabsTrigger,
			func() { t.Select(tab.Value) },
			tab.Label,
		))
	}
	var texts []ui.Node
	for _, tab := range t.Tabs {
		var div ui.Node
		if tab.Value == t.current() {
			div = ui.HTML(`<div key={tab.Value} role="tabpanel" id={t.id(tab, "panel")} aria-labelledby={t.id(tab, "tab")} tabindex="0" class="flex-1 outline-none">{tab.Panel}</div>`, tab.Value, t.id(tab, "panel"), t.id(tab, "tab"), tab.Panel)
		}
		texts = append(texts, ui.HTML(`{div}`, div))
	}
	return ui.HTML(`
<div class={cn("flex flex-col gap-2", t.Class)}>
  <div role="tablist" class={tabsList}>{buttons}</div>
  {texts}
</div>`, cn("flex flex-col gap-2", t.Class), tabsList, buttons, texts)
}
```

:::

</div>

Beside it, the same in both: `tabs.go`.

```go
package design

import "github.com/vuka-lang/ui/v2"

// Tab is a tab of Tabs, a <slot:tab>: its Value names it, Label is what its
// button says, and the slot's content is its panel.
type Tab struct {
	Value    string
	Label    string
	Disabled bool
	Panel    ui.Node
}
```
