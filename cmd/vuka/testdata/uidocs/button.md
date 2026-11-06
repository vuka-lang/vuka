```vuka-html
<design.Button on={Save}>Save</design.Button>
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
<design.Button variant={design.ButtonGhost} size={design.SizeIcon} attrs={closeLabel}><svg>…</svg></design.Button>
<design.Button type="submit" disabled={busy}>Send</design.Button>
<design.Button onClick={open = !open}>Menu</design.Button>
```

`onClick` takes what an element's `onClick` does: statements writing
[browser state](/ui/state) (`onClick={open = true}`), calls among them
(`onClick={open = false; Save()}`), a `ui.JS` chain. `on` takes a live
handler or a `ui.JS` chain; `onClick` wins when both are set. The variant can be written as
its string too: `variant="outline"`.

The classes are data: `buttonVariants`, a [`ui.Variants`](/ui/classes),
holds each variant's and size's classes, and
`buttonVariants.Class("variant", …, "size", …, "class", Class)` merges them
with yours last, so your `class` wins. A new variant is a constant and an
entry under `"variant"`; the editor completes `variant="…"` from them.
