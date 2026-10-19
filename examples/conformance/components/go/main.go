// Command components is a gallery in plain Go: struct components with
// slots, a script and a scoped stylesheet, a layout taking the render's
// context, partials, and stateful components keyed in a loop.
package main

import (
	"log"

	"github.com/vuka-lang/web"
)

//go:generate go tool vuka gen -inplace

func init() {
	web.Routes.Live("/", NewGallery)
}

func main() {
	app := web.New()
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
