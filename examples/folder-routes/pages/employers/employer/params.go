// Package employer is /employers/{employer}: the folder is a parameter
// because the package declares Params.
package employer

// Params is the folder's parameter, {employer}: digits, as its type says;
// /employers/x is not found.
type Params struct {
	Employer int
}
