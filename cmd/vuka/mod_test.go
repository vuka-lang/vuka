package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestModTidy: go mod tidy alone drops what only .vuka files use; vuka mod
// tidy keeps it, and leaves no files behind. Offline: uuid must be in the
// module cache already.
func TestModTidy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	cache, _ := exec.Command("go", "env", "GOMODCACHE").Output()
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(string(cache)), "github.com/google/uuid@v1.6.0")); err != nil {
		t.Skip("github.com/google/uuid v1.6.0 isn't in the module cache")
	}
	dir := module(t, map[string]string{
		"main.vuka": `package main

import (
	"fmt"

	"github.com/google/uuid"
)

func id() Result[string] { return Ok(uuid.NewString()) }

func main() { fmt.Println(len(id().Unwrap())) }
`,
		"helper.go": "package main\n\nfunc helper() {}\n",
	})
	mod := filepath.Join(dir, "go.mod")
	src, _ := os.ReadFile(mod)
	os.WriteFile(mod, append(src, []byte("\nrequire github.com/google/uuid v1.6.0\n")...), 0o644)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	chdir(t, dir)

	if err := mod_(t, "tidy"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(mod)
	for _, want := range []string{"github.com/google/uuid v1.6.0", "github.com/vuka-lang/vuka"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("vuka mod tidy dropped %s:\n%s", want, got)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "zz_vuka_mod") {
			t.Fatalf("left %s behind", e.Name())
		}
	}
}

func mod_(t *testing.T, args ...string) error {
	t.Helper()
	return mod(args)
}
