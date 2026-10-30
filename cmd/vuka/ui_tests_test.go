package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The registry's components rendered: testdata/uitests/*_test.go are
// tests of package design, run in the module ejectRegistry builds from
// the registry against the ui checkout.
func TestUIComponentTests(t *testing.T) {
	tests, _ := filepath.Glob("testdata/uitests/*_test.go")
	if len(tests) == 0 {
		t.Fatal("no testdata/uitests/*_test.go")
	}
	reg, err := readRegistry(regSource{})
	if err != nil {
		t.Fatal(err)
	}
	var srcs []string
	for _, f := range tests {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, string(b))
	}
	dir, _ := ejectRegistry(t, reg)
	for i, f := range tests {
		writeFile(t, filepath.Join(dir, "components/ui", filepath.Base(f)), srcs[i])
	}
	cmd := exec.Command("go", "test", "-count=1", "./components/ui")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
}
