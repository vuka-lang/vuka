package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

func main() {
	ctx := web.WithVars(context.Background(), web.RequestVars{Request: web.Request{Path: "/orders"}})
	ctx = web.WithVars(ctx, web.CSRFVars{CSRFToken: "t0k"})
	anon := web.WithVars(ctx, auth.Vars{})
	ada := web.WithVars(ctx, auth.Vars{User: &auth.Identity{ID: "1", Name: "Ada"}, Perms: auth.Perms{"orders.*"}})
	for _, c := range []context.Context{anon, ada} {
		out, err := ui.String(c, Header{Title: "Orders"})
		fmt.Println(out, err, greeting(c))
	}
	out, err := ui.String(ada, Shadow{})
	fmt.Println(out, err)
}
