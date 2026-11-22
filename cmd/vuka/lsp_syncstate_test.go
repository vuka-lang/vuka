package main

import (
	"strings"
	"testing"
)

// TestHoverSyncedState: hovering a browser-state field says what the
// classification made of it (one state, §12) — synced, with the methods
// whose Go reads or writes it, or browser-local.
func TestHoverSyncedState(t *testing.T) {
	src := []byte(`package shop

import "github.com/vuka-lang/ui/v2"

type Search struct {
	ui.Live
	q    ui.State[string]
	note ui.State[string]
}

func (s *Search) Find() {
	_ = s.q
}
<div>
  <input value={q} />
  <input value={note} />
  <button onClick={s.Find}>go</button>
</div>
`)
	s := scanShape("search.vuka.html", src)
	doc := func(name string) string {
		for _, r := range s.refs {
			if r.name == name && r.kind == refBrowser {
				return s.refDoc("search.vuka.html", r)
			}
		}
		t.Fatalf("no browser-state ref %q in %v", name, s.refs)
		return ""
	}
	q := doc("q")
	if !strings.Contains(q, "Synced") || !strings.Contains(q, "`Find`") {
		t.Errorf("q's hover doesn't say it is synced and why:\n%s", q)
	}
	note := doc("note")
	if !strings.Contains(note, "Browser-local") || strings.Contains(note, "Synced") {
		t.Errorf("note's hover doesn't say it is browser-local:\n%s", note)
	}
}
