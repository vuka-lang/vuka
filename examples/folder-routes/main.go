// Command folder-routes is a vuka app in plain Go whose routes are the
// folders under pages/: vuka gen writes their web.Routes calls and typed
// URLs, and vuka routes lists them. Run it with vuka run, or vuka gen
// -inplace then go run.
package main

import (
	"log"

	_ "example.com/folder-routes/pages"
	"example.com/folder-routes/store"
	"github.com/vuka-lang/web"
)

func main() {
	app := web.New(web.Provide(store.New))
	app.Get("/", func() web.Redirect { return "/employers" })
	if err := app.Run(""); err != nil {
		log.Fatal(err)
	}
}
