package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// linkModule is a module with a route registry, a main that serves what was
// registered, and a page package main never imports: the auto-link case.
func linkModule(t *testing.T, mainSrc string) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	return tempModule(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n",
		"reg/reg.vuka": `package reg

import "github.com/vuka-lang/vuka"

var Routes []string

func Route(path string) func(*vuka.Decl) {
	return func(d *vuka.Decl) { Routes = append(Routes, path) }
}
`,
		"pages/alpha/alpha.vuka": "package alpha\n\nimport \"example.com/app/reg\"\n\n@reg.Route(\"/applicants\")\nfunc Applicants() {}\n",
		"main.vuka":              mainSrc,
	})
}

func runVuka(t *testing.T, vuka, dir string, args ...string) (string, error) {
	t.Helper()
	c := exec.Command(vuka, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", twEnv+"=off")
	out, err := c.CombinedOutput()
	return string(out), err
}

// TestAutoLinkBuild mirrors the incident: a page package nothing imports is
// compiled into the binary by vuka build — its routes register, and an
// unused variable in it fails the build — and vuka test, vet and gen see it
// the same way.
func TestAutoLinkBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	vuka := buildVuka(t)
	root := linkModule(t, `package main

import (
	"fmt"

	"example.com/app/reg"
)

func main() { fmt.Println(reg.Routes) }
`)
	bin := filepath.Join(root, "app")
	out, err := runVuka(t, vuka, root, "build", "-o", bin, ".")
	if err != nil {
		t.Fatalf("vuka build: %v\n%s", err, out)
	}
	if !strings.Contains(out, "● linking 1 declarer package (pages/alpha)") {
		t.Errorf("vuka build didn't say what it linked:\n%s", out)
	}
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "[/applicants]" {
		t.Errorf("the unimported page package's route: %q", got)
	}
	if out, err := runVuka(t, vuka, root, "vet", "./..."); err != nil {
		t.Errorf("vuka vet: %v\n%s", err, out)
	}
	if out, err := runVuka(t, vuka, root, "test", "./..."); err != nil {
		t.Errorf("vuka test: %v\n%s", err, out)
	}

	// An unused variable in the linked package now fails the build.
	alpha := filepath.Join(root, "pages", "alpha", "alpha.vuka")
	good, _ := os.ReadFile(alpha)
	writeFile(t, alpha, string(good)+"\nfunc bad() {\n\tx := 1\n}\n")
	if out, err := runVuka(t, vuka, root, "build", "-o", bin, "."); err == nil || !strings.Contains(out, "declared and not used") {
		t.Errorf("an unused variable in the linked package: err %v\n%s", err, out)
	}
	writeFile(t, alpha, string(good))

	// vuka gen -inplace writes the link file, plain go build works, and
	// -check reports drift once a new declarer package appears.
	if out, err := runVuka(t, vuka, root, "gen", "-inplace"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out)
	}
	link, err := os.ReadFile(filepath.Join(root, "vuka_link_gen.go"))
	if err != nil || !strings.Contains(string(link), `_ "example.com/app/pages/alpha"`) {
		t.Fatalf("vuka_link_gen.go: %v\n%s", err, link)
	}
	c := exec.Command("go", "build", "-o", bin, ".")
	c.Dir = root
	c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("plain go build after gen -inplace: %v\n%s", err, out)
	}
	if got, _ := exec.Command(bin).Output(); strings.TrimSpace(string(got)) != "[/applicants]" {
		t.Errorf("the plain build's route: %q", got)
	}
	if out, err := runVuka(t, vuka, root, "gen", "-inplace", "-check"); err != nil {
		t.Errorf("gen -inplace -check right after gen: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(root, "pages", "beta", "beta.vuka"), "package beta\n\nimport \"example.com/app/reg\"\n\n@reg.Route(\"/beta\")\nfunc Beta() {}\n")
	if out, err := runVuka(t, vuka, root, "gen", "-inplace", "-check"); err == nil || !strings.Contains(out, "stale: vuka_link_gen.go") {
		t.Errorf("gen -inplace -check with a new declarer package: err %v\n%s", err, out)
	}
	if out, err := runVuka(t, vuka, root, "gen", "-inplace"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out)
	}
	if link, _ := os.ReadFile(filepath.Join(root, "vuka_link_gen.go")); !strings.Contains(string(link), `_ "example.com/app/pages/beta"`) {
		t.Errorf("vuka_link_gen.go after a new package:\n%s", link)
	}

	// With the page packages gone, the next build empties the on-disk link
	// file through the overlay instead of tripping over its stale imports.
	if err := os.RemoveAll(filepath.Join(root, "pages")); err != nil {
		t.Fatal(err)
	}
	if out, err := runVuka(t, vuka, root, "build", "-o", bin, "."); err != nil {
		t.Fatalf("vuka build after removing the pages: %v\n%s", err, out)
	}
	if got, _ := exec.Command(bin).Output(); strings.TrimSpace(string(got)) != "[]" {
		t.Errorf("routes after removing the pages: %q", got)
	}

	// //vuka:nolink on the main package turns auto-link off for it.
	if out, err := runVuka(t, vuka, root, "gen", "-inplace"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(root, "pages", "alpha", "alpha.vuka"), "package alpha\n\nimport \"example.com/app/reg\"\n\n@reg.Route(\"/applicants\")\nfunc Applicants() {}\n")
	main, _ := os.ReadFile(filepath.Join(root, "main.vuka"))
	writeFile(t, filepath.Join(root, "main.vuka"), "//vuka:nolink\n"+string(main))
	os.Remove(filepath.Join(root, "vuka_link_gen.go"))
	if out, err := runVuka(t, vuka, root, "build", "-o", bin, "."); err != nil {
		t.Fatalf("vuka build with //vuka:nolink: %v\n%s", err, out)
	} else if strings.Contains(out, "● linking") {
		t.Errorf("//vuka:nolink main still links:\n%s", out)
	}
	if got, _ := exec.Command(bin).Output(); strings.TrimSpace(string(got)) != "[]" {
		t.Errorf("routes under //vuka:nolink: %q", got)
	}
}

