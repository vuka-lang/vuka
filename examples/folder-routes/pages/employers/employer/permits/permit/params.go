// Package permit is /employers/{employer}/permits/{permit}.
package permit

import "example.com/folder-routes/pages/employers/employer"

// Params is {permit}, below the employer's {employer}.
type Params struct {
	employer.Params
	Permit int
}
