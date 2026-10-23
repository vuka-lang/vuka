package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/cmd/vuka/v2/internal/progress"
	"github.com/vuka-lang/vuka/v2/diag"
)

// Frontends for apps on github.com/vuka-lang/web/v2 (web.Frontend): a Vite
// project in frontend/ (a directory with a package.json). vuka run starts its
// dev server beside the app, whose pages then load their modules from it;
// vuka build runs vite build before the go build embeds the output. Both
// install the project's dependencies when Vite isn't installed yet, and keep
// the Vite plugin (vuka/vite.js, from the web module the app links) up to
// date. A frontend directory without a package.json is a prebuilt one,
// served as it is.

const (
	feEnv = "VUKA_FRONTEND" // the frontend's directory when it isn't frontend/; off: none
	// fePlugin is where the plugin is written in the frontend's directory,
	// and fePluginSrc where web keeps it.
	fePlugin    = "vuka/vite.js"
	fePluginSrc = "vite/vuka.js"
)

// frontendDir is the Vite project to run: $VUKA_FRONTEND, else frontend/ in
// the working directory, else in the module root; "" when there is none
// with a package.json.
func frontendDir(root string) string {
	if v := os.Getenv(feEnv); strings.EqualFold(v, "off") {
		return ""
	} else if v != "" {
		dirs, _ := filepath.Abs(v)
		return dirIf(dirs)
	}
	wd, _ := os.Getwd()
	for _, d := range []string{wd, root} {
		if dir := dirIf(filepath.Join(d, "frontend")); dir != "" {
			return dir
		}
	}
	return ""
}

func dirIf(dir string) string {
	if fileExists(filepath.Join(dir, "package.json")) {
		return dir
	}
	return ""
}

// installCommand is how the project installs its dependencies: its
// package.json's packageManager, else the one its lockfile is for, else
// npm; frozen to the lockfile when there is one.
func installCommand(dir string) []string {
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		json.Unmarshal(data, &pkg)
	}
	name, _, _ := strings.Cut(pkg.PackageManager, "@")
	locks := map[string]string{"pnpm": "pnpm-lock.yaml", "yarn": "yarn.lock", "bun": "bun.lock", "npm": "package-lock.json"}
	if name == "" {
		for _, n := range []string{"pnpm", "yarn", "bun", "npm"} {
			if fileExists(filepath.Join(dir, locks[n])) || n == "bun" && fileExists(filepath.Join(dir, "bun.lockb")) {
				name = n
				break
			}
		}
	}
	if name == "" {
		name = "npm"
	}
	locked := fileExists(filepath.Join(dir, locks[name])) || name == "bun" && fileExists(filepath.Join(dir, "bun.lockb"))
	switch {
	case name == "npm":
		if locked {
			return []string{"npm", "ci"}
		}
		return []string{"npm", "install"}
	case !locked:
		return []string{name, "install"}
	case name == "yarn" && fileExists(filepath.Join(dir, ".yarnrc.yml")):
		return []string{"yarn", "install", "--immutable"}
	}
	return []string{name, "install", "--frozen-lockfile"}
}

// task is a long, quiet step shown on w as it goes (see package progress):
// a spinner when w is a terminal, a heartbeat line elsewhere.
func task(w io.Writer, label, done, failed, still string) progress.Task {
	tty := false
	if f, ok := w.(*os.File); ok && isTerminal(f) && os.Getenv("TERM") != "dumb" {
		tty = true
	}
	return progress.Task{W: w, TTY: tty, Color: tty && os.Getenv("NO_COLOR") == "",
		Label: label, Done: done, Failed: failed, Still: still}
}

// installTask is a package install (cmd) in dir; the tool's output shows
// only when it fails.
func installTask(w io.Writer, dir string, cmd []string) progress.Task {
	where := filepath.ToSlash(dir) + "/"
	return task(w, "installing "+cmd[0]+" packages in "+where, cmd[0]+" packages installed in "+where,
		strings.Join(cmd, " ")+" failed", "still installing")
}

func viteBinary(dir string) string {
	return filepath.Join(dir, "node_modules", ".bin", "vite")
}

// prepareFrontend installs the dependencies when Vite isn't installed and
// brings the plugin up to date.
func prepareFrontend(root, dir string, stderr io.Writer) error {
	if !fileExists(viteBinary(dir)) {
		cmd := installCommand(dir)
		if _, err := exec.LookPath(cmd[0]); err != nil {
			return diag.New("E0602", "frontend: %s isn't installed: the frontend in %s needs Node.js 20+ and its package manager", cmd[0], rel(root, dir)).
				WithHelp("install Node.js 20+ and %s, or set %s=off to leave the frontend alone", cmd[0], feEnv)
		}
		if err := checkNPM(cmd, rel(root, dir)+"/", stderr); err != nil {
			return err
		}
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = dir
		if err := installTask(stderr, rel(root, dir), cmd).Run(c); err != nil {
			return fmt.Errorf("frontend: %s: %v", strings.Join(cmd, " "), err)
		}
		if !fileExists(viteBinary(dir)) {
			return diag.New("E0603", "frontend: %s has no Vite after installing", rel(root, dir)).WithHelp("add it: npm i -D vite")
		}
	}
	return syncPlugin(root, dir)
}

