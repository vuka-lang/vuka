package load

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCompile(t *testing.T) {
	c, err := ParseCompile("vuka.toml", []byte("[fmt]\nwidth = 120\n\n[compile]\nimplicit_ctx = false # authority in the source\n"))
	if err != nil || !c.NoImplicitCtx {
		t.Fatalf("%+v %v", c, err)
	}
	if c, err := ParseCompile("vuka.toml", []byte("[compile]\nimplicit_ctx = true\n")); err != nil || c.NoImplicitCtx {
		t.Fatalf("implicit_ctx = true: %+v %v", c, err)
	}
	for _, c := range []struct{ src, want string }{
		{"[compile]\nimplicit = false\n", "vuka.toml:2: unknown key implicit in [compile]: it has implicit_ctx"},
		{"[compile]\nimplicit_ctx = off\n", "vuka.toml:2: implicit_ctx is true or false"},
		{"[build]\n", "vuka.toml:1: unknown table [build]: vuka.toml has [fmt] and [compile]"},
		{"implicit_ctx = false\n", "vuka.toml:1: implicit_ctx is outside a table: write it under [compile]"},
	} {
		if _, err := ParseCompile("vuka.toml", []byte(c.src)); err == nil || err.Error() != c.want {
			t.Errorf("%q: %v", c.src, err)
		}
	}
}

// TestLoadCompile: a package's settings are those of the vuka.toml beside
// its module's go.mod; none is the default, implicit ctx on.
func TestLoadCompile(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644)
	dir := filepath.Join(root, "views")
	os.MkdirAll(dir, 0o755)
	if c, err := LoadCompile(dir); err != nil || c.NoImplicitCtx {
		t.Fatalf("no vuka.toml: %+v %v", c, err)
	}
	os.WriteFile(filepath.Join(root, ConfigFile), []byte("[compile]\nimplicit_ctx = false\n"), 0o644)
	if c, err := LoadCompile(dir); err != nil || !c.NoImplicitCtx {
		t.Fatalf("implicit_ctx = false: %+v %v", c, err)
	}
}
