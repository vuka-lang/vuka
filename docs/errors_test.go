package docs

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite errors/index.md")

var codeRe = regexp.MustCompile(`^[EW]\d{4}$`)

func TestCatalogue(t *testing.T) {
	names, _ := filepath.Glob("errors/[EW]*.md")
	if len(names) == 0 {
		t.Fatal("no entries")
	}
	entries := Errors()
	if len(entries) != len(names) {
		t.Fatalf("%d entries, %d files", len(entries), len(names))
	}
	titles := map[string]string{}
	for i, e := range entries {
		file := strings.TrimSuffix(filepath.Base(names[i]), ".md")
		switch {
		case e.Code != file:
			t.Errorf("%s: its code is %q", names[i], e.Code)
		case !codeRe.MatchString(e.Code):
			t.Errorf("%s: %q isn't a code (E or W, four digits)", names[i], e.Code)
		case e.Title == "" || e.Help == "":
			t.Errorf("%s: needs a title and a help line", e.Code)
		case e.Area != Areas[e.Code[1:3]]:
			t.Errorf("%s: area %q, want %q", e.Code, e.Area, Areas[e.Code[1:3]])
		case !strings.HasPrefix(e.Body, "# "+e.Code+": "+e.Title+"\n"):
			t.Errorf("%s: the page starts with # %s: %s", e.Code, e.Code, e.Title)
		case !strings.Contains(e.Body, "\n## Wrong\n") || !strings.Contains(e.Body, "\n## Right\n"):
			t.Errorf("%s: needs a ## Wrong and a ## Right example", e.Code)
		}
		if prev, ok := titles[e.Title]; ok {
			t.Errorf("%s and %s share the title %q", prev, e.Code, e.Title)
		}
		titles[e.Title] = e.Code
		if i > 0 && entries[i-1].Code == e.Code {
			t.Errorf("%s is listed twice", e.Code)
		}
		if l, ok := Lookup(strings.ToLower(e.Code)); !ok || l.Title != e.Title {
			t.Errorf("Lookup(%s) = %v, %v", e.Code, l, ok)
		}
	}
}

// TestCodesUsed checks that every code the toolchain's source uses has a
// page, and every page a use: vuka's own source, and ui's and web's when
// they are checked out beside it (or at $VUKA_UI and $VUKA_WEB).
func TestCodesUsed(t *testing.T) {
	used := map[string][]string{}
	scan(t, "..", used)
	siblings := 0
	for _, s := range []struct{ env, dir string }{{"VUKA_UI", "vuka-ui"}, {"VUKA_WEB", "vuka-web"}} {
		dir := os.Getenv(s.env)
		if dir == "" {
			dir = filepath.Join("..", "..", s.dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			t.Logf("%s isn't checked out at %s: its codes aren't checked", s.dir, dir)
			continue
		}
		siblings++
		scan(t, dir, used)
	}
	if len(used) == 0 {
		t.Fatal("no codes found in the source")
	}
	for code, at := range used {
		if _, ok := Lookup(code); !ok {
			t.Errorf("%s (used at %s) has no page: add errors/%s.md", code, strings.Join(at, ", "), code)
		}
	}
	if siblings < 2 {
		return
	}
	for _, e := range Errors() {
		if _, ok := used[e.Code]; !ok {
			t.Errorf("%s has a page and no use", e.Code)
		}
	}
}

var useRe = regexp.MustCompile(`"([EW]\d{4})"`)

func scan(t *testing.T, root string, used map[string][]string) {
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		n := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(n, ".") || n == "testdata" || n == "node_modules" || n == "docs" || n == "build" || n == "examples") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range useRe.FindAllSubmatchIndex(b, -1) {
			code := string(b[m[2]:m[3]])
			used[code] = append(used[code], fmt.Sprintf("%s:%d", p, bytes.Count(b[:m[0]], []byte("\n"))+1))
		}
		return nil
	})
}

func TestIndex(t *testing.T) {
	got := index()
	if *update {
		if err := os.WriteFile("errors/index.md", got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile("errors/index.md")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("errors/index.md is out of date: go test ./docs -run TestIndex -update")
	}
}

// index is errors/index.md: every code, by area.
func index() []byte {
	var b bytes.Buffer
	b.WriteString(`# Errors

Every diagnostic the vuka toolchain reports has a stable code: the
compiler, ` + "`vuka vet`" + `, the editor and the libraries' checks at boot. ` + "`E`" + ` is
an error and ` + "`W`" + ` a warning; the first two digits name the area. Each page
says what is wrong, why, and shows the fix. ` + "`vuka explain E0201`" + ` prints the
same page in the terminal.

`)
	byArea := map[string][]Entry{}
	for _, e := range Errors() {
		byArea[e.Code[1:3]] = append(byArea[e.Code[1:3]], e)
	}
	areas := make([]string, 0, len(byArea))
	for a := range byArea {
		areas = append(areas, a)
	}
	sort.Strings(areas)
	for _, a := range areas {
		fmt.Fprintf(&b, "## %s\n\n| Code | |\n|---|---|\n", Areas[a])
		for _, e := range byArea[a] {
			fmt.Fprintf(&b, "| [%s](./%s) | %s |\n", e.Code, e.Code, e.Title)
		}
		b.WriteString("\n")
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}
