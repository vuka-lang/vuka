// Package employer is /api/employers/{employer}.
package employer

// Params is {employer}: digits, so /api/employers/x is not found.
type Params struct {
	Employer int
}
