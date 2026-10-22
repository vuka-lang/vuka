package docs

import (
	"os"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

// Keep the guides from accidentally reintroducing a second UI authoring form.
// Diagnostic and RFC source files are intentionally outside this contract.
func TestOneWayGuides(t *testing.T) {
	forbidden := []string{"ui.HTML(", "ui.UseState", "ui.UsePageState", "LiveSchema", "@web.Get", "@web.Live", "@web.Service", "web.Routes.", "web.Params", "inject:", "vuka fix"}
	shapes := 0
	for _, page := range docPages(t) {
		if !strings.HasPrefix(page, "ui/") && !strings.HasPrefix(page, "web/") {
			continue
		}
		src, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, old := range forbidden {
			if strings.Contains(string(src), old) {
				t.Errorf("%s: retired authoring form %q", page, old)
			}
		}
		samples, _ := parsePage(page, string(src))
		for _, s := range samples {
			if s.lang != "html" || !strings.HasPrefix(strings.TrimSpace(s.code), "package ") {
				continue
			}
			shape, err := transpile.ParseShape([]byte(s.code))
			if err != nil {
				t.Errorf("%s:%d: %v", page, s.line, err)
				continue
			}
			if shape == nil {
				t.Errorf("%s:%d: complete shape example needs a Go header with one struct", page, s.line)
				continue
			}
			shapes++
		}
	}
	if shapes < 10 {
		t.Errorf("only %d complete shape examples found", shapes)
	}
}
