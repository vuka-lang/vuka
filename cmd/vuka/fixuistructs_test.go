package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixUIStructs rewrites each testdata/fixuistructs/name.in and compares
// the result, with what the fixer reports, with name.golden; a file it
// leaves alone has "unchanged" as its golden. -update rewrites them.
func TestFixUIStructs(t *testing.T) {
	cases, _ := filepath.Glob("testdata/fixuistructs/*.in")
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, path := range cases {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, _ := os.ReadFile(path)
			out, what := fixUIStructsSource(src)
			got := "unchanged\n"
			if what != nil {
				got = "// " + strings.Join(what, "\n// ") + "\n\n" + string(out)
			}
			golden := strings.TrimSuffix(path, ".in") + ".golden"
			if *updateFiles {
				os.WriteFile(golden, []byte(got), 0o644)
				return
			}
			want, _ := os.ReadFile(golden)
			if got != string(want) {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			if what != nil {
				if again, w := fixUIStructsSource(out); w != nil {
					t.Errorf("not idempotent: %v\n%s", w, again)
				}
			}
		})
	}
}
