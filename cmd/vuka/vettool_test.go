package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestMain points vuka vet at a vukavet built from this checkout, beside a
// pretend vuka: the tests vet with the analyzers they are tested with,
// never one fetched from the network.
func TestMain(m *testing.M) {
	// A vuka run -watch of a test keeps no build for a later run, unless the
	// test asks for it (TestRunWatchReuse, with its own VUKA_CACHE).
	if os.Getenv(reuseEnv) == "" {
		os.Setenv(reuseEnv, "off")
	}
	vukaExecutable = func() (string, error) {
		dir, err := checkoutVukavet()
		return filepath.Join(dir, "vuka"), err
	}
	code := m.Run()
	if vukavetDir != "" {
		os.RemoveAll(vukavetDir)
	}
	os.Exit(code)
}

var (
	vukavetOnce sync.Once
	vukavetDir  string
	vukavetErr  error
)

// checkoutVukavet is a directory holding cmd/vukavet built from this
// checkout, built once per test binary.
func checkoutVukavet() (string, error) {
	vukavetOnce.Do(func() {
		dir, err := os.MkdirTemp("", "vukavet-")
		if err != nil {
			vukavetErr = err
			return
		}
		vukavetDir = dir
		c := exec.Command("go", "build", "-o", filepath.Join(dir, "vukavet"+exeSuffix()), ".")
		c.Dir = filepath.Join(testDir, "..", "vukavet")
		c.Env = append(withoutModFlag(os.Environ()), "GOWORK=off")
		if out, err := c.CombinedOutput(); err != nil {
			vukavetErr = errors.New("go build vukavet: " + err.Error() + "\n" + string(out))
		}
	})
	return vukavetDir, vukavetErr
}

// besideVukavet puts this checkout's vukavet beside the vuka at vuka.
func besideVukavet(t *testing.T, vuka string) {
	t.Helper()
	dir, err := checkoutVukavet()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "vukavet"+exeSuffix()), filepath.Join(filepath.Dir(vuka), "vukavet"+exeSuffix())); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
}

func TestHasVettool(t *testing.T) {
	for args, want := range map[string]bool{
		"./...":                      false,
		"-vettool=/x/vukavet ./...":  true,
		"--vettool=/x/vukavet ./...": true,
		"-vettool /x/vukavet ./...":  true,
		"-v ./... -- -vettool=x":     false,
		"-vettools=x":                false,
	} {
		if got := hasVettool(strings.Fields(args)); got != want {
			t.Errorf("hasVettool(%s) = %v, want %v", args, got, want)
		}
	}
}

