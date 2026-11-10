package format

import "testing"

// TestTemplateMultiLineTags: a tag with an attribute spanning lines takes
// an attribute a line, its {Go} broken after { and before } with the
// expression as gofmt writes it; a single-line tag is unchanged.
func TestTemplateMultiLineTags(t *testing.T) {
	const head = "<script lang=\"go\">\npackage p\n\ntype A struct{}\n</script>\n\n"
	for _, c := range []struct{ name, src, want string }{
		{"expression", `<div>
  <button
    type="button"
    onClick={
            ui.JS.Call(editor.Form{Open: editOpen})
          }
  >
    Edit
  </button>
</div>
`, `<div>
  <button
    type="button"
    onClick={
      ui.JS.Call(editor.Form{Open: editOpen})
    }
  >
    Edit
  </button>
</div>
`},
		{"text kept on the >", `<div>
  <tables.ActionMenu icon="pencil-edit-02" onClick={
            ui.JS.Call(editor.Form{Open: editOpen})
          } danger>Edit</tables.ActionMenu>
</div>
`, `<div>
  <tables.ActionMenu
    icon="pencil-edit-02"
    onClick={
      ui.JS.Call(editor.Form{Open: editOpen})
    }
    danger
  >Edit</tables.ActionMenu>
</div>
`},
		{"composite literal", `<orders.Table rows={orders} onSort={ui.JS.Call(orders.Sort{
Column: col,
     Desc:   !desc,
})} />
`, `<orders.Table
  rows={orders}
  onSort={
    ui.JS.Call(orders.Sort{
      Column: col,
      Desc:   !desc,
    })
  }
/>
`},
		{"spread", `<input {
  attrs...
} name="q">
`, `<input
  {
    attrs...
  }
  name="q"
/>
`},
		{"in blocks", `<ul>
  for _, p := range products {
    if p.Visible {
    <li class:selected={p.ID == selected} onClick.once={
        Pick(p.ID)} key={p.ID}>{p.Name}</li>
    }
  }
</ul>
`, `<ul>
  for _, p := range products {
    if p.Visible {
      <li
        class:selected={p.ID == selected}
        onClick.once={
          Pick(p.ID)
        }
        key={p.ID}
      >{p.Name}</li>
    }
  }
</ul>
`},
		{"unparsable", `<button onClick={
      open = false
  Save()
    } disabled={
 busy }>Save</button>
`, `<button
  onClick={
      open = false
  Save()
    }
  disabled={
    busy
  }
>Save</button>
`},
		{"statements on one line", `<button onClick={open = false; Save()}   type="button">Save</button>
`, `<button onClick={open = false; Save()} type="button">Save</button>
`},
		{"single line", `<p><users.Card user={u} model.lazy={name} danger /></p>
`, `<p><users.Card user={u} model.lazy={name} danger /></p>
`},
		{"raw element", `<pre class={
  cls}>x</pre>
`, `<pre class={
  cls}>x</pre>
`},
		{"teleport and slot", `<ui.Teleport to="body">
  <ui.Dialog open={open} onClose={
    ui.JS.Call(editor.Form{Open: false})}>
    <slot:footer><b>ok</b></slot:footer>
  </ui.Dialog>
</ui.Teleport>
`, `<ui.Teleport to="body">
  <ui.Dialog
    open={open}
    onClose={
      ui.JS.Call(editor.Form{Open: false})
    }
  >
    <slot:footer><b>ok</b></slot:footer>
  </ui.Dialog>
</ui.Teleport>
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			check(t, head+c.src, "", head+c.want, Options{Tailwind: true})
		})
	}
}
