package docs

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	limitsBegin = "<!-- known-limits:begin -->"
	limitsEnd   = "<!-- known-limits:end -->"
	siteURL     = "https://vuka-lang.github.io"
)

var siteLink = regexp.MustCompile(`\]\((/[^)]*)\)`)

// knownLimitsBlock is the bullets of the docs' Known Limits page, with
// site-relative links made absolute, as the README shows them.
func knownLimitsBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("reference/known-limits.md")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "- ") || (len(out) > 0 && strings.HasPrefix(l, "  ")) {
			out = append(out, siteLink.ReplaceAllString(l, "]("+siteURL+"$1)"))
		}
	}
	if len(out) == 0 {
		t.Fatal("reference/known-limits.md has no bullets")
	}
	return strings.Join(out, "\n")
}

// TestReadmeMatchesDocs keeps the README in step with the docs: its known
// limits are the docs page's, every feature page is linked from it, and its
// Roadmap names nothing a feature page documents. -update rewrites the
// known-limits block.
func TestReadmeMatchesDocs(t *testing.T) {
	b, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	begin, end := strings.Index(readme, limitsBegin), strings.Index(readme, limitsEnd)
	if begin < 0 || end < begin {
		t.Fatalf("README.md needs %s and %s around the known limits", limitsBegin, limitsEnd)
	}
	want := "\n" + knownLimitsBlock(t) + "\n"
	if got := readme[begin+len(limitsBegin) : end]; got != want {
		if *update {
			readme = readme[:begin+len(limitsBegin)] + want + readme[end:]
			if err := os.WriteFile("../README.md", []byte(readme), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Errorf("README's known limits differ from docs/reference/known-limits.md; run: go test ./docs -run TestReadmeMatchesDocs -update")
		}
	}

	pages, err := filepath.Glob("features/*.md")
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{} // page name -> its title
	title := regexp.MustCompile(`(?m)^# (.+)$`)
	for _, p := range pages {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if !strings.Contains(readme, "("+siteURL+"/features/"+name+")") {
			t.Errorf("README.md doesn't link the %s page: %s/features/%s", name, siteURL, name)
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if m := title.FindSubmatch(src); m != nil {
			titles[name] = string(m[1])
		}
	}

	a := strings.Index(readme, "## Roadmap")
	z := strings.Index(readme, "## Libraries")
	if a < 0 || z < a {
		t.Fatal("README.md has no Roadmap section before Libraries")
	}
	for _, line := range strings.Split(readme[a:z], "\n") {
		for name, ttl := range titles {
			// A page title, as written, as a whole word: "Enums" is named, an
			// unimplemented "arity overloading" is not.
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(ttl) + `\b`).MatchString(line) {
				t.Errorf("Roadmap line %q names %q, which has a docs page (features/%s)", strings.TrimSpace(line), ttl, name)
			}
		}
	}
}

var semverTag = regexp.MustCompile(`^v2\.(\d+)\.(\d+)$`)

// TestLatestTagHasReleaseNotes requires docs/releases.md to have a heading
// for the highest v2.x.y tag of the checkout, so a release can't go out
// without its notes.
func TestLatestTagHasReleaseNotes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git command")
	}
	out, err := exec.Command("git", "tag", "--list", "v2.*").Output()
	if err != nil {
		t.Skip("not a git checkout")
	}
	var latest string
	var lm, lp int
	for _, tag := range strings.Fields(string(out)) {
		m := semverTag.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		mi, _ := strconv.Atoi(m[1])
		pa, _ := strconv.Atoi(m[2])
		if latest == "" || mi > lm || (mi == lm && pa > lp) {
			latest, lm, lp = tag, mi, pa
		}
	}
	if latest == "" {
		t.Skip("no v2 tags")
	}
	b, err := os.ReadFile("releases.md")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(latest) + `(\s|$)`).Match(b) {
		t.Errorf("docs/releases.md has no \"## %s\" heading for the latest tag", latest)
	}
}
