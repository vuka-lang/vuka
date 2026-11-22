// Package design declares prop types of another package, for the checks of
// the quoted values set on them (enumlit_test.go).
package design

// Tone is a component's colour.
type Tone string

const (
	ToneNeutral Tone = ""
	ToneInfo    Tone = "info"
	ToneDanger  Tone = "danger"
)

// Selector is a CSS selector; This is the one preset.
//
//vuka:open
type Selector string

const This Selector = ""

type (
	// Gap is a spacing step.
	//vuka:open
	Gap string
	// Width is a column count.
	Width int
)

const (
	GapSm Gap = "sm"

	Width1 Width = 1
	Width2 Width = 2
)
