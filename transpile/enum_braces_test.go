package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// FuzzPackage found that matchClose also accepts a closing parenthesis.
// Reject it as an enum delimiter before lowering can erase the syntax error.
func TestEnumMismatchedClosingBrace(t *testing.T) {
	for _, src := range []string{
		"package A\nenum s{A)\nenum A{A00{},A)\nfunc A(){match 0{case A:0}}\n",
		"package A\nenum S{A]\n",
		"package A\nenum S{A{X int) }\n",
	} {
		_, err := transpile.Package([]transpile.File{{Name: "f.vuka", Src: []byte(src)}}, transpile.Options{})
		if err == nil || !strings.Contains(err.Error(), "unclosed") {
			t.Fatalf("malformed enum: %v", err)
		}
	}
}
