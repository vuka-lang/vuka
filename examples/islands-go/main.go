// Command islands-go is a vuka app in Go with islands: a Vue component
// placed in a live page (Chart) and a page backed by its component
// (Profile), its pages the .vuka.html files under pages/. go generate
// compiles them (vuka gen -inplace) and builds the islands into web/dist,
// which the binary embeds; under VUKA_DEV=1 go run . web.Dev keeps them
// built.
package main

import (
	"embed"
	"log"

	"example.com/islands-go/pages"
	"github.com/vuka-lang/web"
)

//go:generate go tool vuka gen -inplace
//go:generate go tool vuka build-islands

//go:embed all:web/dist
var dist embed.FS

func main() {
	app := web.New(pages.Routes(), web.Frontend(dist, "web/dist"), web.Dev())
	if err := app.Run(""); err != nil {
		log.Fatal(err)
	}
}
