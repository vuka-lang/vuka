```vuka-html
<design.Button on={Save}>Save</design.Button>
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
<design.Button variant={design.ButtonGhost} size={design.SizeIcon} attrs={closeLabel}><svg>…</svg></design.Button>
<design.Button type="submit" disabled={busy}>Send</design.Button>
<design.Button on={ui.JS.Toggle(ui.Sel("#menu"))}>Menu</design.Button>
```

`on` takes a live handler or a `ui.JS` chain. The variant can be written as
its string too: `variant="outline"`.

The classes are data: `buttonVariants`, a [`ui.Variants`](/ui/classes),
holds each variant's and size's classes, and
`buttonVariants.Class("variant", …, "size", …, "class", Class)` merges them
with yours last, so your `class` wins. A new variant is a constant and an
entry under `"variant"`; the editor completes `variant="…"` from them.
