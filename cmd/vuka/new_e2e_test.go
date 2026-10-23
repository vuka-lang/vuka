package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// e2e is a vuka command built from this package and a directory to make
// apps in. The apps require the released web, ui and orm, or the local
// checkouts VUKA_NEW_REPLACE names (module=dir,…).
type e2e struct {
	t    *testing.T
	vuka string
	dir  string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs apps")
	}
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	vuka := filepath.Join(dir, "vuka")
	if out, err := exec.Command("go", "build", "-o", vuka, ".").CombinedOutput(); err != nil {
		t.Fatalf("building vuka: %v\n%s", err, out)
	}
	besideVukavet(t, vuka)
	return &e2e{t, vuka, dir}
}

// run runs a command in dir, with the app's environment: this vuka first
// on PATH, no workspace.
func (e *e2e) run(dir string, env []string, name string, args ...string) string {
	e.t.Helper()
	if name == "vuka" {
		name = e.vuka
	}
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), append([]string{"GOWORK=off", "PATH=" + filepath.Dir(e.vuka) + string(os.PathListSeparator) + os.Getenv("PATH")}, env...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		e.t.Fatalf("%s %s in %s: %v\n%s", filepath.Base(name), strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// new makes an app with vuka new and these flags, checking its
// dependencies resolved.
func (e *e2e) new(name string, flags ...string) string {
	e.t.Helper()
	out := e.run(e.dir, nil, "vuka", append([]string{"new", name, "example.com/" + name, "--yes"}, flags...)...)
	if strings.Contains(out, "couldn't fetch the dependencies") {
		needs(e.t, "the app's dependencies aren't available (set VUKA_NEW_REPLACE to local checkouts of web and orm):\n%s", out)
	}
	return filepath.Join(e.dir, name)
}

func freePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// serve runs the app's binary on a free port until the test ends or stop
// is called; it returns its URL.
func serve(t *testing.T, dir, bin string) (base string, stop func()) {
	t.Helper()
	port := freePort(t)
	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PORT="+port)
	logs, _ := cmd.StderrPipe()
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(logs)
		for sc.Scan() {
			log.WriteString(sc.Text() + "\n")
		}
		close(done)
	}()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
		<-done
	}
	t.Cleanup(stop)
	base = "http://127.0.0.1:" + port
	for start := time.Now(); ; time.Sleep(50 * time.Millisecond) {
		if res, err := http.Get(base + "/static/logo.svg"); err == nil {
			res.Body.Close()
			return base, stop
		}
		if time.Since(start) > 15*time.Second {
			stop()
			t.Fatalf("the app didn't start:\n%s", log.String())
		}
	}
}

// envValue is key's value in the app's .env.
func envValue(t *testing.T, dir, key string) string {
	for line := range strings.SplitSeq(read(t, filepath.Join(dir, ".env")), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && k == key {
			return v
		}
	}
	t.Fatalf("no %s in .env", key)
	return ""
}

// The default app, end to end: its tests pass, it grows a module, it
// builds, the binary serves the login page, the admin from .env signs in
// (the migrations ran, the user was made), and a restart keeps the
// session; in Chrome the login page shows the logo in both themes, and
// signing in and out works.
func TestNewEndToEnd(t *testing.T) {
	e := newE2E(t)
	dir := e.new("shop")
	for _, f := range []string{"main.vuka", "pages/login/index.vuka.html", "modules/users/stores.vuka", "migrations/0001_initial.go", "static/logo.svg", "go.sum"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("no %s", f)
		}
	}
	e.run(dir, nil, "vuka", "test", "./...")

	// A module: its test passes, and its migration is what the models need.
	e.run(dir, nil, "vuka", "add", "module", "notes")
	e.run(dir, nil, "vuka", "test", "./...")
	if out := e.run(dir, nil, "go", "tool", "orm", "makemigrations", "--check"); !strings.Contains(out, "No changes") {
		t.Errorf("makemigrations --check: %s", out)
	}

	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no Tailwind CLI: vuka build needs it")
	}
	bin := filepath.Join(dir, "shop")
	e.run(dir, nil, "vuka", "build", "-o", bin, ".")
	base, stop := serve(t, dir, bin)

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Accept", "text/html")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	login := func(password string) (int, string) {
		t.Helper()
		form := url.Values{"login": {"admin@example.com"}, "password": {password}, "next": {"/"}}
		req, _ := http.NewRequest("POST", base+"/auth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", base)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode, res.Header.Get("Location")
	}

	if code, body := get("/login"); code != 200 || !strings.Contains(body, `src="/static/logo.svg"`) || !strings.Contains(body, `name="remember"`) {
		t.Fatalf("the login page: %d", code)
	}
	if code, to := login("not the password"); code != 303 || !strings.HasPrefix(to, "/login?error=invalid") {
		t.Errorf("a wrong password: %d %s", code, to)
	}
	password := envValue(t, dir, "ADMIN_PASSWORD")
	if code, to := login(password); code != 303 || to != "/" {
		t.Fatalf("the admin's sign-in: %d %s", code, to)
	}
	if code, body := get("/"); code != 200 || !strings.Contains(body, "Welcome back, Admin") {
		t.Errorf("home: %d", code)
	}
	if code, body := get("/users"); code != 200 || !strings.Contains(body, "admin@example.com") {
		t.Errorf("users: %d", code)
	}
	if code, _ := get("/notes"); code != 200 {
		t.Errorf("the notes module's page: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "app.db")); err != nil {
		t.Errorf("the SQLite database: %v", err)
	}
	stop()
	if out := e.run(dir, nil, "go", "tool", "orm", "showmigrations"); !strings.Contains(out, "[X] 0001_initial") || !strings.Contains(out, "[X] 0002_notes") {
		t.Errorf("showmigrations: %s", out)
	}

	// A restart keeps the session: it is in the database.
	base2, _ := serve(t, dir, bin)
	from, _ := url.Parse(base)
	to, _ := url.Parse(base2)
	jar.SetCookies(to, jar.Cookies(from))
	base = base2
	if code, _ := get("/"); code != 200 {
		t.Errorf("home after a restart: %d", code)
	}

	browserCheck(t, base, password)
}

