package table

import "github.com/vuka-lang/ui/v2"

// Table keeps its page and sort in the URL, and reads {id}.
type Table struct { // want Table:`urlKeys\(page,sort\)`
	ui.Live
	Sort string
	page int
	id   int64
}

func (t *Table) LiveSchema(l *ui.LiveSchema) {
	l.Field(&t.Sort, ui.URL("sort"))
	l.Field(&t.page, ui.URL("page").Default(1))
	l.Field(&t.id, *new(ui.URL))
}

func (t *Table) Render() ui.Node { return nil }
