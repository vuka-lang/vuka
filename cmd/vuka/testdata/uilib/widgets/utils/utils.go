// Package utils is the library's helpers: class joins and the ids it mints.
package utils

import (
	"crypto/rand"
	"strings"

	"github.com/a-h/templ"
)

// RandomID is an id minted at random, as such libraries do.
func RandomID() string { return "id-" + rand.Text() }

// CN joins classes.
func CN(classes ...string) string {
	return strings.Join(strings.Fields(strings.Join(classes, " ")), " ")
}

// Merge is attributes with more added.
func Merge(a templ.Attributes, more templ.Attributes) templ.Attributes {
	out := templ.Attributes{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}
