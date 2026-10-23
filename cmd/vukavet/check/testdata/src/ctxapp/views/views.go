// What vuka gen writes for markup reading processor variables, and a
// library's explicit reads.
package views

import (
	"context"

	"ctxapp/users"
	"github.com/vuka-lang/web/v2"
)

func Header(ctx context.Context) string {
	_ = web.Var[*users.User](ctx, "me")
	_ = web.Var[int](ctx, "siteName")
	return web.Var[string](ctx, "sitName")
}
