package pages

import "github.com/vuka-lang/web"

func mount() {
	app := web.New()
	app.Get("/t", NewHome) // a test mounts the page it tests
}
