package variants

import "github.com/vuka-lang/ui/v2"

var alert = ui.Variants{ // want alert:"variants"
	Base: "flex rounded-lg border px-4 py-3",
	Variants: map[string]map[string]string{
		"tone": {"error": "bg-red-50", "info": "bg-gray-50"},
		"size": {"sm": "px-2", "md": ""},
	},
	Defaults: map[string]string{"tone": "inf", "sise": "sm"}, // want `E0371: the variant tone has no value "inf": it is "error" or "info"` `E0371: the variants have no "sise": they are "size" or "tone"`
}

// Alert passes its props to the variants.
type Alert struct {
	Tone  string // want Tone:`variant\(tone\)`
	Size  string // want Size:`variant\(size\)`
	Class string
}

func (a Alert) View() ui.Node {
	return ui.HTML(`<div class={alert.Class("tone", a.Tone, "size", a.Size, "class", a.Class)}/>`, alert.Class("tone", a.Tone, "size", a.Size, "class", a.Class))
}

func (a Alert) Badge() string {
	return alert.Class("tone", "eror", "colour", "red") // want `E0371: the variant tone has no value "eror": it is "error" or "info"` `E0371: alert's variants have no "colour": they are "size" or "tone"`
}

// Page sets the props.
type Page struct{}

func (Page) View() ui.Node {
	ok := Alert{Tone: "error", Size: ""}
	bad := Alert{Tone: "eror", Size: "lg"} // want `E0371: Alert's Tone is its variant tone, which has no value "eror": it is "error" or "info"` `E0371: Alert's Size is its variant size, which has no value "lg": it is "md" or "sm"`
	return ui.HTML(`{ok}{bad}`, ok, bad)
}
