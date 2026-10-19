package lib

import "github.com/vuka-lang/web"

func Pets() []string { return nil }

func init() {
	web.Routes.Get("/pets", Pets)
}
