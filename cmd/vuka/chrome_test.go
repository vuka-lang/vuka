package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// chrome is a headless Chrome driven over the DevTools protocol through a
// pipe (--remote-debugging-pipe): no WebSocket, no dependency. Tests skip
// without Chrome; VUKA_CHROME names the binary.
type chrome struct {
	t   *testing.T
	cmd *exec.Cmd
	in  *os.File      // Chrome reads commands here (its fd 3)
	out *bufio.Reader // and writes replies and events here (its fd 4)
	id  int
}

func chromeBinary() string {
	if b := os.Getenv("VUKA_CHROME"); b != "" {
		return b
	}
	for _, b := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
		if _, err := os.Stat(b); err == nil {
			return b
		}
	}
	for _, b := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(b); err == nil {
			return p
		}
	}
	return ""
}

func launchChrome(t *testing.T) *chrome {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Chrome is driven through fds 3 and 4 (--remote-debugging-pipe), which Windows doesn't pass on")
	}
	bin := chromeBinary()
	if bin == "" {
		needs(t, "no Chrome (set VUKA_CHROME)")
	}
	cmdR, cmdW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	resR, resW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--headless=new", "--remote-debugging-pipe", "--user-data-dir="+t.TempDir(),
		"--no-first-run", "--no-default-browser-check", "--disable-gpu", "--hide-scrollbars", "--disable-extensions",
		"--password-store=basic", "--use-mock-keychain", "about:blank")
	cmd.ExtraFiles = []*os.File{cmdR, resW}
	ownGroup(cmd) // its helpers with it, to end them all
	if err := cmd.Start(); err != nil {
		needs(t, "starting Chrome: %v", err)
	}
	cmdR.Close()
	resW.Close()
	c := &chrome{t: t, cmd: cmd, in: cmdW, out: bufio.NewReader(resR)}
	t.Cleanup(func() {
		c.in.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		// Its helpers may outlive it a moment, writing to the profile the
		// test's temporary directory removes next: they end first.
		endGroup(cmd.Process.Pid)
	})
	return c
}

type cdpMessage struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// call sends a command (to a page's session when session isn't empty) and
// returns its result, skipping the events that come before it.
func (c *chrome) call(session, method string, params any, result any) error {
	c.id++
	msg := map[string]any{"id": c.id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if session != "" {
		msg["sessionId"] = session
	}
	b, _ := json.Marshal(msg)
	if _, err := c.in.Write(append(b, 0)); err != nil {
		return err
	}
	for {
		raw, err := c.out.ReadBytes(0)
		if err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		var m cdpMessage
		if json.Unmarshal(raw[:len(raw)-1], &m) != nil || m.ID != c.id {
			continue
		}
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	}
}

// tab is a page of the browser.
type tab struct {
	c       *chrome
	session string
}

// open is a new tab at url, width × height.
func (c *chrome) open(url string, width, height int) *tab {
	c.t.Helper()
	var target struct{ TargetID string }
	if err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"}, &target); err != nil {
		c.t.Fatal(err)
	}
	var att struct{ SessionID string }
	if err := c.call("", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &att); err != nil {
		c.t.Fatal(err)
	}
	p := &tab{c, att.SessionID}
	p.do("Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 2, "mobile": false})
	p.goTo(url)
	return p
}

func (p *tab) do(method string, params any) {
	p.c.t.Helper()
	if err := p.c.call(p.session, method, params, nil); err != nil {
		p.c.t.Fatal(err)
	}
}

// goTo loads url and waits for the document.
func (p *tab) goTo(url string) {
	p.c.t.Helper()
	p.do("Page.navigate", map[string]any{"url": url})
	time.Sleep(100 * time.Millisecond)
	p.wait(`document.readyState === "complete"`)
}

// eval is the value of a JavaScript expression (awaited when a promise).
func (p *tab) eval(expr string) any {
	p.c.t.Helper()
	v, err := p.tryEval(expr)
	if err != nil {
		p.c.t.Fatal(err)
	}
	return v
}

// tryEval is eval returning the failure instead of ending the test.
func (p *tab) tryEval(expr string) (any, error) {
	var r struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := p.c.call(p.session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		return nil, fmt.Errorf("%s: %s %s", expr, r.ExceptionDetails.Text, r.ExceptionDetails.Exception.Description)
	}
	return r.Result.Value, nil
}

// wait polls a JavaScript condition for up to 10 seconds. A navigation can
// destroy the execution context between polls; its evaluation failure means
// "not yet", like a false condition.
func (p *tab) wait(cond string) {
	p.c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v, err := p.tryEval("!!(" + cond + ")")
		if b, _ := v.(bool); err == nil && b {
			return
		}
		if time.Now().After(deadline) {
			href, _ := p.tryEval("location.href")
			p.c.t.Fatalf("timed out waiting for %s on %v", cond, href)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// fill sets a field's value as typing would.
func (p *tab) fill(selector, value string) {
	p.c.t.Helper()
	sel, _ := json.Marshal(selector)
	val, _ := json.Marshal(value)
	p.eval(fmt.Sprintf(`(() => { const el = document.querySelector(%s); el.focus(); el.value = %s; el.dispatchEvent(new Event("input", {bubbles: true})); })()`, sel, val))
}

// click clicks an element.
func (p *tab) click(selector string) {
	p.c.t.Helper()
	sel, _ := json.Marshal(selector)
	p.eval(fmt.Sprintf(`document.querySelector(%s).click()`, sel))
}

// text is an element's text, "" when there is none.
func (p *tab) text(selector string) string {
	sel, _ := json.Marshal(selector)
	s, _ := p.eval(fmt.Sprintf(`(document.querySelector(%s) || {}).textContent || ""`, sel)).(string)
	return strings.TrimSpace(s)
}

// dark sets prefers-color-scheme.
func (p *tab) dark(on bool) {
	scheme := "light"
	if on {
		scheme = "dark"
	}
	p.do("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": scheme}}})
}

// screenshot writes a PNG of the viewport to path.
// screenshotOf saves a picture of the element the JS expression el is,
// with pad pixels around it, at twice the page's scale.
func (p *tab) screenshotOf(el, path string, pad float64) {
	p.c.t.Helper()
	box, ok := p.eval(`(() => { const r = (` + el + `).getBoundingClientRect(); return [r.x + scrollX, r.y + scrollY, r.width, r.height] })()`).([]any)
	if !ok || len(box) != 4 {
		p.c.t.Fatalf("no box for %s: %v", el, box)
	}
	n := func(i int) float64 { f, _ := box[i].(float64); return f }
	x, y := max(0, n(0)-pad), max(0, n(1)-pad)
	clip := map[string]any{"x": x, "y": y, "width": n(0) + n(2) + pad - x, "height": n(1) + n(3) + pad - y, "scale": 2}
	var r struct{ Data string }
	if err := p.c.call(p.session, "Page.captureScreenshot", map[string]any{"format": "png", "clip": clip, "captureBeyondViewport": true}, &r); err != nil {
		p.c.t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		p.c.t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		p.c.t.Fatal(err)
	}
}

func (p *tab) screenshot(path string) {
	p.c.t.Helper()
	var r struct{ Data string }
	if err := p.c.call(p.session, "Page.captureScreenshot", map[string]any{"format": "png"}, &r); err != nil {
		p.c.t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		p.c.t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		p.c.t.Fatal(err)
	}
}
