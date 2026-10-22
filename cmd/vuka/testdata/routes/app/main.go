package main

import (
	"github.com/vuka-lang/web"

	"example.com/app/pages"
)

func main() {
	web.Routes.Get("/health", health)
	web.New(pages.Routes())
}

func health() string { return "ok" }
