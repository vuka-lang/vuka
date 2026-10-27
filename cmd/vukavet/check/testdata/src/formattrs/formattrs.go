package formattrs

import "github.com/vuka-lang/ui/v2"

type Signup struct {
	ui.FormState
	Name     string `validate:"required,max=80"` // want `W0312: Signup.Name's control has no required` `W0312: Signup.Name's control has no maxlength`
	Postcode string `input:"numeric" validate:"len=5"`
	Age      int    `validate:"min=18"`
}
