package check

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestRoutes(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Routes, "routes") }
func TestLive(t *testing.T)   { analysistest.Run(t, analysistest.TestData(), Live, "live") }
func TestTemplates(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Templates, "templates")
}
func TestSchema(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Schema, "schema") }

type collect []string

func (c *collect) Errorf(format string, args ...any) { *c = append(*c, fmt.Sprintf(format, args...)) }

func TestBareNames(t *testing.T) {
	var got collect
	analysistest.Run(&got, analysistest.TestData(), Templates, "bare")
	want := []string{
		"card.vuka.html:4:14: unexpected diagnostic: W0101: Title is Card's field or method and a name of the package; the template reads the field",
		"card.vuka.html:4:30: unexpected diagnostic: W0101: Label is Card's field or method and a name of the package; the template reads the field",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("got %q, want %q", got[i], w)
		}
	}
}

func TestListKeys(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Templates, "keys") }

func TestTemplateDocs(t *testing.T) {
	var got collect
	analysistest.Run(&got, analysistest.TestData(), Templates, "docs")
	want := []string{
		"docs.vuka.html:7:6: unexpected diagnostic: W0104: the exported template List has no doc comment",
		"docs.vuka.html:16:15: unexpected diagnostic: W0104: the exported template Badge has no doc comment",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("got %q, want %q", got[i], w)
		}
	}
}
