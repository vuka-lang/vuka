//go:build tools

// Package golden keeps what the golden cases import in go.mod.
package golden

import (
	_ "github.com/a-h/templ"
	_ "github.com/vuka-lang/ui"
	_ "github.com/vuka-lang/ui/live"
	_ "github.com/vuka-lang/vuka"
)
