package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateFixUI = flag.Bool("update", false, "rewrite testdata/fixui/*.golden")

// TestFixUI rewrites each testdata/fixui/name.in and compares the result,
// with what the fixer reports, with name.golden; a file it leaves alone has
// "unchanged" as its golden.
func TestFixUI(t *testing.T) {
	cases, _ := filepath.Glob("testdata/fixui/*.in")
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, path := range cases {
		name := strings.TrimSuffix(filepath.Base(path), ".in")
		t.Run(name, func(t *testing.T) {
			src, _ := os.ReadFile(path)
			out, what := fixUISource(src, strings.HasSuffix(name, ".vuka"))
			got := "unchanged\n"
			if what != nil {
				got = "// " + strings.Join(what, "\n// ") + "\n\n" + string(out)
			}
			golden := strings.TrimSuffix(path, ".in") + ".golden"
			if *updateFixUI {
				os.WriteFile(golden, []byte(got), 0o644)
				return
			}
			want, _ := os.ReadFile(golden)
			if got != string(want) {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			if what != nil {
				if again, w := fixUISource(out, strings.HasSuffix(name, ".vuka")); w != nil {
					t.Errorf("not idempotent: %v\n%s", w, again)
				}
			}
		})
	}
}
