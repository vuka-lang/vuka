package permit

import (
	"strings"

	"example.com/app/pages/orgs/org"
)

// Params binds {permit}, below {org}.
type Params struct {
	org.Params
	Permit Code
}

// Code is a permit's code, written in upper case.
type Code string

func (c Code) MarshalText() ([]byte, error) { return []byte(strings.ToUpper(string(c))), nil }

func (c *Code) UnmarshalText(b []byte) error { *c = Code(strings.ToLower(string(b))); return nil }
