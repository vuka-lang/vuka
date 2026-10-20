// Command crud is a blog's API over the orm, in plain Go: authors and
// their posts, written in transactions and counted with update arithmetic.
package main

import (
	"log"

	"github.com/vuka-lang/web"
)

//go:generate go run github.com/vuka-lang/orm/cmd/orm generate
//go:generate go tool vuka gen -inplace

func init() {
	web.Routes.Service(NewBlog)
	web.Routes.Get("/authors", ListAuthors)
	web.Routes.Get("/posts", ListPosts, web.Params("ctx", "author", "live"))
	web.Routes.Get("/posts/{id}", ShowPost, web.Params("ctx", "id"))
	web.Routes.Get("/find", FindPost, web.Params("ctx", "q"))
	web.Routes.Post("/posts", CreatePost)
	web.Routes.Post("/posts/{id}/view", ViewPost, web.Params("ctx", "id"))
	web.Routes.Post("/posts/{id}/publish", PublishPost, web.Params("ctx", "id"))
	web.Routes.Get("/audit", Audit)
}

func main() {
	app := web.New()
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
