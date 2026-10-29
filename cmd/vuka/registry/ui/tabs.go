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
