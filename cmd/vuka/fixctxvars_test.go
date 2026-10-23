package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixCtxVars: vuka fix ctx-vars rewrites each testdata/fixctxvars
// name.in, in a package of an app listing web.RequestContext and
// auth.Context, to name.golden, dropping the imports left unused; the app
// still vets, and a second run has nothing to do.
func TestFixCtxVars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	cases, _ := filepath.Glob("testdata/fixctxvars/*.in")
	for i, c := range cases {
		cases[i], _ = filepath.Abs(c)
	}
	main, _ := os.ReadFile("testdata/fixctxvars/main.go")
	files := map[string]string{"main.go": string(main)}
	for _, c := range cases {
		src, _ := os.ReadFile(c)
		files["orders/"+strings.TrimSuffix(filepath.Base(c), ".in")] = string(src)
	}
	dir := refactorModule(t, files)
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out bytes.Buffer
	if err := fix([]string{"ctx-vars"}, &out); err != nil {
		t.Fatalf("vuka fix: %v\n%s", err, out.String())
	}
	for _, c := range cases {
		name := strings.TrimSuffix(filepath.Base(c), ".in")
		got := readFile(t, filepath.Join(dir, "orders", name))
		golden := strings.TrimSuffix(c, ".in") + ".golden"
		if *updateFixUI {
			os.WriteFile(golden, []byte(got), 0o644)
			continue
		}
		if want, _ := os.ReadFile(golden); got != string(want) {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}
	}
	out.Reset()
	if err := fix([]string{"ctx-vars"}, &out); err != nil || !strings.Contains(out.String(), "nothing to fix") {
		t.Errorf("again: %v\n%s", err, out.String())
	}
	vets(t, dir)
}