// TestVetTool: the vukavet vuka vet runs — VUKA_VUKAVET's, the app's go
// tool, one beside vuka, on PATH, or one it builds — and what it says when
// there is none.
func TestVetTool(t *testing.T) {
	if testing.Short() {
		t.Skip("builds vukavet")
	}
	built, err := checkoutVukavet()
	if err != nil {
		t.Fatal(err)
	}
	vukavet := filepath.Join(built, "vukavet"+exeSuffix())
	empty := t.TempDir()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("PATH", filepath.Dir(goTool(t)))
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LocalAppData", cache)
	app := tempModule(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.24\n"})
	saved, savedInstall := vukaExecutable, installVukavet
	t.Cleanup(func() { vukaExecutable, installVukavet = saved, savedInstall })
	at := func(dir string) func() (string, error) {
		return func() (string, error) { return filepath.Join(dir, "vuka"), nil }
	}
	tool := func(t *testing.T, root string) (string, string) {
		t.Helper()
		var stderr bytes.Buffer
		p, err := vetTool(root, &stderr)
		if err != nil {
			t.Fatalf("vetTool: %v\n%s", err, stderr.Bytes())
		}
		return p, stderr.String()
	}

	t.Run("beside vuka", func(t *testing.T) {
		vukaExecutable = at(built)
		if p, out := tool(t, app); p != vukavet || out != "" {
			t.Errorf("vetTool = %q, said %q; want %s", p, out, vukavet)
		}
	})
	t.Run("on PATH", func(t *testing.T) {
		vukaExecutable = at(empty)
		t.Setenv("PATH", built+string(os.PathListSeparator)+os.Getenv("PATH"))
		if p, _ := tool(t, app); p != vukavet {
			t.Errorf("vetTool = %q, want %s", p, vukavet)
		}
	})
	t.Run("VUKA_VUKAVET", func(t *testing.T) {
		vukaExecutable = at(built)
		t.Setenv(vukavetEnv, "off")
		if p, out := tool(t, app); p != "" || out != "" {
			t.Errorf("VUKA_VUKAVET=off: vetTool = %q, said %q", p, out)
		}
		t.Setenv(vukavetEnv, filepath.Join(empty, "nope"))
		if _, err := vetTool(app, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no such program") {
			t.Errorf("VUKA_VUKAVET naming nothing: %v", err)
		}
	})
	t.Run("the app's tool", func(t *testing.T) {
		vukaExecutable = at(built)
		root := tempModule(t, map[string]string{
			"go.mod":          "module example.com/app\n\ngo 1.24\n\ntool " + vukavetModPath + "\n\nrequire " + vukavetModPath + " v2.0.0\n\nreplace " + vukavetModPath + " => ./fakevet\n",
			"fakevet/go.mod":  "module " + vukavetModPath + "\n\ngo 1.24\n",
			"fakevet/main.go": "package main\n\nfunc main() {}\n",
		})
		p, _ := tool(t, root)
		if p == "" || p == vukavet || filepath.Base(p) != "vukavet"+exeSuffix() {
			t.Errorf("vetTool = %q, want the app's go tool vukavet", p)
		}
	})
	t.Run("built", func(t *testing.T) {
		vukaExecutable = at(empty)
		installVukavet = func(dir, v string) ([]byte, error) {
			if v != version {
				t.Errorf("builds vukavet %s, want %s", v, version)
			}
			return nil, os.WriteFile(filepath.Join(dir, "vukavet"+exeSuffix()), nil, 0o755)
		}
		p, out := tool(t, app)
		if filepath.Base(p) != "vukavet"+exeSuffix() || !strings.HasPrefix(p, cache) || !strings.Contains(out, "building vukavet "+version) {
			t.Errorf("vetTool = %q, said %q", p, out)
		}
		installVukavet = func(string, string) ([]byte, error) { t.Error("built again"); return nil, nil }
		if q, out := tool(t, app); q != p || out != "" {
			t.Errorf("the second time: vetTool = %q, said %q", q, out)
		}
	})
	t.Run("none", func(t *testing.T) {
		vukaExecutable = at(empty)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CACHE_HOME", os.Getenv("HOME"))
		t.Setenv("LocalAppData", os.Getenv("HOME"))
		installVukavet = func(string, string) ([]byte, error) {
			return []byte("no network"), errors.New("exit status 1")
		}
		p, out := tool(t, app)
		if p != "" || !strings.Contains(out, "without vukavet's checks") || !strings.Contains(out, "no network") || !strings.Contains(out, "go get -tool "+vukavetModPath) {
			t.Errorf("vetTool = %q, said %q", p, out)
		}
	})
}

// goTool is the go command's path, in GOROOT.
func goTool(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return p
}

// TestVetRunsVukavet: plain vuka vet runs vukavet's checks — a live list
// without keys is W0102 — and VUKA_VUKAVET=off leaves them out.
func TestVetRunsVukavet(t *testing.T) {
	e := newE2E(t)
	off := []string{"VUKA_TAILWIND=off", "VUKA_FRONTEND=off"}
	dir := e.new("vetshop", "--auth=no")
	writeFile(t, filepath.Join(dir, "pages", "rows.vuka.html"), `package pages

// Rows is a live list whose items have no key.
type Rows struct {
	ui.Live
	rows []string
}

func (r *Rows) Add() { r.rows = append(r.rows, "x") }
<ul>
  for _, s := range rows {
    <li>{s}</li>
  }
  <button onClick={Add}>add</button>
</ul>
`)
	out, err := e.cmd(dir, off, "vuka", "vet", "./...").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "W0102") {
		t.Errorf("vuka vet with a keyless live list: %v\n%s", err, out)
	}
	if out, err := e.cmd(dir, append(off, vukavetEnv+"=off"), "vuka", "vet", "./...").CombinedOutput(); err != nil || strings.Contains(string(out), "W0102") {
		t.Errorf("VUKA_VUKAVET=off vuka vet: %v\n%s", err, out)
	}
}
