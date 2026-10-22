package lib

import "github.com/vuka-lang/web/v2"

func Pets() []string { return nil }

func init() {
	web.Routes.Get("/pets", Pets)
}
