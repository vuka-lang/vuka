//go:build tools

// Package golden keeps what the golden cases import in go.mod.
package golden

import (
	_ "github.com/a-h/templ"
	_ "github.com/vuka-lang/ui/v2"
	_ "github.com/vuka-lang/ui/v2/live"
	_ "github.com/vuka-lang/vuka/v2"
)
