package vuka_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2"
)

type released struct {
	Name     string
	SKU      vuka.Option[string]    `json:"sku"`
	At       vuka.Option[time.Time] `json:"at"`
	Size     vuka.Option[size]      `json:"size"`
	Scores   []vuka.Option[int]     `json:"scores"`
	Note     vuka.Option[string]    `json:"note,omitzero"`
	Previous vuka.Option[string]    `json:"previous,omitempty"`
}

type size struct{ W, H int }

// TestOptionJSON: an Option is its value or null in JSON, both ways; a None
// tagged omitzero is left out.
func TestOptionJSON(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)
	in := released{
		Name:   "kettle",
		SKU:    vuka.Some("K-1"),
		At:     vuka.Some(at),
		Size:   vuka.Some(size{3, 4}),
		Scores: []vuka.Option[int]{vuka.Some(1), vuka.None[int](), vuka.Some(0)},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Name":"kettle","sku":"K-1","at":"2026-10-30T09:30:00Z","size":{"W":3,"H":4},"scores":[1,null,0],"previous":null}`
	if string(b) != want {
		t.Fatalf("marshal:\n got %s\nwant %s", b, want)
	}
	var out released
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.At.IsSome() || !out.At.Unwrap().Equal(at) || out.SKU.Unwrap() != "K-1" || out.Size.Unwrap() != (size{3, 4}) ||
		len(out.Scores) != 3 || out.Scores[1].IsSome() || out.Scores[2].Unwrap() != 0 || out.Note.IsSome() || out.Previous.IsSome() {
		t.Errorf("unmarshal: %+v", out)
	}
	none, _ := json.Marshal(released{})
	if string(none) != `{"Name":"","sku":null,"at":null,"size":null,"scores":null,"previous":null}` {
		t.Errorf("None: %s", none)
	}
	out = released{SKU: vuka.Some("old")}
	if err := json.Unmarshal([]byte(`{"sku":null}`), &out); err != nil || out.SKU.IsSome() {
		t.Errorf("null into Some: %v %v", err, out.SKU)
	}
	if err := json.Unmarshal([]byte(`{"sku":7}`), &out); err == nil {
		t.Error("a number into Option[string]: no error")
	}
}

type upper string

func (u upper) MarshalText() ([]byte, error) { return []byte(strings.ToUpper(string(u))), nil }

func (u *upper) UnmarshalText(b []byte) error { *u = upper(strings.ToLower(string(b))); return nil }

type kind string

// TestOptionText: Some is the text of its value, as T's own methods or
// strconv write it, and None is empty; text reads back as Some, the empty
// text too for a string; a T with no text form is an error.
func TestOptionText(t *testing.T) {
	text := func(o interface{ MarshalText() ([]byte, error) }) string {
		b, err := o.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, tc := range []struct {
		got, want string
	}{
		{text(vuka.None[int]()), ""},
		{text(vuka.Some(0)), "0"},
		{text(vuka.Some(-7)), "-7"},
		{text(vuka.Some(uint8(9))), "9"},
		{text(vuka.Some(1.5)), "1.5"},
		{text(vuka.Some(true)), "true"},
		{text(vuka.Some("")), ""},
		{text(vuka.Some("a b")), "a b"},
		{text(vuka.Some(kind("k"))), "k"},
		{text(vuka.Some(upper("x"))), "X"},
		{text(vuka.Some(time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC))), "2026-10-30T09:30:00Z"},
	} {
		if tc.got != tc.want {
			t.Errorf("text %q, want %q", tc.got, tc.want)
		}
	}
	var n vuka.Option[int64]
	if err := n.UnmarshalText([]byte("0")); err != nil || n != vuka.Some[int64](0) {
		t.Errorf("0: %v, %v", n, err)
	}
	var s vuka.Option[string]
	if err := s.UnmarshalText(nil); err != nil || s != vuka.Some("") {
		t.Errorf("empty string: %v, %v", s, err)
	}
	var u vuka.Option[upper]
	if err := u.UnmarshalText([]byte("AB")); err != nil || u.Value() != "ab" {
		t.Errorf("upper: %v, %v", u, err)
	}
	var tm vuka.Option[time.Time]
	if err := tm.UnmarshalText([]byte("2026-10-30T09:30:00Z")); err != nil || tm.IsNone() {
		t.Errorf("time: %v, %v", tm, err)
	}
	for _, bad := range []string{"", "x", "1.5"} {
		var i vuka.Option[int]
		if err := i.UnmarshalText([]byte(bad)); err == nil || i.IsSome() {
			t.Errorf("int %q: %v, %v", bad, i, err)
		}
	}
	var i8 vuka.Option[int8]
	if err := i8.UnmarshalText([]byte("300")); err == nil {
		t.Error("int8 300 fits")
	}
	if _, err := vuka.Some(size{1, 2}).MarshalText(); err == nil {
		t.Error("a struct has a text form")
	}
	var sz vuka.Option[size]
	if err := sz.UnmarshalText([]byte("1")); err == nil {
		t.Error("a struct reads text")
	}
	b, _ := json.Marshal(vuka.Some(7))
	if string(b) != `7` {
		t.Errorf("JSON is not the value's: %s", b)
	}
}
