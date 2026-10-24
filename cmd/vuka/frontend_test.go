package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInstallCommand(t *testing.T) {
	for _, c := range []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"package.json": "{}"}, "npm install"},
		{map[string]string{"package.json": "{}", "package-lock.json": "{}"}, "npm ci"},
		{map[string]string{"package.json": "{}", "pnpm-lock.yaml": ""}, "pnpm install --frozen-lockfile"},
		{map[string]string{"package.json": "{}", "yarn.lock": ""}, "yarn install --frozen-lockfile"},
		{map[string]string{"package.json": "{}", "yarn.lock": "", ".yarnrc.yml": ""}, "yarn install --immutable"},
		{map[string]string{"package.json": "{}", "bun.lockb": ""}, "bun install --frozen-lockfile"},
		{map[string]string{"package.json": `{"packageManager": "pnpm@9.1.0"}`}, "pnpm install"},
		{map[string]string{"package.json": `{"packageManager": "pnpm@9.1.0"}`, "package-lock.json": "{}"}, "pnpm install"},
	} {
		dir := tempModule(t, c.files)
		if got := strings.Join(installCommand(dir), " "); got != c.want {
			t.Errorf("%v: %s; want %s", slices.Sorted(func(yield func(string) bool) {
				for k := range c.files {
					if !yield(k) {
						return
					}
				}
			}), got, c.want)
		}
	}
}

func TestFrontendDir(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "frontend/package.json": "{}", "cmd/app/main.go": "package main\n",
		"cmd/app/frontend/package.json": "{}", "web/package.json": "{}", "dist/index.html": ""})
	t.Chdir(root)
	if got := frontendDir(root); got != filepath.Join(root, "frontend") {
		t.Errorf("from the root: %s", got)
	}
	t.Chdir(filepath.Join(root, "cmd", "app"))
	if got := frontendDir(root); got != filepath.Join(root, "cmd", "app", "frontend") {
		t.Errorf("from the package: %s", got)
	}
	t.Setenv(feEnv, filepath.Join(root, "web"))
	if got := frontendDir(root); got != filepath.Join(root, "web") {
		t.Errorf("VUKA_FRONTEND: %s", got)
	}
	t.Setenv(feEnv, filepath.Join(root, "dist")) // prebuilt: nothing to run
	if got := frontendDir(root); got != "" {
		t.Errorf("a directory without package.json: %s", got)
	}
	t.Setenv(feEnv, "off")
	if got := frontendDir(root); got != "" {
		t.Errorf("off: %s", got)
	}
}

