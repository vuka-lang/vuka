package route

import (
	"context"
	"io"

	"github.com/vuka-lang/ui"
)

// Index is the client app's shell, the same for every /app/... URL.
type Index struct {
	Params
}

func NewIndex() *Index { return &Index{} }

func (p *Index) Title() string { return "App" }

func (p *Index) Render(ctx context.Context, w io.Writer) error {
	return ui.HTML(`<div id="app"></div>
<script>document.getElementById("app").textContent = "client route: " + location.pathname</script>`).Render(ctx, w)
}
