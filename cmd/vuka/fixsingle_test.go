package main

import (
	"os"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
)

// TestFixSingleAttrs: vuka fix attributes writes a form field's single ui
// attributes as its tags and Spec members, byte for byte as
// testdata/fixsingle/out.vuka has them, notes the fields it leaves, and a
// second run changes nothing.
func TestFixSingleAttrs(t *testing.T) {
	t.Parallel()
	in, err := os.ReadFile("testdata/fixsingle/in.vuka")
	if err != nil {
		t.Fatal(err)
	}
	out, n, err := attributeTags(in)
	if err != nil || n != 0 {
		t.Fatalf("attributeTags: %d %v", n, err)
	}
	out, n, notes := singleAttrTags(out)
	if src, err := format.Source(out); err == nil {
		out = src
	}
	want, err := os.ReadFile("testdata/fixsingle/out.vuka")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(want) {
		if os.Getenv("VUKA_UPDATE") != "" {
			os.WriteFile("testdata/fixsingle/out.vuka", out, 0o644)
		}
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if n != 12 {
		t.Errorf("%d fields changed, want 12", n)
	}
	wantNotes := []string{
		"Signup.Count keeps its single attributes: @ui.Required with no message on a number: a ui.FormFor requires the field by its type; elsewhere write validate:\"required\"",
		"Product.Name keeps its single attributes: @ui.Max beside the model's attributes, which bound the field too: write it in @ui.Spec where it tightens them",
		"ProductForm.Other keeps its single attributes: @ui.Email has no Spec spelling on a ui.Meta member",
	}
	if got := strings.Join(notes, "\n"); got != strings.Join(wantNotes, "\n") {
		t.Errorf("notes\n%s\nwant\n%s", got, strings.Join(wantNotes, "\n"))
	}
	again, n, _ := singleAttrTags(out)
	if n != 0 || string(again) != string(out) {
		t.Errorf("a second run changed %d fields", n)
	}
}
