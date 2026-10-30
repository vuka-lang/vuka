package vuka_test

import (
	"encoding/json"
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
