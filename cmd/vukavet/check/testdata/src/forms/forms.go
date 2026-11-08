package forms

import (
	"context"

	"github.com/vuka-lang/orm/v2"
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/widget"
)

type Address struct {
	City string
}

type Signup struct {
	ui.FormState
	Name    string `label:"Full name" placeholder:"Ada Lovelace"`
	Email   string `placeholder:"name@example.com"` // want `W0310: Signup.Email has a placeholder and no label`
	Country int
	City    int `depends:"Country"`
	Town    int `depends:"Contry"` // want `W0313: Signup.Town depends on Contry, which isn't a field of Signup`
	Address Address
	note    string
}

func (f *Signup) CleanEmail(ctx context.Context) error { return nil }

func (f *Signup) CleanEmial(ctx context.Context) error { return nil } // want `W0309: Signup.CleanEmial names no field of Signup, so it never runs`

func (f *Signup) CleanAddressCity(ctx context.Context) error { return nil }

func (f *Signup) Clean(ctx context.Context) error { return nil }

func (f *Signup) CountryChoices() []ui.Option { return nil }

func (f *Signup) CityChoices() []ui.Option { return nil }

func (f *Signup) TwonChoices() []ui.Option { return nil } // want `W0309: Signup.TwonChoices names no field of Signup`

type User struct {
	orm.Model[User]
	Name  string
	Admin bool
}

type Profile struct {
	ui.FormState
	User // want `W0311: Profile embeds User, a database model: a submit may set every column of it`
}

// NotAForm has Clean methods of its own: no form, nothing to check.
type NotAForm struct{ Name string }

func (n *NotAForm) CleanNothing() error { return nil }

// The form components by their former names in ui, and by widget's.
var (
	_ = ui.Field{}   // want `W0320: ui.Field is widget.Field now: a form's components come from package widget`
	_ = []ui.Error{} // want `W0320: ui.Error is widget.Error now`
	_ = widget.Error{}
	_ = widget.Summary{}
)
