package parts

import (
	"context"
	"io"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/vuka/v2"
)

type Index struct{}

func (p *Index) Stats(ctx context.Context) (int, error)  { return 1, nil }
func (p *Index) Recent(ctx context.Context) (int, error) { return 1, nil }
func (p *Index) Unused(ctx context.Context) (int, error) { return 1, nil }
func (p *Index) Count() int                              { return 1 }
func (p *Index) Twice(ctx context.Context) (int, error)  { return 1, nil }

func (p *Index) Render(ctx context.Context, w io.Writer) error {
	n, _ := p.Recent(ctx) // want `E0343: ui.Defer: Recent is called from Go \(parts.go:20\), where its result is needed now`
	_ = n
	return ui.Part(p.Stats, nil, func(n int) ui.Node { return nil }).Render(ctx, w)
}

type Card struct{ Page *Index }

func (c *Card) Render(ctx context.Context, w io.Writer) error {
	return ui.Part(c.Page.Recent, nil, func(n int) ui.Node { return nil }).Render(ctx, w) // want `E0349: ui.Part names Index.Recent in Card's method: a part is its own page's or component's`
}

type Service struct{}

func (s *Service) Load(ctx context.Context) (int, error) { return 1, nil }

func load(ctx context.Context) (int, error) { return 1, nil }

type Live struct{ ui.Live }

func (l *Live) Stats(ctx context.Context) (int, error) { return 1, nil }
func (l *Live) Render() ui.Node                        { return nil }

func init() {
	vuka.RegisterFunc((*Index).Stats, ui.Defer(""))
	vuka.RegisterFunc((*Index).Recent, ui.Defer("g")) // want `E0342: ui.Defer: no ui.Part of Index names Recent`
	vuka.RegisterFunc((*Index).Unused, ui.Defer("")) // want `E0342: ui.Defer: no ui.Part of Index names Unused, so there is no part to defer`
	vuka.RegisterFunc((*Index).Count, ui.Defer(""))  // want `E0347: ui.Defer: Index.Count is func\(\) int; a deferred method takes a context.Context and returns \(T, error\)`
	vuka.RegisterFunc((*Index).Twice, ui.Defer(""), ui.Defer("g")) // want `E0345: Index.Twice has ui.Defer twice`
	vuka.RegisterFunc((*Service).Load, ui.Defer(""))                // want `E0341: ui.Defer goes on a page's or component's method: Load is a method of Service, which renders nothing`
	vuka.RegisterFunc(load, ui.Defer(""))                           // want `E0341: ui.Defer goes on a page's or component's method: load is a function`
	vuka.RegisterFunc((*Live).Stats, []any{ui.Defer("")}...)        // want `E0346: ui.Defer is for stateless pages; Live is live`
}
