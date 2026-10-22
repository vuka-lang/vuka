package routes // want package:`GET /pets/\{id\} ShowPet at routes.go:44`

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
func Find(ctx context.Context, q string, s *Store) (Pet, error)  { return Pet{}, nil }
func Search(q string, page *int, tags []string) []Pet            { return nil }
func Both(r *http.Request) (Pet, Pet)                            { return Pet{}, Pet{} }
func Res() (vuka.Result[Pet], error)                             { return vuka.Result[Pet]{}, nil }
func ListPets() []Pet                                            { return nil }

type Admin struct{}

func NewAdmin() *Admin                    { return &Admin{} }
func (a *Admin) Index() []Pet             { return nil }
func (a *Admin) Show(id int) (Pet, error) { return Pet{}, nil }

type Card struct{ Name string }

type Kennel struct {
	dogs map[string]Pet `inject:"-"` // want `E0420: the inject: tag is gone`
	s    *Store
}

func NewCard() *Card { return &Card{} }

func init() {
	vuka.RegisterParams(Find, "ctx", "q", "s")
	web.Routes.Get("/pets/{id}", ShowPet)
	web.Routes.Get("/find", Find)
	web.Routes.Get("/search", Search)         // want `E0405: GET /search \(Search\): parameter #1 string binds the path segment its position gives, and the path has none left for it` `E0405: .*#2 \*int` `E0405: .*#3 \[\]string`
	web.Routes.Get("/both", Both)             // want `E0406: GET /both \(Both\): results \(routes.Pet, routes.Pet\)`
	web.Routes.Get("/res", Res)               // want `E0406: GET /res`
	web.Routes.Get("/x", 42)                  // want `E0403: GET /x: the handler is int, not a function`
	web.Routes.Get("/pet/{id}/{tab}", Search) // want `E0405: GET /pet/\{id\}/\{tab\} \(Search\): parameter #3 \[\]string binds the path segment`
	web.Routes.Get("/pets", ListPets)         // want `E0401: GET /pets is declared twice: by lib.Pets at lib.go:8 and by ListPets at routes.go:\d+`
	web.Routes.Controller("/admin", NewAdmin)
	web.Routes.Get("/", (*Admin).Index)
	web.Routes.Get("/{id}", (*Admin).Show)
	web.Routes.Get("/show", (*Admin).Show)         // want `E0405: GET /show \(\(\*Admin\).Show\): parameter #1 int`
	web.Routes.Post("/card", NewCard, ui.Island()) // want `E0331: POST /card \(page Card\): a page backed by a component is a GET`
	web.Routes.Get("/card", NewCard, ui.Island())
}

func main() {
	app := web.New()
	app.Service(NewStore)
	app.Service(Pet{})          // want `E0418: Service\(…\): Pet is not a function`
	app.Service(ListPets)       // want `E0418: Service\(…\): a constructor returns a pointer to a struct, not \[\]Pet`
	app.Get("/admin", ListPets) // want `E0401: GET /admin is declared twice: by \(\*Admin\).Index at routes.go:\d+ and by ListPets at routes.go:\d+`
	app.Get("/one", ListPets).
		Get("/one", ListPets) // want `E0401: GET /one is declared twice: by ListPets at routes.go:66 and by ListPets at routes.go:67`
	other := web.New()
	other.Get("/one", ListPets)
	app.Handle("OPTIONS", "/one", ListPets)
}
