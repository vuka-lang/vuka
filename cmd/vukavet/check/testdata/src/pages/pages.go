package pages // want package:`GET /api/list ListPets`

import (
	"context"
	"io"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

type Home struct{}

func (Home) Render(ctx context.Context, w io.Writer) error { return nil }

func NewHome() *Home { return &Home{} }

type Dash struct {
	ui.Live
}

func NewDash() *Dash { return &Dash{} }

type Pet struct{ Name string }

func ListPets() []Pet                        { return nil }
func Banner() ui.Node                        { return nil }
func Save(ctx context.Context) error         { return nil }
func Show(ctx context.Context) (Home, error) { return Home{}, nil }

func main() {
	app := web.New()
	app.Get("/", NewHome)      // want `E0720: app.Get\("/", NewHome\) registers a page \(its struct Home renders\) by hand: a page is a file under pages/`
	app.Live("/dash", NewDash) // want `E0720: app.Live\("/dash", NewDash\) registers a page \(a live page\) by hand`
	app.Get("/banner", Banner) // a handler answering with a ui.Node
	app.Get("/show", Show)
	app.Get("/card", NewDash, ui.Island())   // want `E0720: .*\(an island page\)`
	app.Page("/island", &Home{})             // want `E0720: app.Page\("/island", &Home\{\}\) registers a page \(an island page\)`
	app.Get("/api/pets", ListPets)           // an API
	app.Post("/api/save", Save)              // an API
	web.Routes.Get("/api/list", ListPets)    // want
	web.Routes.Get("/about", NewHome)        // want `E0720: web.Routes.Get\("/about", NewHome\)`
	app.Get("/x", ListPets, web.PageRoute()) // want `W0702: web.PageRoute is what pages/route.go registers with`
}
