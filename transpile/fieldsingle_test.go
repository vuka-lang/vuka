package transpile_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestSingleAttrsWarn: each of ui's single field attributes is warned
// (W0314) with what to write instead — the tag, or Spec's member where no
// tag says the same or the field is a ui.Meta member — and one with
// nothing to replace it there isn't.
func TestSingleAttrsWarn(t *testing.T) {
	const src = `package main

import (
	"vukatest.local/orm"
	ui "vukatest.local/stateui"
)

type Signup struct {
	Email    string  @ui.Email("Enter a work address") @ui.Label("Work email") @ui.Autocomplete("email")
	Password string  @ui.Widget("password") @ui.Min(12) @ui.Msg("min", "At least {n}, max=no")
	Plan     string  @ui.Help("Pick one") @ui.OneOf("free", "pro")
	Code     string  @ui.Required("Enter the code") @ui.Pattern("[A-Z]+") @ui.Placeholder("ABC")
	Seats    int     @ui.Max(50) @ui.Required("Seats, please")
	Rate     float64 @ui.Min(0.5)
	Terms    bool    @ui.Required
	Count    int     @ui.Required
}

type Product struct {
	orm.Base
	Name string
	Note string
	Mail string
}

type ProductForm struct {
	ui.FormFor[Product]

	Name ui.Meta @ui.Placeholder("e.g. Blue mug") @ui.Max(60) @ui.Required("Name is required.")
	Note ui.Meta @ui.Msg("max", "Too long") @ui.Min(0.5)
	Mail ui.Meta @ui.Email @ui.Required
}
`
	dir, _ := filepath.Abs("testdata/golden")
	res, err := transpile.Package([]transpile.File{{Name: "w.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range res.Files[0].Warnings {
		if w.Code == "W0314" {
			got = append(got, w.Error())
		}
	}
	want := []string{
		"w.vuka:9:19: @ui.Email is the validate: rule email — write `validate:\"email\" msg:\"email=Enter a work address\"`; vuka fix attributes writes it",
		"w.vuka:9:53: @ui.Label is the label: tag — write `label:\"Work email\"`, or set Label in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:9:77: @ui.Autocomplete is the autocomplete: tag — write `autocomplete:\"email\"`, or set Autocomplete in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:10:19: @ui.Widget is the input: tag — write `input:\"password\"`, or set Widget in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:10:42: @ui.Min is the validate: rule min — write `validate:\"min=12\"`, or set Min in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:10:54: @ui.Msg is the msg: tag — write `msg:\"min=At least {n}\\\\, max=no\"`, or set Msg in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:11:19: @ui.Help is the help: tag — write `help:\"Pick one\"`, or set Help in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:11:40: @ui.OneOf is Choices in @ui.Spec — write @ui.Spec{Choices: []string{\"free\", \"pro\"}}; vuka fix attributes writes it",
		"w.vuka:12:19: @ui.Required is the validate: rule required — write `validate:\"required\" msg:\"required=Enter the code\"`, or set Required in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:12:50: @ui.Pattern is Pattern in @ui.Spec — write @ui.Spec{Pattern: \"[A-Z]+\"}; vuka fix attributes writes it",
		"w.vuka:12:72: @ui.Placeholder is the placeholder: tag — write `placeholder:\"ABC\"`, or set Placeholder in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:13:19: @ui.Max is the validate: rule max — write `validate:\"max=50\"`, or set Max in @ui.Spec; vuka fix attributes writes it",
		"w.vuka:13:31: @ui.Required is Required in @ui.Spec — write @ui.Spec{Required: \"Seats, please\"}; vuka fix attributes writes it",
		"w.vuka:14:19: @ui.Min is the validate: rule min — write `validate:\"min=0.5\"`; vuka fix attributes writes it",
		"w.vuka:15:19: @ui.Required is the validate: rule required — write `validate:\"required\"`; vuka fix attributes writes it",
		"w.vuka:29:15: @ui.Placeholder on a ui.Meta member is Placeholder in @ui.Spec — write @ui.Spec{Placeholder: \"e.g. Blue mug\"}; vuka fix attributes writes it",
		"w.vuka:29:48: @ui.Max on a ui.Meta member is Max in @ui.Spec — write @ui.Spec{Max: 60}; vuka fix attributes writes it",
		"w.vuka:29:60: @ui.Required on a ui.Meta member is Required in @ui.Spec — write @ui.Spec{Required: \"Name is required.\"}; vuka fix attributes writes it",
		"w.vuka:30:15: @ui.Msg on a ui.Meta member is Msg in @ui.Spec — write @ui.Spec{Msg: ui.Msgs{\"max\": \"Too long\"}}; vuka fix attributes writes it",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
