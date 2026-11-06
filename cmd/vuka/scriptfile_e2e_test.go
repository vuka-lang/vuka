package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const scriptOrdersGo = `package orders

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

// Orders is a live page whose script is this file, beside its markup.
type Orders struct {
	ui.Live
	orders []int
}

// Add adds an order.
func (p *Orders) Add() { p.orders = append(p.orders, len(p.orders)+1) }

func (p *Orders) label(n int) string { return "order " + strconv.Itoa(n) }
`

const scriptOrdersMarkup = `import "example.com/app/components"

<main>
  <ul id="orders">
    for _, n := range orders {
      <li key={n}>{label(n)}</li>
    }
  </ul>
  <button id="add" onClick={Add}>add</button>
  <components.Counter start={5} />
</main>
`

const scriptCounterVuka = `package components

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

// Counter's script is this .vuka file: its fields are tracked, its hooks
// decorators.
type Counter struct {
	ui.Live
	Start int
	n     int
}

@ui.Mount
func (c *Counter) mount() error {
	c.n = c.Start
	return nil
}

// Inc counts one more.
func (c *Counter) Inc() { c.n++ }

func (c *Counter) text() string { return strconv.Itoa(c.n) }
`

const scriptCounterMarkup = `<button id="inc" onClick={Inc}>{text()}</button>
`

// TestScriptFileE2E: a folder page and a component whose scripts are files
// beside their markup — orders/index.go, plain Go, and counter.vuka with a decorated hook — build and run as
// inline scripts do (vuka#356).
func TestScriptFileE2E(t *testing.T) {
	if chromeBinary() == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	root := assetsModule(t, map[string]string{"main.go": pagesMain,
		"components/counter.vuka":      scriptCounterVuka,
		"components/counter.vuka.html": scriptCounterMarkup,
		"pages/orders/index.go":        scriptOrdersGo,
		"pages/orders/index.vuka.html": scriptOrdersMarkup,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)

	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), assetsEnv+"=")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, func() bool { res, err := http.Get(base + "/orders"); return err == nil && res.Body.Close() == nil })

	ch := launchChrome(t)
	p := ch.open("about:blank", 900, 700)
	p.do("Page.enable", map[string]any{})
	p.goTo(base + "/orders")
	p.wait(`document.documentElement.getAttribute("data-vk-state") === "connected"`)
	p.click("#add")
	p.click("#add")
	p.wait(`document.querySelector("#orders").textContent === "order 1order 2"`)
	if got := p.text("#inc"); got != "5" {
		t.Errorf("the counter's mount: %q", got)
	}
	p.click("#inc")
	p.wait(`document.querySelector("#inc").textContent === "6"`)
	if t.Failed() {
		t.Logf("page:\n%v\napp log:\n%s", p.eval(`document.documentElement.outerHTML`), logs.String())
	}
}
