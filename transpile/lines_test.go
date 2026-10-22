package transpile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// The compiler's errors in generated code point at the .vuka line they come
// from: in code Vuka copied, after the lines it removed or added, and in a
// decorator's arguments, which move into generated code.
func TestLineDirectives(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the program")
	}
	src := `package main

import "fmt"

func logged(name string) func(func(int) int) func(int) int {
	return func(f func(int) int) func(int) int { return f }
}

type Tag struct{ Name string }

// double is decorated.
@logged(1)
func double(n int) int {
	var s string = n
	return n * 2
}

@Tag{Name: 2}
func plain() int { return 1 }

func shape(r Result[int]) string {
	match r {
	case Ok(0):
		return "zero"
	case Ok(n):
		return n
	case Err(e):
		return e.Error()
	}
}

func main() {
	fmt.Println(double(2), plain(), shape(Ok(1)))
	var x int = "x"
}
`
	want := map[int]string{
		12: "cannot use 1",
		14: "cannot use n",
		18: "cannot use 2",
		26: "cannot use n",
		34: `cannot use "x"`,
	}
	dir := "testdata/golden"
	res, err := transpile.Package([]transpile.File{{Name: "lines.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	mod, sum := goldenModule(t)
	os.WriteFile(filepath.Join(tmp, "go.mod"), mod, 0o644)
	os.WriteFile(filepath.Join(tmp, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(tmp, "lines_vuka.go"), res.Files[0].Src, 0o644)
	cmd := exec.Command("go", "build", "-gcflags=-e", "-o", os.DevNull, ".")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, _ := cmd.CombinedOutput()
	got := map[int]string{}
	for _, m := range regexp.MustCompile(`lines\.vuka:(\d+)(?::\d+)?: (.*)`).FindAllStringSubmatch(string(out), -1) {
		n, _ := strconv.Atoi(m[1])
		got[n] = m[2]
	}
	var lines []int
	for l := range want {
		lines = append(lines, l)
	}
	slices.Sort(lines)
	for _, l := range lines {
		if !strings.Contains(got[l], want[l]) {
			t.Errorf("line %d: want an error with %q; go build says:\n%s\ngenerated:\n%s", l, want[l], out, res.Files[0].Src)
		}
	}
}
