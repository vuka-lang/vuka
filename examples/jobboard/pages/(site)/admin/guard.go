package admin

import (
	"context"

	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

// Guard lets only moderators into /admin.
type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	if !auth.Can(ctx, "jobs.moderate") {
		return web.Forbidden
	}
	return nil
}
