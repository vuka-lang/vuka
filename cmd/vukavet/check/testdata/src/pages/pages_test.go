package pages

import "github.com/vuka-lang/web/v2"

func mount() {
	app := web.New()
	app.Get("/t", NewHome) // a test mounts the page it tests
}
