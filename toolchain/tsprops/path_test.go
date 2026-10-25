package tsprops

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveIslandPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pages", "admin", "users")
	for _, tc := range []struct{ path, root, want, problem string }{
		{"@/web/Form.vue", root, filepath.Join(root, "web", "Form.vue"), ""},
		{"@/web/../Form.vue", root, filepath.Join(root, "Form.vue"), ""},
		{"./Form.vue", "", filepath.Join(dir, "Form.vue"), ""},
		{"../Form.vue", root, filepath.Join(filepath.Dir(dir), "Form.vue"), ""},
		{"@/../Form.vue", root, "", "escapes"},
		{"@/web/../../Form.vue", root, "", "escapes"},
		{"@//Form.vue", root, "", "escapes"},
		{"@/Form.vue", "", "", "unknown"},
	} {
		t.Run(tc.path+tc.problem, func(t *testing.T) {
			got, err := ResolveIslandPath(tc.root, dir, tc.path)
			if tc.problem != "" {
				if err == nil || !strings.Contains(err.Error(), tc.problem) {
					t.Fatalf("got %q, %v", got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if runtime.GOOS == "windows" {
		for _, path := range []string{"@/islands/CON.vue", "@/aux.tsx", "@/islands/a:b.vue"} {
			if _, err := ResolveIslandPath(root, dir, path); err == nil || !strings.Contains(err.Error(), "invalid component path") {
				t.Errorf("%s: %v", path, err)
			}
		}
	}
	for _, p := range []string{"@/Form.vue", "@/Form.tsx", "@/Form.svelte", "@/Form.ts"} {
		if !IsComponent(p) {
			t.Errorf("%s is not a component", p)
		}
	}
	if IsComponent("@/example.com/pkg") {
		t.Fatal("classified a package as a component")
	}
}
