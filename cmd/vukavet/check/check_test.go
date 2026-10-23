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

// TestShape: in a file in the 2.0 shape the header's Go is not templates —
// its exported methods and element-named functions aren't reported — and
// the markup below it is its struct's.
func TestShape(t *testing.T) {
	var got collect
	analysistest.Run(&got, analysistest.TestData(), Templates, "shape")
	want := []string{
		"page.vuka.html:11:8: unexpected diagnostic: W0101: Title is Page's field or method and a name of the package; the template reads the field",
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

// TestHandWrittenGen: uigen in a file a person wrote is W0105; in a
// generated one, nothing.
func TestHandWrittenGen(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Templates, "uigenuse")
}

// TestHandWrittenSchema: a LiveSchema method a person wrote is W0306; a
// generated one, nothing.
func TestHandWrittenSchema(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Live, "schemause")
}

func TestPages(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Routes, "pages") }

// TestLayoutsReadingTheRequest: a folder's layout reading the request,
// through a function of its package, is W0703, and so is one reading the
// page's title in its Load; one that doesn't, or reads the title in its
// markup, nothing.
func TestLayoutsReadingTheRequest(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Routes, "layouts/crumbs", "layouts/shell", "layouts/titled", "layouts/header")
}

func TestHandWrittenHTML(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Templates, "htmluse")
}
