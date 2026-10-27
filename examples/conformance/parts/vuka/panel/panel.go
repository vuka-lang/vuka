// Package panel is the parts page's figures written in Go, as a component:
// ui.Part for each part, vuka.RegisterFunc for each method's timing.
package panel

import (
	"context"
	"io"

	"example.com/app/store"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/vuka/v2"
)

// Figures shows the account's figures and lists, deferred.
type Figures struct {
	store store.Store
}

func init() {
	vuka.RegisterFunc((*Figures).Counts, ui.Defer(""))
	vuka.RegisterFunc((*Figures).Recent, ui.Defer("lists"))
	vuka.RegisterFunc((*Figures).Tags, ui.Defer("lists"))
	vuka.RegisterFunc((*Figures).Alerts, ui.Defer(""))
}

// Counts are the account's counts.
func (p *Figures) Counts(ctx context.Context) (store.Figures, error) { return p.store.Figures(ctx) }

// Recent are the latest applications.
func (p *Figures) Recent(ctx context.Context) ([]string, error) { return p.store.Recent(ctx) }

// Tags are the account's tags.
func (p *Figures) Tags(ctx context.Context) ([]string, error) { return p.store.Tags(ctx) }

// Alerts fail.
func (p *Figures) Alerts(ctx context.Context) (int, error) { return p.store.Alerts(ctx) }

// View is its markup: a part for each deferred method.
func (p *Figures) View() ui.Node {
	return ui.El("section", []ui.Attr{{Name: "class", Value: "panel"}},
		ui.El("h2", nil, ui.Text("My applications, in Go")),
		ui.Part(p, (*Figures).Counts, ui.El("span", []ui.Attr{{Name: "class", Value: "skeleton"}}, ui.Text("…")), func(f store.Figures) ui.Node {
			return ui.El("p", []ui.Attr{{Name: "class", Value: "figures"}}, ui.Textf("%d open · %d shortlisted", f.Open, f.Shortlisted))
		}),
		ui.Part(p, (*Figures).Recent, nil, func(rs []string) ui.Node {
			var lis []ui.Node
			for _, r := range rs {
				lis = append(lis, ui.El("li", nil, ui.Text(r)))
			}
			return ui.El("ul", []ui.Attr{{Name: "class", Value: "recent"}}, lis...)
		}),
		ui.Part(p, (*Figures).Tags, nil, func(ts []string) ui.Node {
			return ui.El("p", []ui.Attr{{Name: "class", Value: "tags"}}, ui.Textf("%d tags", len(ts)))
		}),
		ui.Part(p, (*Figures).Alerts, ui.PartFallback{Error: ui.El("p", []ui.Attr{{Name: "class", Value: "alerts"}}, ui.Text("Alerts are unavailable."))}, func(n int) ui.Node {
			return ui.El("p", []ui.Attr{{Name: "class", Value: "alerts"}}, ui.Textf("%d alerts", n))
		}),
	)
}

func (p *Figures) Render(ctx context.Context, w io.Writer) error { return p.View().Render(ctx, w) }
