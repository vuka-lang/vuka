package docs

import (
	"os"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// Keep the guides from accidentally reintroducing a second UI authoring form.
// Diagnostic and RFC source files are intentionally outside this contract.
func TestOneWayGuides(t *testing.T) {
	forbidden := []string{"ui.HTML(", "ui.UseState", "ui.UsePageState", "LiveSchema", "@web.Get", "@web.Live", "@web.Service", "web.Routes.", "web.Params", "inject:", "vuka fix"}
	shapes := 0
	for _, page := range docPages(t) {
		if !strings.HasPrefix(page, "ui/") && !strings.HasPrefix(page, "web/") || generatedUIPage(page) {
			continue
		}
		src, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		// The Go a sample becomes may hold what a guide's own code mustn't.
		authored := string(src)
		pairs, _ := generatedPairs(page, authored)
		for _, p := range pairs {
			authored = strings.Replace(authored, p.gen.code, "", 1)
		}
		for _, old := range forbidden {
			if strings.Contains(authored, old) {
				t.Errorf("%s: retired authoring form %q", page, old)
			}
		}
		samples, _ := parsePage(page, string(src))
		for _, s := range samples {
			if s.lang != "html" {
				continue
			}
			code := strings.TrimSpace(s.code)
			if strings.HasPrefix(code, "package ") {
				t.Errorf("%s:%d: a shape in the Go-header form: write its Go in <script lang=\"go\"> (vuka fmt -rewrite)", page, s.line)
				continue
			}
			if !strings.HasPrefix(code, `<script lang="go">`) || !strings.Contains(code, "\npackage ") {
				continue
			}
			shape, err := transpile.ParseShape([]byte(s.code))
			if err != nil {
				t.Errorf("%s:%d: %v", page, s.line, err)
				continue
			}
			if shape == nil {
				t.Errorf("%s:%d: complete shape example needs a <script lang=\"go\"> with one struct", page, s.line)
				continue
			}
			shapes++
		}
	}
	if shapes < 10 {
		t.Errorf("only %d complete shape examples found", shapes)
	}
}
