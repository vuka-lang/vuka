package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutsideTSProject(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	os.MkdirAll(web, 0o755)
	os.WriteFile(filepath.Join(web, "tsconfig.json"), []byte(`{"include": ["**/*.ts", "**/*.vue"]}`), 0o644)
	islands := []*islandComp{
		{framework: "vue", src: filepath.Join(root, "modules", "settings", "levels.vue"), rel: "modules/settings/levels.vue"},
		{framework: "vue", src: filepath.Join(root, "modules", "users", "card.vue"), rel: "modules/users/card.vue"},
		{framework: "vue", src: filepath.Join(web, "islands", "a.vue"), rel: "web/islands/a.vue"},
	}
	got := outsideTSProject(root, web, islands)
	if len(got) != 1 || !strings.Contains(got[0], `"../modules/**/*.vue"`) || !strings.Contains(got[0], "modules/settings/levels.vue") {
		t.Errorf("got %q", got)
	}
	os.WriteFile(filepath.Join(web, "tsconfig.json"), []byte("{\n  // the colocated islands\n  \"include\": [\"**/*.vue\", \"../modules/**/*.vue\"]\n}"), 0o644)
	if got := outsideTSProject(root, web, islands); len(got) != 0 {
		t.Errorf("an included directory is reported: %q", got)
	}
}
