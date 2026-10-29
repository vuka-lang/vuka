```vuka-html
<design.Button on={Save}>Save</design.Button>
<design.Button variant={design.ButtonOutline} size={design.SizeSm}>Cancel</design.Button>
<design.Button variant={design.ButtonGhost} size={design.SizeIcon} attrs={closeLabel}><svg>…</svg></design.Button>
<design.Button type="submit" disabled={busy}>Send</design.Button>
<design.Button on={ui.JS.Toggle(ui.Sel("#menu"))}>Menu</design.Button>
```

`on` takes a live handler or a `ui.JS` chain. The variant can be written as
its string too: `variant="outline"`.

The classes live in two Go maps in the file, `buttonVariant` and
`buttonSize`, joined by `buttonVariants(v, s)`: a new variant is a constant
and a map entry.
