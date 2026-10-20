// Package file is /files/{file...}.
package file

import "github.com/vuka-lang/web"

// Params is the rest of the path, slashes included: handbook/leave.txt.
type Params struct {
	File web.Rest
}