// TestRunWatchAutoLink: a declarer package created during a watch session is
// linked by the next rebuild, and a deleted one unlinked without breaking
// the build.
func TestRunWatchAutoLink(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	vuka := buildVuka(t)
	root := linkModule(t, `package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"example.com/app/reg"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "routes:"+strings.Join(reg.Routes, ","))
	})
	http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), nil)
}
`)
	alpha := filepath.Join(root, "pages", "alpha", "alpha.vuka")
	beta := filepath.Join(root, "pages", "beta", "beta.vuka")
	r := startWatch(t, vuka, root, nil)
	waitBody := func(want string) {
		t.Helper()
		for end := time.Now().Add(60 * time.Second); ; time.Sleep(100 * time.Millisecond) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", r.port))
			if err == nil {
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if string(b) == want {
					return
				}
			}
			if time.Now().After(end) {
				t.Fatalf("the app never answered %q; output:\n%s", want, r.log.String())
			}
		}
	}
	waitBody("routes:/applicants")
	r.waitLog("● linking 1 declarer package (pages/alpha)", 1)

	writeFile(t, beta, "package beta\n\nimport \"example.com/app/reg\"\n\n@reg.Route(\"/beta\")\nfunc Beta() {}\n")
	waitBody("routes:/applicants,/beta")
	r.waitLog("● linking 2 declarer packages (pages/alpha, pages/beta)", 1)

	if err := os.RemoveAll(filepath.Dir(alpha)); err != nil {
		t.Fatal(err)
	}
	waitBody("routes:/beta")
	r.stop()
	if strings.Contains(r.log.String(), "build failed") {
		t.Errorf("a removed declarer package broke a build:\n%s", r.log.String())
	}
}
