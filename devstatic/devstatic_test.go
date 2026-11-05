package devstatic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoad: the texts come from the file FileEnv names, only when its keys
// are the binary's.
func TestLoad(t *testing.T) {
	file := filepath.Join(t.TempDir(), "texts.json")
	data, _ := json.Marshal(File{Keys: "k1", Texts: []string{"a.0", "<p>one</p>", "a.1", "<b>two</b>"}})
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(FileEnv, file)
	Load("k1")
	if got := S("a.0") + S("a.1"); got != "<p>one</p><b>two</b>" {
		t.Fatalf("texts %q", got)
	}
	for _, c := range []struct{ name, env, keys, want string }{
		{"another build's", file, "k2", "another build"},
		{"no file named", "", "k1", FileEnv + " isn't set"},
		{"a missing file", file + ".gone", "k1", "no such file"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(FileEnv, c.env)
			defer func() {
				if r, _ := recover().(string); !strings.Contains(r, c.want) {
					t.Fatalf("panic %q, want %q", r, c.want)
				}
			}()
			Load(c.keys)
		})
	}
}
