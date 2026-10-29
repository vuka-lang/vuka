package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEjectEnumCauses ejects an error enum with a cause: the generated
// Unwrap is plain Go that go build and go vet take.
func TestEjectEnumCauses(t *testing.T) { ejectGolden(t, "enum_causes") }

// TestEjectMatchErrors ejects matches on (T, error) calls and errors:
// errors.Is and errors.As in plain Go.
func TestEjectMatchErrors(t *testing.T) { ejectGolden(t, "match_errors") }

// ejectGolden builds the transpiler's golden case name with vuka, ejects it
// with -rm, and checks go build and go vet take the Go and the program
// prints what it did.
func ejectGolden(t *testing.T, name string) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	src, err := os.ReadFile(filepath.Join("../../transpile/testdata/golden", name+".vuka"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	writeFile(t, filepath.Join(root, "main.vuka"), string(src))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	bin := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", bin, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	want, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, want)
	}
	var out bytes.Buffer
	if err := eject([]string{"-rm", "./..."}, &out); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"build", "-o", bin, "."}, {"vet", "./..."}} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}
	got, err := exec.Command(bin).CombinedOutput()
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("built with go build: %v\n%s\nwant:\n%s", err, got, want)
	}
}