// syncPlugin writes the Vite plugin of the web module the app links into
// the frontend, when its vite.config imports it or an older copy is there.
func syncPlugin(root, dir string) error {
	target := filepath.Join(dir, filepath.FromSlash(fePlugin))
	if !fileExists(target) && !importsPlugin(dir) {
		return nil
	}
	c := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", webPath)
	c.Dir = root
	out, err := c.Output()
	web := strings.TrimSpace(string(out))
	if err != nil || web == "" {
		return nil // no web: nothing to copy
	}
	for _, ext := range []string{".js", ".d.ts"} {
		src, err := os.ReadFile(filepath.Join(web, filepath.FromSlash(strings.TrimSuffix(fePluginSrc, ".js")+ext)))
		if err != nil {
			if ext == ".js" {
				return nil // a web without the plugin
			}
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		if err := writeIfChanged(strings.TrimSuffix(target, ".js")+ext, src); err != nil {
			return err
		}
	}
	return nil
}

func importsPlugin(dir string) bool {
	for _, n := range []string{"vite.config.js", "vite.config.ts", "vite.config.mjs", "vite.config.mts"} {
		if src, err := os.ReadFile(filepath.Join(dir, n)); err == nil && bytes.Contains(src, []byte(strings.TrimSuffix(fePlugin, ".js"))) {
			return true
		}
	}
	return false
}

func rel(root, dir string) string {
	if r, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return dir
}

// buildFrontend runs vite build for vuka build, before the go build embeds
// its output.
func buildFrontend(root string, stderr io.Writer) error {
	dir := frontendDir(root)
	if dir == "" {
		return nil
	}
	if err := prepareFrontend(root, dir, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "vuka: building the frontend (vite build in %s)\n", rel(root, dir))
	c := exec.Command(viteBinary(dir), "build")
	c.Dir = dir
	c.Env = append(os.Environ(), "NODE_ENV=production")
	w := &prefixed{w: stderr, prefix: "vite: "}
	c.Stdout, c.Stderr = w, w
	err := c.Run()
	w.flush()
	if err != nil {
		return fmt.Errorf("frontend: vite build: %v", err)
	}
	return nil
}

// devFrontend starts the frontend's Vite dev server for vuka run, in the
// background, and returns a function stopping it (SIGTERM, then SIGKILL
// after 2s), which waits for it to exit so the plugin removes its hot file.
func devFrontend(root string, stderr io.Writer) func() {
	dir := frontendDir(root)
	if dir == "" {
		return func() {}
	}
	var (
		mu      sync.Mutex
		cmd     *exec.Cmd
		done    chan struct{}
		stopped bool
	)
	go func() {
		if err := prepareFrontend(root, dir, stderr); err != nil {
			fmt.Fprintln(stderr, "vuka:", err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return
		}
		c := exec.Command(viteBinary(dir))
		c.Dir = dir
		ownGroup(c) // a terminal's Ctrl-C reaches vuka, which stops Vite after the app
		w := &prefixed{w: stderr, prefix: "vite: ", dev: true}
		c.Stdout, c.Stderr = w, w
		if err := c.Start(); err != nil {
			fmt.Fprintln(stderr, "vuka: frontend:", err)
			return
		}
		cmd, done = c, make(chan struct{})
		go func() { c.Wait(); w.flush(); close(done) }()
	}()
	return sync.OnceFunc(func() {
		mu.Lock()
		stopped = true
		c, d := cmd, done
		mu.Unlock()
		if c == nil {
			return
		}
		terminate(c)
		select {
		case <-d:
		case <-time.After(2 * time.Second):
			kill(c)
			<-d
		}
	})
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// prefixed passes on a tool's output line by line, each prefixed. For the
// dev server (dev) it leaves out the banner with the dev server's own
// address: the app's pages are opened at the app's address.
type prefixed struct {
	w      io.Writer
	prefix string
	dev    bool
	buf    []byte
}

func (p *prefixed) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			return len(b), nil
		}
		p.line(string(p.buf[:i]))
		p.buf = p.buf[i+1:]
	}
}

func (p *prefixed) flush() {
	if len(p.buf) > 0 {
		p.line(string(p.buf))
		p.buf = nil
	}
}

func (p *prefixed) line(s string) {
	plain := strings.TrimSpace(ansi.ReplaceAllString(s, ""))
	if plain == "" {
		return
	}
	if p.dev {
		switch {
		case strings.Contains(plain, "ready in"):
			plain = "dev server ready: open the app's address; its pages load their modules from Vite"
		case strings.HasPrefix(plain, "➜") || strings.HasPrefix(plain, "Local:") || strings.HasPrefix(plain, "Network:") || strings.Contains(plain, "press h"):
			return
		}
		s = plain
	}
	fmt.Fprintln(p.w, p.prefix+strings.TrimRight(s, " \r"))
}