func TestPrefixed(t *testing.T) {
	var b bytes.Buffer
	w := &prefixed{w: &b, prefix: "vite: ", dev: true}
	w.Write([]byte("\n  \x1b[32mVITE\x1b[39m v8.3.4  ready in 312 ms\n\n  ➜  Local:   http://localhost:5173/\n  ➜  Network: use --host to expose\n  ➜  press h + enter to show help\n10:14:03 AM [vite] (client) hmr upd"))
	w.Write([]byte("ate /src/App.jsx\npartial"))
	w.flush()
	want := "vite: dev server ready: open the app's address; its pages load their modules from Vite\nvite: 10:14:03 AM [vite] (client) hmr update /src/App.jsx\nvite: partial\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// fakeVite installs a vite that logs its runs in the frontend directory and,
// as a dev server, runs until it's terminated.
func fakeVite(t *testing.T, dir string) {
	writeFile(t, filepath.Join(dir, "node_modules", ".bin", "vite"), `#!/bin/sh
echo "run $*" >> runs.log
if [ "$1" = build ]; then mkdir -p dist; echo built > dist/index.html; echo "vite build done"; exit 0; fi
trap 'echo stopped >> runs.log; exit 0' TERM
echo "  VITE v8.0.0  ready in 5 ms"
while :; do sleep 0.05; done
`)
	os.Chmod(filepath.Join(dir, "node_modules", ".bin", "vite"), 0o755)
	if runtime.GOOS == "windows" {
		// npm's shim, the one that runs there; it is killed, never asked.
		writeFile(t, filepath.Join(dir, "node_modules", ".bin", "vite.cmd"), "@echo off\r\n"+
			"echo run %*>> runs.log\r\n"+
			"if \"%1\"==\"build\" (\r\n  mkdir dist 2>nul\r\n  echo built> dist\\index.html\r\n  echo vite build done\r\n  exit /b 0\r\n)\r\n"+
			"echo   VITE v8.0.0  ready in 5 ms\r\n"+
			"ping -n 120 127.0.0.1 > nul\r\n")
	}
}

// crlf is s with Windows' line ends made \n: what a .cmd stand-in wrote.
func crlf(s []byte) string { return strings.ReplaceAll(string(s), "\r\n", "\n") }

func TestBuildFrontend(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "frontend/package.json": "{}"})
	fakeVite(t, filepath.Join(root, "frontend"))
	t.Chdir(root)
	var out bytes.Buffer
	if err := buildFrontend(root, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "vite: vite build done") {
		t.Errorf("output: %s", out.String())
	}
	if data, _ := os.ReadFile(filepath.Join(root, "frontend", "dist", "index.html")); crlf(data) != "built\n" {
		t.Errorf("dist/index.html: %q", data)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestDevFrontend(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "frontend/package.json": "{}"})
	dir := filepath.Join(root, "frontend")
	fakeVite(t, dir)
	t.Chdir(root)
	var out syncBuf
	stop := devFrontend(root, &out)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "dev server ready") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	start := time.Now()
	stop()
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("stopping Vite took %v", took)
	}
	want := "run \nstopped\n"
	if runtime.GOOS == "windows" {
		want = "run \n" // the .cmd shim is killed after the grace period
	}
	log, _ := os.ReadFile(filepath.Join(dir, "runs.log"))
	if crlf(log) != want || !strings.Contains(out.String(), "dev server ready") {
		t.Errorf("runs.log: %q; output: %s", log, out.String())
	}
	stop() // once
}

func TestTailwindSkipsFrontends(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod, "main.vuka": "package main\n",
		"frontend/package.json": "{}", "frontend/index.html": "<div></div>"})
	if tw, err := tailwindFor(root); tw != nil || err != nil {
		t.Errorf("a frontend's index.html made the app use Tailwind: %v %v", tw, err)
	}
}

// fakeNPM puts an npm on PATH (npm 11) that prints a lot and, unless it
// fails, installs a fake vite.
func fakeNPM(t *testing.T, fail bool) {
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 11.6.2; exit 0; fi\nfor i in 1 2 3 4 5 6 7 8; do echo \"npm noise $i\"; done\n"
	if fail {
		script += "echo 'npm error E404 not found' >&2\nexit 1\n"
	} else {
		script += "mkdir -p node_modules/.bin && printf '#!/bin/sh\\n' > node_modules/.bin/vite && chmod +x node_modules/.bin/vite\n"
	}
	writeFile(t, filepath.Join(bin, "npm"), script)
	os.Chmod(filepath.Join(bin, "npm"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPrepareFrontendInstallProgress(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "web/package.json": "{}"})
	dir := filepath.Join(root, "web")
	fakeNPM(t, false)
	var out bytes.Buffer
	if err := prepareFrontend(root, dir, &out); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^installing npm packages in web/ …\n✓ npm packages installed in web/ \(\d+s\)\n$`).MatchString(out.String()) {
		t.Errorf("output: %q", out.String())
	}

	os.RemoveAll(filepath.Join(dir, "node_modules"))
	fakeNPM(t, true)
	out.Reset()
	err := prepareFrontend(root, dir, &out)
	if err == nil {
		t.Fatal("no error")
	}
	got := out.String()
	if !strings.Contains(got, "✗ npm install failed (exit 1)\n") || !strings.HasSuffix(got, "  npm noise 8\n  npm error E404 not found\n") {
		t.Errorf("output: %q", got)
	}
}
