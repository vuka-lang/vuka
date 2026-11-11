# Theming

[[toc]]

## Introduction

The kit's look is defined by one set of CSS variables, the **token contract**.
Every kit component reads these variables and nothing else: no component
stylesheet holds a colour, radius, shadow or size of its own. An app changes
how the kit looks by overriding tokens in one block of its own CSS, and never
by editing or overriding component rules. A test in the kit fails on a literal
colour outside the token section, so the contract stays the only place values
live.

The tokens are defined in the kit's `ui.css`, which `ui.Styles` serves. Light
values are the default; the dark values apply under
`prefers-color-scheme: dark`, and under `[data-theme="dark"]` or `.dark` when
the app picks the scheme itself (`[data-theme="light"]` or `.light` forces
light).

## Overriding tokens

Load the app's block after the kit's styles and set only what differs:

```css
:root {
  --ui-primary: #7c3aed;
  --ui-radius: .75rem;
}
:root[data-theme="dark"] {
  --ui-primary: #a78bfa;
}
```

Names that exist as tokens are stable: a name the kit renames keeps an alias
for the old one.

## The shadcn preset

An app that already defines shadcn's variables (`--background`, `--primary`,
`--radius` and the rest) can have the kit follow them instead of repeating
values. `preset-shadcn.css` is opt-in: load it after `ui.css`. It maps each
`--ui-*` colour, radius and image token onto the shadcn name that plays the
same role, using complete CSS colour values (the modern shadcn form). Tokens
with no shadcn counterpart keep the kit's defaults, and an `--ui-*` override in
the app's own block still wins.

## Opening a component without chrome

A component opened in place with `ui.Into("#side")` is wrapped in an inline
card: a header, a close button and body padding. A host that only provides a
place for toasts or modals does not want that card, and `ui.Bare()` removes it:

```html
<button onClick={ui.JS.Call(components.Saved{}, ui.Into("#toasts", ui.Bare()))}>Save</button>
```

A component whose root is a Toast, Dialog, Drawer or Notice brings its own
presentation and is bare without the option. Plain components keep the inline
card. Dialogs and toasts shown from the overlay host take pointer events in
the kit's own CSS, so a host needs no rule of its own.

## Token reference

Each token with its light and dark value.

### Colour

| Token | Light | Dark |
| --- | --- | --- |
| `--ui-bg` | `#ffffff` | `#0f172a` |
| `--ui-fg` | `#0f172a` | `#e2e8f0` |
| `--ui-muted` | `#64748b` | `#94a3b8` |
| `--ui-subtle` | `#f1f5f9` | `#1e293b` |
| `--ui-border` | `#e2e8f0` | `#334155` |
| `--ui-ring` | `#94a3b8` | `#64748b` |
| `--ui-overlay` | `rgba(15,23,42,.45)` | `rgba(0,0,0,.6)` |
| `--ui-primary` | `#2563eb` | `#3b82f6` |
| `--ui-primary-fg` | `#ffffff` | `#ffffff` |
| `--ui-info` | `#0ea5e9` | `#0ea5e9` |
| `--ui-success` | `#16a34a` | `#16a34a` |
| `--ui-warning` | `#d97706` | `#d97706` |
| `--ui-danger` | `#dc2626` | `#dc2626` |
| `--ui-info-bg` | `#f0f9ff` | `#082f49` |
| `--ui-success-bg` | `#f0fdf4` | `#052e16` |
| `--ui-warning-bg` | `#fffbeb` | `#451a03` |
| `--ui-danger-bg` | `#fef2f2` | `#450a0a` |
| `--ui-danger-fg` | `#fff` | `#fff` |
| `--ui-success-fg` | `#fff` | `#fff` |
| `--ui-warning-fg` | `#fff` | `#fff` |
| `--ui-info-fg` | `#fff` | `#fff` |
| `--ui-image-bg` | `#e2e8f0` | `#e2e8f0` |
| `--ui-image-shimmer` | `rgb(255 255 255/.6)` | `rgb(255 255 255/.6)` |
| `--ui-image-scrim-start` | `rgb(0 0 0/.6)` | `rgb(0 0 0/.6)` |
| `--ui-image-scrim-end` | `rgb(0 0 0/.08)` | `rgb(0 0 0/.08)` |

