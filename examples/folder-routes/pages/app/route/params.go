// Package route is /app/{route...}: every other URL under /app is the
// client app's, which routes it in the browser.
package route

import "github.com/vuka-lang/web"

type Params struct {
	Route web.Rest
}
