package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// fixExplicitLive embeds ui.Live in each .vuka.html struct that is live
// without it. The compiler already added it to these, so they behave the same.
func fixExplicitLive(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	err := walkModule(m.root, func(path string) error {
		if !strings.HasSuffix(path, ".vuka.html") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, why := transpile.EmbedLive(src)
		if out == nil {
			return nil
		}
		rel, _ := filepath.Rel(m.root, path)
		changes = append(changes, fixChange{
			what:  "in " + rel + ", embed ui.Live: " + why,
			apply: func() error { return os.WriteFile(path, out, 0o644) },
		})
		return nil
	})
	return changes, err
}
