// Command library-users is a vuka app written in plain Go: built and run
// with go build and go run, no vuka command. decorated/ is the same app
// written in Vuka; vuka eject turns it into this code.
package main

import (
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

func init() {
	web.Routes.Service(NewDirectory)
	web.Routes.Get("/api/users", ListUsers)
	web.Routes.Get("/api/users/{id}", ShowUser, web.Params("ctx", "id", "dir"))
	web.Routes.Live("/", NewDashboard)
	web.Routes.Live("/users/{id}", NewProfile, ui.Island(ui.Layout(page)))
}

func main() {
	app := web.New()
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
