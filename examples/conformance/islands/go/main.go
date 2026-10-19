// Command islands is a dashboard in plain Go placing script islands (no
// Node): a chart in a live page, its props following the page, its emits
// the page's events; and a badge in a page that isn't live.
package main

import (
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

//go:generate go tool vuka gen -inplace

func init() {
	ui.RegisterIsland[Chart]()
	ui.RegisterIsland[Badge]()
	web.Routes.Live("/", NewDashboard)
	web.Routes.Get("/about", About)
}

func main() {
	app := web.New()
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
