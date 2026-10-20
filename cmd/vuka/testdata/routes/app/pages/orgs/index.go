package orgs

import (
	"github.com/vuka-lang/ui"

	"example.com/app/pages/orgs/org"
)

// Index lists the organisations.
type Index struct {
	ui.Live
	First string
}

func NewIndex() *Index { return &Index{First: org.IndexURL(1)} }

func (x *Index) Render() ui.Node { return ui.Text(x.First + NewURL()) }