// browserCheck signs in and out in Chrome, and looks at the login page in
// both themes. With VUKA_SHOTS set, it saves screenshots there.
func browserCheck(t *testing.T, base, password string) {
	shots := os.Getenv("VUKA_SHOTS")
	shot := func(p *tab, name string) {
		if shots != "" {
			p.eval(`new Promise((r) => setTimeout(r, 250))`) // transitions
			p.screenshot(filepath.Join(shots, name+".png"))
		}
	}
	ch := launchChrome(t)
	p := ch.open(base+"/login", 1280, 860)
	logo := `(() => { const i = document.querySelector('img[src="/static/logo.svg"]'); return i && i.complete && i.naturalWidth > 0 })()`
	background := `getComputedStyle(document.querySelector("form").closest("div")).backgroundColor`
	p.wait(logo)
	light := p.eval(background)
	shot(p, "login-light")
	p.dark(true)
	p.wait(logo)
	if dark := p.eval(background); dark == light {
		t.Errorf("the login card is %v in both themes", dark)
	}
	shot(p, "login-dark")

	p.fill("input[name=login]", "admin@example.com")
	p.fill("input[name=password]", "not the password")
	p.click("#login-form button[type=submit]")
	p.wait(`location.search.includes("error=invalid") && document.readyState === "complete"`)
	if msg := p.text("[role=alert]"); msg != "That email and password don't match an account." {
		t.Errorf("the error: %q", msg)
	}
	if v := p.eval(`document.querySelector("input[name=login]").value`); v != "admin@example.com" {
		t.Errorf("the email after an error: %v", v)
	}
	shot(p, "login-error-dark")

	p.dark(false)
	p.fill("input[name=password]", password)
	p.click("#login-form button[type=submit]")
	p.wait(`location.pathname === "/" && document.documentElement.dataset.vkState === "connected"`)
	if h := p.text("h1"); h != "Welcome back, Admin" {
		t.Errorf("the dashboard: %q", h)
	}
	shot(p, "dashboard")

	// The user menu opens in the browser; its sign-out ends the session.
	p.click("#user-button")
	p.wait(`getComputedStyle(document.querySelector("#user-menu")).display !== "none" && document.querySelector("#user-button").getAttribute("aria-expanded") === "true"`)
	shot(p, "user-menu")
	p.dark(true)
	shot(p, "user-menu-dark")
	p.dark(false)

	// Live navigation to the notes module's page; a note is added in place.
	p.click(`#user-button`)
	p.click(`header nav a[href="/notes"]`)
	p.wait(`location.pathname === "/notes" && document.querySelector('a[href="/notes"]').getAttribute("aria-current") === "page"`)
	p.fill("input[name=title]", "Ship the starter")
	p.click(`#new-notes button[type=submit]`)
	p.wait(`document.querySelector("#notes").textContent.includes("Ship the starter")`)
	shot(p, "notes")

	p.click("#user-button")
	p.click("#sign-out")
	p.wait(`location.pathname === "/login"`)
	p.goTo(base + "/")
	p.wait(`location.pathname === "/login" && location.search === "?next=%2F"`)
}

// The other kinds compile, and their tests pass: PostgreSQL (the tests run
// on SQLite), a React frontend, no sign-in, templ views, an API.
func TestNewVariants(t *testing.T) {
	e := newE2E(t)
	off := []string{"VUKA_TAILWIND=off", "VUKA_FRONTEND=off"}
	for _, v := range []struct {
		name  string
		flags []string
		check func(dir string)
	}{
		{"pgshop", []string{"--db=postgres"}, func(dir string) {
			if !strings.Contains(read(t, filepath.Join(dir, "main.vuka")), `"github.com/vuka-lang/orm/v2/postgres"`) {
				t.Error("postgres: no driver import")
			}
		}},
		{"reactshop", []string{"--frontend=react"}, func(dir string) {
			if !strings.Contains(read(t, filepath.Join(dir, "frontend/package.json")), `"react"`) {
				t.Error("react: no package.json")
			}
		}},
		{"openshop", []string{"--auth=no", "--mail=no"}, nil},
		{"templshop", []string{"--ui=templ"}, nil},
		{"apishop", []string{"--ui=api", "--frontend=vue"}, nil},
	} {
		dir := e.new(v.name, v.flags...)
		if out := e.run(dir, off, "vuka", "vet", "./..."); strings.Contains(out, "warning[W030") {
			t.Errorf("%s: the scaffold uses deprecated forms:\n%s", v.name, out)
		}
		e.run(dir, off, "vuka", "test", "./...")
		e.run(dir, off, "vuka", "build", "-o", filepath.Join(dir, v.name), ".")
		e.run(dir, off, "vuka", "add", "module", "tasks")
		e.run(dir, off, "vuka", "test", "./...")
		if v.check != nil {
			v.check(dir)
		}
	}
}
