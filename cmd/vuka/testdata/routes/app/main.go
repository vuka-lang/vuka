package main

import (
	"github.com/vuka-lang/web/v2"

	"example.com/app/pages"
)

func main() {
	web.New(pages.Routes()).Get("/health", health)
}

func health() string { return "ok" }
