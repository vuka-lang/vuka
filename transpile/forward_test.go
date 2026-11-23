package transpile_test

import (
	"strings"
	"testing"
)

const fallCard = `<script lang="go">
package main

import ui "vukatest.local/falltarget"

type Card struct {
	Title    string
	Children ui.Node
}
</script>

<div class="rounded-xl border bg-card" role="region">{Children}</div>
`

const fallPage = `<script lang="go">
package main

import ui "vukatest.local/falltarget"

type Page struct {
	ID   string
	Hint string
	Q    string
	n    int
}

func (p *Page) Save() error { return nil }
</script>

<main>
  @@
</main>
`

// compileFall compiles markup in a page beside Card, and the files given
// as name, source pairs, over falltarget.
func compileFall(t *testing.T, markup string, files ...string) (string, error) {
	t.Helper()
	fs := map[string]string{
		"page.vuka.html": strings.Replace(fallPage, "@@", markup, 1),
		"card.vuka.html": fallCard,
	}
	for i := 0; i+1 < len(files); i += 2 {
		fs[files[i]] = files[i+1]
	}
	return compileFiles(fs)
}

// TestFallthroughShape: a template declaring no Attrs gets a Fallthrough
// field naming its forward element, which takes it: the element's class
// merged, its own attributes ui.Fallthrough.Or, the rest spread.
func TestFallthroughShape(t *testing.T) {
	src, err := compileFall(t, `<Card title="x">a</Card>`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		"Fallthrough ui.Fallthrough `vuka:\"forward=div\"`",
		`<div {c.Fallthrough.Attrs()...} class={c.Fallthrough.Merge("rounded-xl border bg-card")} role={c.Fallthrough.Or("role", "region")}>{c.Children}</div>`,
		`card := Card{Title: "x", Children: ui.HTML(`+"`a`"+`)}`)
}

// TestFallthroughCaller: the attributes no prop takes are the tag's
// Fallthrough, in order: class, HTML, ARIA and data attributes, a ?= one
// as ui.OmitZero, an attr: one as written.
func TestFallthroughCaller(t *testing.T) {
	src, err := compileFall(t, `<Card title="x" class="border-red-200 mt-4" id={ID} data-tester="t" aria-label="Billing" role="note" aria-describedby?={Hint} attr:x-y="z">a</Card>`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src, `card := Card{Title: "x", Fallthrough: ui.Fallthrough{ui.Attr{Name: "class", Value: "border-red-200 mt-4"}, ui.Attr{Name: "id", Value: p.ID}, ui.Attr{Name: "data-tester", Value: "t"}, ui.Attr{Name: "aria-label", Value: "Billing"}, ui.Attr{Name: "role", Value: "note"}, ui.Attr{Name: "aria-describedby", Value: ui.OmitZero(p.Hint)}, ui.Attr{Name: "x-y", Value: "z"}}, Children: ui.HTML(`+"`a`"+`)}`)
}

const fallInput = `<script lang="go">
package main

import ui "vukatest.local/falltarget"

type Input struct {
	Label string
}
</script>

<label>{Label} <input class="h-10 rounded-md border" forward/></label>
`

// TestFallthroughEventsBind: a DOM event no prop takes is the forward
// element's, its call of a method a push; bind:value on a component whose
// forward element is a control is that control's binding; ref and a bool
// attribute pass on as written.
func TestFallthroughEventsBind(t *testing.T) {
	src, err := compileFall(t, `<Card onClick={Save()} ref="card">a</Card>
  <Input label="Email" bind:value={Q} disabled placeholder="you@example.com"/>`, "input.vuka.html", fallInput)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`Card{Fallthrough: ui.Fallthrough{ui.Attr{Name: "onClick", Value: ui.JS.Push(p.Save)}, ui.Attr{Name: "ref", Value: "card"}}`,
		`ui.RegisterPush((*Page).Save)`,
		`Input{Label: "Email", Fallthrough: ui.Fallthrough{ui.Attr{Name: "bind:value", Value: ui.BindValue(p, &p.Q)}, ui.Attr{Name: "disabled", Value: true}, ui.Attr{Name: "placeholder", Value: "you@example.com"}}}`,
		"Fallthrough ui.Fallthrough `vuka:\"forward=input\"`",
		`<label>{i.Label} <input {i.Fallthrough.Attrs()...} class={i.Fallthrough.Merge("h-10 rounded-md border")}/></label>`)
}

func fallComp(name, fields, markup string) string {
	return `<script lang="go">
package main

import ui "vukatest.local/falltarget"

type ` + name + ` struct {
	` + fields + `
}

func (c ` + name + `) Toggle() {}
</script>

` + markup + "\n"
}

