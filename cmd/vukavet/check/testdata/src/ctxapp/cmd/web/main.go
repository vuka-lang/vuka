package main

import (
	"ctxapp/settings"
	"ctxapp/users"
	_ "ctxapp/views"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

var _ = web.ContextProcessors( // want `E0427: .*views.go:14:6: web.Var reads "siteName" as int, but settings.Context gives it as string` `E0427: .*views.go:15:9: web.Var reads "sitName", which no context processor in this list gives`
	web.RequestContext,
	auth.Context,
	web.Browser(settings.Context),
	web.Browser(users.Context),               // want `E0426: users.Context reads the signed-in user`
	web.Browser(web.Personal(users.Context)), // want `E0421: users.Context is listed twice`
	users.Other,                              // want `E0421: users.Context \(users.go:13\) and users.Other \(users.go:23\) both provide me`
	users.Count,                              // want `E0425: users.Count isn't a context processor: it doesn't return \(S, error\)`
)

func main() {}
