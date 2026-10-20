package main

import (
	"github.com/vuka-lang/web"

	_ "example.com/app/pages"
)

func main() { web.Routes.Get("/health", health) }

func health() string { return "ok" }
