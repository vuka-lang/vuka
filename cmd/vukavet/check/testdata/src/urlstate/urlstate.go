package urlstate

import (
	"github.com/vuka-lang/ui/v2"

	"urlstate/table"
)

// Tabs keeps its tab in the URL.
type Tabs struct { // want Tabs:`urlKeys\(tab\)`
	ui.Live
	tab string
}

func (t *Tabs) LiveSchema(l *ui.LiveSchema) { l.Field(&t.tab, ui.URL("tab")) }
func (t *Tabs) Render() ui.Node             { return nil }

// Board renders two tables each under a key of its own, and two tabs
// sharing one.
type Board struct{ Prefix string }

func (b Board) View() ui.Node {
	return ui.HTML(`…`,
		ui.URLKey("requests", &table.Table{}),
		ui.URLKey("users", ui.Key(1, &table.Table{})),
		ui.URLKey(b.Prefix, &table.Table{}),
		&Tabs{},
		&Tabs{}, // want `W0307: Tabs and the Tabs before it both keep the query parameter tab`
	)
}

// Report renders two tables without keys.
type Report struct{}

func (r *Report) Render() ui.Node {
	return ui.HTML(`…`,
		&table.Table{},
		&table.Table{}, // want `W0307: Table and the Table before it both keep the query parameter page` `W0307: Table and the Table before it both keep the query parameter sort`
		ui.URLKey("a", &table.Table{}),
		ui.URLKey("a", &table.Table{}), // want `W0307: Table \(urlKey "a"\) and the Table before it both keep the query parameter a.page` `query parameter a.sort`
	)
}

// Grid is generic: each instantiation keeps its fields in the URL.
type Grid[K comparable, V any] struct { // want Grid:`urlKeys\(page\)`
	ui.Live
	Rows map[K]V
	page int
}

func (g *Grid[K, V]) LiveSchema(l *ui.LiveSchema) { l.Field(&g.page, ui.URL("page")) }
func (g *Grid[K, V]) Render() ui.Node             { return nil }

// Grids renders two instantiations: under keys of their own, and without.
type Grids struct{}

func (g *Grids) Render() ui.Node {
	return ui.HTML(`…`,
		ui.URLKey("tasks", &Grid[string, int]{}),
		ui.URLKey("users", &Grid[int, string]{}),
		&Grid[string, int]{},
		&Grid[int, string]{}, // want `W0307: Grid and the Grid before it both keep the query parameter page`
	)
}
