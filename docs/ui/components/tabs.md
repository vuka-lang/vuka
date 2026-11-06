---
title: Tabs
description: "Panels shown one at a time, the tab shown kept in browser state."
pageClass: vk-ui-component
aside: false
---

<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->

# Tabs

Panels shown one at a time, the tab shown kept in browser state.

<div class="vk-ui-shots">
  <img src="/ui/components/tabs-light.png" alt="Tabs in the light theme">
  <img src="/ui/components/tabs-dark.png" alt="Tabs in the dark theme">
</div>

## Adding It

```sh
vuka ui add tabs
```

It writes `components/ui/tabs.vuka.html` and `components/ui/tabs.go`, with what it uses: [cn](/ui/vendored#cn). It runs in the browser ([the tiers](/ui/vendored#tiers)) and needs `github.com/vuka-lang/ui/v2` at commit `c2870b7828d7` or newer.

## Using It

```vuka-html
<script lang="go">
package settings

import "github.com/vuka-lang/ui/v2"

// Plan is the billing settings.
type Plan struct {
	plan ui.State[string]
}
</script>

<section>
  <design.Tabs id="plan" value={plan} default="monthly">
    <slot:tab value="monthly" label="Monthly"><p>Billed every month.</p></slot:tab>
    <slot:tab value="yearly" label="Yearly"><p>Billed once a year.</p></slot:tab>
    <slot:tab value="lifetime" label="Lifetime" disabled><p>Not offered.</p></slot:tab>
  </design.Tabs>
  <design.Button variant={design.ButtonLink} onClick={plan = "yearly"}>Save two months</design.Button>
</section>
```

The tab shown is [browser state](/ui/state): a `ui.State[string]` holding
the shown tab's value, passed as `value`. A click on a tab writes it, so the
panel switches at once with no round trip, and anything else writing it —
a button, a live handler's `p.plan = "yearly"` — switches it too. Without
`value` the tabs keep a state of their own. Until a tab is picked, the
`default` one shows, and the server renders it so.

Every panel is rendered, the others `hidden`. A live page that wants to hear
the switch declares the state with
[`@ui.Notify`](/ui/state#notify): `plan ui.State[string] @ui.Notify(OnPlan)`.
The markup is `role="tablist"`, `role="tab"` buttons with `aria-selected`
and `aria-controls`, and the `role="tabpanel"` labelled by its tab.

## Props

| Prop | Type | |
|---|---|---|
| `value` | `ui.State[string]` | Value is the browser state holding the Value of the tab shown: pass the page's to read or switch it from elsewhere, or leave it to the tabs' own. Empty shows Default. |
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
// browser state: a click switches it at once, with no round trip.
//
//	tab ui.State[string]
//
//	<design.Tabs id="settings" value={tab} default="account">
//	  <slot:tab value="account" label="Account">…</slot:tab>
//	  <slot:tab value="password" label="Password">…</slot:tab>
//	</design.Tabs>
//	<design.Button onClick={tab = "password"}>Change the password</design.Button>
//
// Until a tab is picked it shows the Default one; the server renders it so.
type Tabs struct {
	// Value is the browser state holding the Value of the tab shown: pass
	// the page's to read or switch it from elsewhere, or leave it to the
	// tabs' own. Empty shows Default.
	Value ui.State[string]
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
}

// first is the Value of the tab shown until one is picked.
func (t *Tabs) first() string {
	if t.Default != "" || len(t.Tabs) == 0 {
		return t.Default
	}
	return t.Tabs[0].Value
}

func (t *Tabs) id(tab Tab, part string) string {
	id := t.ID
	if id == "" {
		id = "tabs"
	}
	return id + "-" + part + "-" + tab.Value
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
        aria-selected={Value == tab.Value || Value == "" && tab.Value == t.first()}
        disabled={tab.Disabled}
        class={tabsTrigger}
        onClick={Value = tab.Value}
      >{tab.Label}</button>
    }
  </div>
  for _, tab := range t.Tabs {
    <div
      key={tab.Value}
      role="tabpanel"
      id={t.id(tab, "panel")}
      aria-labelledby={t.id(tab, "tab")}
      tabindex="0"
      show={Value == tab.Value || Value == "" && tab.Value == t.first()}
      class="flex-1 outline-none"
    >{tab.Panel}</div>
  }
</div>
```

```go [tabs_html.go]
package design

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/uigen"
)

// Tabs shows one panel at a time, a tab list above them. The tab shown is
// browser state: a click switches it at once, with no round trip.
//
//	tab ui.State[string]
//
//	<design.Tabs id="settings" value={tab} default="account">
//	  <slot:tab value="account" label="Account">…</slot:tab>
//	  <slot:tab value="password" label="Password">…</slot:tab>
//	</design.Tabs>
//	<design.Button onClick={tab = "password"}>Change the password</design.Button>
//
// Until a tab is picked it shows the Default one; the server renders it so.
type Tabs struct {
	// Value is the browser state holding the Value of the tab shown: pass
	// the page's to read or switch it from elsewhere, or leave it to the
	// tabs' own. Empty shows Default.
	Value ui.State[string]
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
}

// first is the Value of the tab shown until one is picked.
func (t *Tabs) first() string {
	if t.Default != "" || len(t.Tabs) == 0 {
		return t.Default
	}
	return t.Tabs[0].Value
}

func (t *Tabs) id(tab Tab, part string) string {
	id := t.ID
	if id == "" {
		id = "tabs"
	}
	return id + "-" + part + "-" + tab.Value
}

const tabsList = "inline-flex h-9 w-fit items-center justify-center rounded-lg bg-muted p-[3px] text-muted-foreground"

const tabsTrigger = "inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-transparent px-2 py-1 " +
	"text-sm font-medium text-foreground transition-[color,box-shadow] disabled:pointer-events-none disabled:opacity-50 " +
	"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1 focus-visible:outline-ring " +
	"aria-selected:bg-background aria-selected:shadow-sm"

func (t *Tabs) View() ui.Node {
	uigen.States(t)
	var buttons []ui.Node
	for _, tab := range t.Tabs {
		buttons = append(buttons, ui.HTML(`<button key={tab.Value} type="button" role="tab" id={t.id(tab, "tab")} aria-controls={t.id(tab, "panel")} aria-selected={uigen.Or(uigen.Eq(t.Value, tab.Value), uigen.And(uigen.Eq(t.Value, ""), tab.Value == t.first()))} disabled={tab.Disabled} class={tabsTrigger} onClick={ui.JS.Set(t.Value, tab.Value)}>{tab.Label}</button>`,
			tab.Value,
			t.id(tab, "tab"),
			t.id(tab, "panel"),
			uigen.Or(uigen.Eq(t.Value, tab.Value), uigen.And(uigen.Eq(t.Value, ""), tab.Value == t.first())),
			tab.Disabled,
			tabsTrigger,
			ui.JS.Set(t.Value, tab.Value),
			tab.Label,
		))
	}
	var divs []ui.Node
	for _, tab := range t.Tabs {
		divs = append(divs, ui.HTML(`<div key={tab.Value} role="tabpanel" id={t.id(tab, "panel")} aria-labelledby={t.id(tab, "tab")} tabindex="0" show={uigen.Or(uigen.Eq(t.Value, tab.Value), uigen.And(uigen.Eq(t.Value, ""), tab.Value == t.first()))} class="flex-1 outline-none">{tab.Panel}</div>`, tab.Value, t.id(tab, "panel"), t.id(tab, "tab"), uigen.Or(uigen.Eq(t.Value, tab.Value), uigen.And(uigen.Eq(t.Value, ""), tab.Value == t.first())), tab.Panel))
	}
	return ui.HTML(`
<div class={cn("flex flex-col gap-2", t.Class)}>
  <div role="tablist" class={tabsList}>{buttons}</div>
  {divs}
</div>`, cn("flex flex-col gap-2", t.Class), tabsList, buttons, divs)
}

func (t *Tabs) Render(ctx context.Context, w io.Writer) error { return t.View().Render(ctx, w) }
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
