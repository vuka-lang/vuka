package main

import (
	"slices"
	"strings"
	"testing"
)

// web's first listen line is the banner; the app restarting at the same
// address says nothing, at another one line.
func TestTermStatusBanner(t *testing.T) {
	ts, b := newTestTerm()
	ts.version = "v2.0.0"
	app := ts.wrap(b)
	app.Write([]byte("2026/01/02 12:04:01 web: listening on http://localhost:8080\n"))
	want := []string{"", "  vuka v2.0.0  dev", "", "  ➜  Local:   http://localhost:8080", "", ""}
	if got := screen(b.String()); !slices.Equal(got, want) {
		t.Errorf("banner without the count:\n%q\nwant\n%q", got, want)
	}

	ts, b = newTestTerm()
	ts.version = "v2.0.0"
	ts.setPackages(216)
	app = ts.wrap(b)
	app.Write([]byte("2026/01/02 12:04:01 web: listening on http://localhost:8080\n"))
	if got := screen(b.String()); got[1] != "  vuka v2.0.0  dev — 216 packages" {
		t.Errorf("banner: %q", got)
	}
	n := len(b.String())
	app.Write([]byte("2026/01/02 12:04:09 web: listening on http://localhost:8080\n"))
	if len(b.String()) != n {
		t.Errorf("a restart said %q", b.String()[n:])
	}
	app.Write([]byte("GET /\n2026/01/02 12:05:00 web: listening on http://localhost:8081\nready\n"))
	got := screen(b.String())
	if want := []string{"GET /", "  ➜  Local:   http://localhost:8081", "ready", ""}; !slices.Equal(got[len(got)-4:], want) {
		t.Errorf("another address: %q", got)
	}
	if strings.Count(b.String(), "vuka v2.0.0") != 1 {
		t.Errorf("the banner twice: %q", got)
	}
}

// Only a whole line of the app's is one: a line it continues, or one that
// only mentions it, goes through.
func TestTermStatusBannerConservative(t *testing.T) {
	for _, in := range [][]string{
		{"note: ", "web: listening on http://localhost:8080\n"},
		{"web: listening on http://localhost:8080"},
		{"web: listening on localhost:8080\n"},
		{"web: listening on http://localhost:8080 (tls)\n"},
		{"the docs say web: listening on http://x/ and more\n"},
	} {
		ts, b := newTestTerm()
		app := ts.wrap(b)
		for _, p := range in {
			app.Write([]byte(p))
		}
		if got := b.String(); got != strings.Join(in, "") {
			t.Errorf("%q came out as %q", in, got)
		}
	}
}
