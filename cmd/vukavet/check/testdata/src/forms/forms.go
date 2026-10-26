package forms

import (
	"context"

	"github.com/vuka-lang/orm/v2"
	"github.com/vuka-lang/ui/v2"
)

type Address struct {
	City string
}

type Signup struct {
	ui.FormState
	Name    string `label:"Full name" placeholder:"Ada Lovelace"`
	Email   string `placeholder:"name@example.com"` // want `W0310: Signup.Email has a placeholder and no label`
	Country int
	Address Address
	note    string
}

func (f *Signup) CleanEmail(ctx context.Context) error { return nil }

func (f *Signup) CleanEmial(ctx context.Context) error { return nil } // want `W0309: Signup.CleanEmial names no field of Signup, so it never runs`

func (f *Signup) CleanAddressCity(ctx context.Context) error { return nil }

func (f *Signup) Clean(ctx context.Context) error { return nil }

func (f *Signup) CountryChoices() []ui.Option { return nil }

func (f *Signup) CityChoices() []ui.Option { return nil } // want `W0309: Signup.CityChoices names no field of Signup`

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
