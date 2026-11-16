package transpile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A route decorator on an @ui.Island struct routes the page its component
// backs, and stays; on any other type it declares a page outside pages/
// (E0720). The check needs the web import resolved, so an importer that
// can't resolve it (a module whose go.mod go list won't complete) skips it
// — which is why the LSP reported E0720 on routed islands only under
// GOFLAGS=-mod=mod.
func TestRoutedIslandIsNoPageDecorator(t *testing.T) {
	dir := "testdata/golden"
	src := `package main

import (
	ui "vukatest.local/stateui"
	"github.com/vuka-lang/web/v2"
)

@web.Live("/board")
@ui.Island("components/Board.vue")
type Board struct {
	ui.Live
	Q string
}

@web.Get("/stats")
@ui.Island("components/Stats.vue", "load")
type Stats struct {
	Views int
}

@web.Live("/plain")
type Plain struct{}
`
	_, err := transpile.Package([]transpile.File{{Name: "routedislands.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
	var list transpile.ErrorList
	if err != nil && !errors.As(err, &list) {
		t.Fatal(err)
	}
	plain := strings.Count(src[:strings.Index(src, `@web.Live("/plain")`)], "\n") + 1
	for _, e := range list {
		if e.Code != "E0720" {
			continue
		}
		if e.Pos.Line != plain {
			t.Errorf("E0720 at line %d: a routed island is the page its component backs, not a page decorator\n%v", e.Pos.Line, e)
		}
		plain = -1 // found the one expected
	}
	if plain != -1 {
		t.Errorf("no E0720 for @web.Live on a plain type:\n%v", err)
	}
}
