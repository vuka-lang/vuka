// Command islands-go is a vuka app in plain Go with islands: a Vue
// component placed in a live page (Chart) and a page backed by its
// component (Profile). go generate builds them into web/dist, which the
// binary embeds; under VUKA_DEV=1 go run . web.Dev keeps them built.
package main

import (
	"embed"
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

//go:generate go tool vuka build-islands

//go:embed all:web/dist
var dist embed.FS

func main() {
	app := web.New(web.Frontend(dist, "web/dist"), web.Dev())
	app.Live("/", NewDashboard)
	app.Live("/users/{id}", NewProfile, ui.Island(ui.Layout(page)))
	if err := app.Run(""); err != nil {
		log.Fatal(err)
	}
}
