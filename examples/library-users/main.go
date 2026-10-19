// Command library-users is a vuka app written in plain Go: built and run
// with go build and go run, no vuka command. decorated/ is the same app
// written in Vuka; vuka eject turns it into this code's registrations.
package main

import (
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

func main() {
	app := web.New()
	app.Service(NewDirectory)
	app.Get("/api/users", ListUsers)
	app.Get("/api/users/{id}", ShowUser, web.Params("id", "dir"))
	app.Live("/", NewDashboard)
	app.Live("/users/{id}", NewProfile, ui.Island(ui.Layout(page)))
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
