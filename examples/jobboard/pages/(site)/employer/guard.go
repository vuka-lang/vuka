package employer

import (
	"context"

	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

// Guard lets only employers into /employer and below.
type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	if !auth.Can(ctx, "jobs.post") {
		return web.Forbidden
	}
	return nil
}
