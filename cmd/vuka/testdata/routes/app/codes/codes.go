// Package codes is the permits' codes.
package codes

import "strings"

// Code is a permit's code, written in upper case.
type Code string

func (c Code) MarshalText() ([]byte, error) { return []byte(strings.ToUpper(string(c))), nil }

func (c *Code) UnmarshalText(b []byte) error { *c = Code(strings.ToLower(string(b))); return nil }
