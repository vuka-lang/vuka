package orgs

import (
	"github.com/vuka-lang/ui"

	"example.com/app/pages/urls"
)

// Index lists the organisations.
type Index struct {
	ui.Live
	First string
}

func NewIndex() *Index { return &Index{First: urls.Orgs.Org(1).Path()} }

func (x *Index) Render() ui.Node { return ui.Text(x.First + urls.Orgs.New.Path()) }
