package diag

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

const page = `package applicants

type ApplicantDetail struct {
	Name string
	CV   *CV ` + "`defer:\"profile\"`" + `
}

@web.Get("/applicants/{id}")
func show() {}
`

func sources(file string) ([]byte, bool) {
	if file == "modules/applicants/detail.vuka" {
		return []byte(page), true
	}
	return nil, false
}

func samples() []Diagnostic {
	return []Diagnostic{
		*New("E0324", "ApplicantDetail.CV is deferred, and the group %q has no loader", "profile").
			At(Span{File: "modules/applicants/detail.vuka", Line: 5, Col: 2, Label: "the group \"profile\" needs a loader"}).
			WithHelp("add a method\nfunc (p *ApplicantDetail) LoadProfile(ctx context.Context) error"),
		*New("E0401", "GET /applicants/{id} is declared twice").
			At(Span{File: "modules/applicants/detail.vuka", Line: 8, Col: 1, EndCol: 9}).
			WithRelated(Span{File: "admin.vuka", Line: 30, Label: "and here"}).
			WithHelp("give one of them another path or method"),
		*Warn("W0301", "ApplicantDetail embeds ui.Live: route it with @web.Live").
			At(Span{File: "modules/applicants/detail.vuka", Line: 8, Col: 1}).
			WithSuggest("@web.Live"),
		*New("E0415", "environment variable DATABASE_URL is not set"),
	}
}

func TestRenderGolden(t *testing.T) {
	var b bytes.Buffer
	Render(&b, samples(), Options{Source: sources})
	b.WriteString("---- no frame\n")
	for _, d := range samples() {
		b.WriteString(d.Error())
		b.WriteString("\n")
	}
	golden(t, "testdata/render.golden", b.Bytes())
}

func TestRenderColor(t *testing.T) {
	var b bytes.Buffer
	Render(&b, samples()[:1], Options{Source: sources, Color: true})
	if !strings.Contains(b.String(), "\x1b[1;31merror[E0324]") {
		t.Fatalf("no colour:\n%q", b.String())
	}
	var plain bytes.Buffer
	Render(&plain, samples()[:1], Options{Source: sources})
	if strings.Contains(plain.String(), "\x1b") {
		t.Fatal("escape codes without colour")
	}
}

func TestColorFor(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	if ColorFor(os.Stdout) {
		t.Fatal("NO_COLOR is ignored")
	}
	t.Setenv("NO_COLOR", "")
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ColorFor(f) {
		t.Fatal("a file is coloured")
	}
}

func TestTabsAndTokens(t *testing.T) {
	src := "\t@ui.Island(\"x\")\n"
	var b bytes.Buffer
	Render(&b, []Diagnostic{*New("E0320", "bad").At(Span{File: "a.vuka", Line: 1, Col: 2})},
		Options{Source: func(string) ([]byte, bool) { return []byte(src), true }})
	if !strings.Contains(b.String(), "  |     ^^^^^^^^^^\n") {
		t.Fatalf("caret under @ui.Island after a tab:\n%s", b.String())
	}
}

func TestFrom(t *testing.T) {
	a, b := New("E0401", "a"), New("E0402", "b")
	plain := errors.New("plain")
	ds, rest := From(errors.Join(a, List{*b}, plain))
	if len(ds) != 2 || ds[0].Code != "E0401" || ds[1].Code != "E0402" || len(rest) != 1 || rest[0] != plain {
		t.Fatalf("From = %v, %v", ds, rest)
	}
	if Of(fmt.Errorf("web: %w", a)) != "E0401" {
		t.Fatal("Of misses a wrapped diagnostic")
	}
}

func TestJSON(t *testing.T) {
	var out []map[string]any
	if err := json.Unmarshal(JSON(samples()[:1]), &out); err != nil {
		t.Fatal(err)
	}
	d := out[0]
	if d["code"] != "E0324" || d["severity"] != "error" || d["url"] != DocsURL+"E0324" || d["help"] == "" {
		t.Fatalf("JSON = %v", d)
	}
	sp := d["span"].(map[string]any)
	if sp["file"] != "modules/applicants/detail.vuka" || sp["line"] != float64(5) || sp["col"] != float64(2) {
		t.Fatalf("span = %v", sp)
	}
	var back []Diagnostic
	if err := json.Unmarshal(JSON(samples()), &back); err != nil || len(back) != 4 || back[2].Severity != Warning {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

func golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)\n%s", err, got)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

// TestNoFooter: Render without the explain lines, which Footer writes.
func TestNoFooter(t *testing.T) {
	ds := samples()
	var whole, body, foot bytes.Buffer
	Render(&whole, ds, Options{Source: sources})
	Render(&body, ds, Options{Source: sources, NoFooter: true})
	Footer(&foot, ds, Options{})
	if strings.Contains(body.String(), "vuka explain") || !strings.Contains(foot.String(), "vuka explain") || body.String()+foot.String() != whole.String() {
		t.Errorf("body:\n%s\nfooter:\n%s\nwhole:\n%s", body.String(), foot.String(), whole.String())
	}
	foot.Reset()
	if Footer(&foot, []Diagnostic{{Message: "no code"}}, Options{}); foot.Len() != 0 {
		t.Errorf("a footer for no code: %q", foot.String())
	}
}
