package main

import (
	"example.com/app/orders"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

var _ = orders.NewBoard

func main() {
	_ = web.New(web.ContextProcessors(web.RequestContext, auth.Context))
}