func TestFallthroughErrors(t *testing.T) {
	for _, c := range []struct {
		name, comp, markup, code, want string
	}{
		{"two markers", fallComp("Box", "Label string", `<div><span forward>a</span><b forward>b</b></div>`), `<Box/>`, "E0137", "marks two forward elements, <span> (line 13) and <b> (line 13)"},
		{"a marker in a loop", fallComp("Box", "Items []string", "<ul>\n  for _, it := range Items {\n    <li forward>{it}</li>\n  }\n</ul>"), `<Box/>`, "E0137", "inside a for loop"},
		{"a marker's value", fallComp("Box", "Label string", `<div forward="yes">{Label}</div>`), `<Box/>`, "E0137", "forward takes no value"},
		{"a marker beside Attrs", fallComp("Box", "Attrs []ui.Attr", `<div forward {Attrs...}>x</div>`), `<Box/>`, "E0137", "Box declares Attrs"},
		{"several roots", fallComp("Box", "Label string", "<h2>{Label}</h2>\n<p>x</p>"), `<Box id="a"/>`, "E0138", "several roots, <h2>, <p>, and none is marked forward"},
		{"a slot root", fallComp("Box", "Title ui.Node", `<slot:title>Untitled</slot:title>`), `<Box class="mt-4"/>`, "E0138", "Box's markup's root is <slot:title>, not an element"},
		{"a component root", fallComp("Box", "Label string", `<Card title={Label}>x</Card>`), `<Box id="a"/>`, "E0138", "forward element is the component <Card>"},
		{"bind on no control", fallComp("Box", "Label string", `<div>{Label}</div>`), `<Box bind:value={Q}/>`, "E0139", "Box's is <div>, not an input, select or textarea"},
		{"an event the element handles", fallComp("Box", "Label string", `<button onClick={Toggle()}>{Label}</button>`), `<Box onClick={Save()}/>`, "E0139", "which sets onClick itself"},
		{"a misspelt prop", fallComp("Box", "Label string", `<div>{Label}</div>`), `<Box lable="x"/>`, "E0106", "did you mean Label?"},
		{"not an attribute", fallComp("Box", "Label string", `<div>{Label}</div>`), `<Box tone="x"/>`, "E0106", "attr:tone={…} passes it on as written"},
		{"bind on a component with Attrs", fallComp("Box", "Attrs []ui.Attr", `<input {Attrs...}/>`), `<Box bind:value={Q}/>`, "E0106", "takes no bind:value"},
		{"a near prop through attr: skips the check", fallComp("Box", "Label string", `<div>{Label}</div>`), `<Box attr:lable="x" bind:value.debounce.300ms={Q}/>`, "E0139", "Box's is <div>"},
		{"an unused class tag", fallComp("Box", "Class string `vuka:\"class\"`", `<div class="p-4">x</div>`), `<Box/>`, "E0123", "Box's Class is tagged vuka:\"class\" and no class attribute uses it"},
	} {
		_, err := compileFall(t, c.markup, "box.vuka.html", c.comp)
		if err == nil || !strings.Contains(codes(err), c.code) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %s %q, got %v", c.name, c.code, c.want, err)
		}
	}
}

// TestFallthroughClassProp: a Class tagged vuka:"class" takes the caller's
// class, its use in a class attribute merged with the caller's classes
// last; an untagged Class takes it as any prop does. Neither leaves class
// to fall through.
func TestFallthroughClassProp(t *testing.T) {
	tagged := fallComp("Badge", "Class string `vuka:\"class\"`\n\tTone string", `<span class={"inline-flex rounded-md " + Class} data-tone={Tone}>x</span>`)
	sum := fallComp("Pill", "Class string `vuka:\"class\"`", `<span class={Class + " px-2"}>x</span>`)
	plain := fallComp("Tag", "Class string", `<span class={"tag " + Class}>x</span>`)
	src, err := compileFall(t, `<Badge class="rounded-none" id="b"/><Pill class="px-4"/><Tag class="t" title="x"/>`, "badge.vuka.html", tagged, "pill.vuka.html", sum, "tag.vuka.html", plain)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`<span {c.Fallthrough.Attrs()...} class={ui.MergeClasses("inline-flex rounded-md ", c.Class)}`,
		`class={ui.MergeClasses(" px-2", c.Class)}`,
		`<span {c.Fallthrough.Attrs()...} class={"tag " + c.Class}>`,
		`Badge{Class: "rounded-none", Fallthrough: ui.Fallthrough{ui.Attr{Name: "id", Value: "b"}}`,
		`Tag{Class: "t", Fallthrough: ui.Fallthrough{ui.Attr{Name: "title", Value: "x"}}`)
}

// TestFallthroughInteractions: an id a ui.AutoID prop takes is the
// caller's, never a second id; a default stands when the tag passes other
// attributes on; a component with Attrs takes everything there, as before.
func TestFallthroughInteractions(t *testing.T) {
	dialog := fallComp("Dialog", "ID ui.AutoID\n\tSize string `vuka:\"default=md\"`", `<dialog id={ID} data-size={Size}>x</dialog>`)
	button := fallComp("Button", "Attrs []ui.Attr", `<button {Attrs...}>x</button>`)
	src, err := compileFall(t, `<Dialog id="confirm" class="w-96"/><Dialog/><Button class="mt-2" id?={Hint}/>`, "dialog.vuka.html", dialog, "button.vuka.html", button)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`<dialog {c.Fallthrough.All()...} id={c.Fallthrough.Or("id", c.ID)} data-size={c.Fallthrough.Or("data-size", c.Size)}>`,
		`Dialog{ID: "confirm", Fallthrough: ui.Fallthrough{ui.Attr{Name: "class", Value: "w-96"}}, Size: "md"}`,
		`Dialog{ID: ui.NewAutoID(ctx), Size: "md"}`,
		`Button{Attrs: []ui.Attr{ui.Attr{Name: "class", Value: "mt-2"}, ui.Attr{Name: "id", Value: ui.OmitZero(p.Hint)}}}`)
	if strings.Contains(src, "type Button struct {\n//line button.vuka.html:6\n\tFallthrough") {
		t.Errorf("a component with Attrs has a Fallthrough:\n%s", src)
	}
}
