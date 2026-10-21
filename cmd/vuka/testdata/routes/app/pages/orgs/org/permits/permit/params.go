package permit

import (
	"example.com/app/codes"
	"example.com/app/pages/orgs/org"
)

// Params binds {permit}, below {org}.
type Params struct {
	org.Params
	Permit codes.Code
}
