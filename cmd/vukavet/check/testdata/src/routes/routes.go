package routes // want package:`GET /pets/\{id\} ShowPet at routes.go:38`

import (
	"context"
	"net/http"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/web"

	_ "routes/lib"
)

type Store struct{}

func NewStore() *Store { return &Store{} }

type Pet struct{ Name string }

func ShowPet(ctx context.Context, id int, s *Store) (Pet, error) { return Pet{}, nil }
func Search(q string, page *int, tags []string) []Pet            { return nil }
func Both(r *http.Request) (Pet, Pet)                            { return Pet{}, Pet{} }
func Res() (vuka.Result[Pet], error)                             { return vuka.Result[Pet]{}, nil }
func ListPets() []Pet                                            { return nil }

type Admin struct{}

func NewAdmin() *Admin                    { return &Admin{} }
func (a *Admin) Index() []Pet             { return nil }
func (a *Admin) Show(id int) (Pet, error) { return Pet{}, nil }

type Card struct{ Name string }

func NewCard() *Card { return &Card{} }

func init() {
	web.Routes.Service(NewStore)
	web.Routes.Get("/pets/{id}", ShowPet, web.Params("ctx", "id", "store"))
	web.Routes.Get("/search", Search)                                      // want `E0405: GET /search \(Search\): parameter #1 string binds by name` `E0405: .*#2 \*int` `E0405: .*#3 \[\]string`
	web.Routes.Get("/both", Both)                                          // want `E0406: GET /both \(Both\): results \(routes.Pet, routes.Pet\)`
	web.Routes.Get("/res", Res)                                            // want `E0406: GET /res`
	web.Routes.Get("/x", 42)                                               // want `E0403: GET /x: the handler is int, not a function`
	web.Routes.Get("/pet", ShowPet, web.Params("ctx", "id", "s", "extra")) // want `E0419: web.Params gives 4 names, and ShowPet takes 3 parameters`
	web.Routes.Get("/pet2", ShowPet, web.Params("a", "a", "s"))            // want `E0419: web.Params names "a" twice`
	web.Routes.Get("/pets", ListPets)                                      // want `E0401: GET /pets is declared twice: by lib.Pets at lib.go:8 and by ListPets at routes.go:\d+`
	web.Routes.Service(Pet{})                                              // want `E0418: Service\(…\): Pet is not a function`
	web.Routes.Service(ListPets)                                           // want `E0418: Service\(…\): a constructor returns a pointer to a struct, not \[\]Pet`
	web.Routes.Controller("/admin", NewAdmin)
	web.Routes.Get("/", (*Admin).Index)
	web.Routes.Get("/{id}", (*Admin).Show)         // want `E0405: GET /\{id\} \(\(\*Admin\).Show\): parameter #1 int`
	web.Routes.Post("/card", NewCard, ui.Island()) // want `E0331: POST /card \(page Card\): a page backed by a component is a GET`
	web.Routes.Get("/card", NewCard, ui.Island())
}

func main() {
	app := web.New()
	app.Get("/admin", ListPets) // want `E0401: GET /admin is declared twice: by \(\*Admin\).Index at routes.go:\d+ and by ListPets at routes.go:\d+`
	app.Get("/one", ListPets).
		Get("/one", ListPets) // want `E0401: GET /one is declared twice: by ListPets at routes.go:58 and by ListPets at routes.go:59`
	other := web.New()
	other.Get("/one", ListPets)
	app.Handle("OPTIONS", "/one", ListPets)
}