### Shape

| Token | Light | Dark |
| --- | --- | --- |
| `--ui-radius` | `.5rem` | same |
| `--ui-shadow` | `0 10px 30px -10px rgba(15,23,42,.35)` | same |
| `--ui-radius-sm` | `calc(var(--ui-radius) - .125rem)` | same |
| `--ui-radius-pill` | `999px` | same |
| `--ui-radius-round` | `50%` | same |
| `--ui-shadow-sm` | `0 1px 2px rgba(15,23,42,.04)` | same |
| `--ui-shadow-lg` | `var(--ui-shadow)` | same |
| `--ui-border-width` | `1px` | same |
| `--ui-focus-width` | `2px` | same |
| `--ui-accent-width` | `4px` | same |
| `--ui-summary-width` | `3px` | same |

### Spacing and type

| Token | Light | Dark |
| --- | --- | --- |
| `--ui-space` | `.25rem` | same |
| `--ui-font` | `inherit` | same |
| `--ui-font-body` | `.9rem` | same |
| `--ui-font-small` | `.8rem` | same |
| `--ui-font-label` | `.85rem` | same |
| `--ui-font-caption` | `.72rem` | same |
| `--ui-font-badge` | `.75rem` | same |
| `--ui-font-table` | `.925rem` | same |
| `--ui-font-meta` | `.875rem` | same |
| `--ui-font-heading` | `1.05rem` | same |
| `--ui-font-title` | `1rem` | same |
| `--ui-font-stat` | `1.5rem` | same |
| `--ui-font-close` | `1.25rem` | same |
| `--ui-font-dismiss` | `1.1rem` | same |
| `--ui-font-empty` | `2rem` | same |
| `--ui-font-tile-icon` | `1.3rem` | same |
| `--ui-font-modal-icon` | `1.2rem` | same |
| `--ui-font-modal-close` | `1.15rem` | same |
| `--ui-weight-normal` | `400` | same |
| `--ui-weight-semibold` | `600` | same |
| `--ui-weight-bold` | `700` | same |
| `--ui-leading-button` | `1.2` | same |
| `--ui-leading-badge` | `1.4` | same |
| `--ui-leading-stat` | `1.1` | same |
| `--ui-tracking-heading` | `.03em` | same |

### Motion

| Token | Light | Dark |
| --- | --- | --- |
| `--ui-ease` | `.12s ease` | same |
| `--ui-motion-toast` | `.4s` | same |
| `--ui-motion-icon` | `.8s` | same |
| `--ui-motion-spinner` | `.6s` | same |
| `--ui-motion-reduced` | `1.4s` | same |
| `--ui-motion-shimmer` | `1.3s` | same |
| `--ui-motion-skeleton` | `2s cubic-bezier(.4,0,.6,1)` | same |

### Layers

| Token | Light | Dark |
| --- | --- | --- |
| `--ui-z-overlay` | `50` | same |
| `--ui-z-popover` | `55` | same |
| `--ui-z-toast` | `60` | same |

### State

| Token | Light | Dark |
| --- | --- | --- |
| `--ui-opacity-disabled-button` | `.55` | same |
| `--ui-opacity-disabled-control` | `.6` | same |
| `--ui-opacity-disabled-pager` | `.5` | same |
| `--ui-opacity-dismiss` | `.6` | same |
| `--ui-opacity-busy` | `.85` | same |
| `--ui-opacity-empty-icon` | `.65` | same |
| `--ui-opacity-pulse` | `.5` | same |
| `--ui-hover-brightness` | `.97` | same |
| `--ui-soft-mix` | `14%` | same |
| `--ui-accent-mix` | `12%` | same |
| `--ui-selected-mix` | `8%` | same |
