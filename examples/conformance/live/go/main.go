// Command live is a notes board in plain Go: a page backed by its script
// island, with deferred fields, a bound and validated query in the URL and
// broadcasts; and a note's page, patched and navigated over the socket.
package main

import (
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

//go:generate go tool vuka gen -inplace

func init() {
	web.Routes.Service(NewNotes)
	web.Routes.Live("/board", NewBoard, ui.Island(ui.Layout(page)))
	web.Routes.Live("/notes/{id}", NewNotePage)
}

func main() {
	app := web.New()
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
