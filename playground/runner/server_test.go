package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake builds nothing and starts an in-process server: / says hello, /ws
// upgrades and echoes, /exit makes it exit.
type fake struct {
	mu      sync.Mutex
	started int
	failing bool
}

func (f *fake) Build(_ context.Context, dir string, log io.Writer) error {
	fmt.Fprintln(log, "compiling")
	if f.failing {
		fmt.Fprintln(log, `[{"code":"E0002","severity":"error","message":"bad","span":{"file":"main.vuka","line":3,"col":2}}]`)
		return fmt.Errorf("exit status 1")
	}
	return os.WriteFile(filepath.Join(dir, "app"), nil, 0o755)
}

func (f *fake) Start(_ context.Context, bin, data string, env []string, out io.Writer) (*Process, error) {
	f.mu.Lock()
	f.started++
	f.mu.Unlock()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Process{Network: "tcp", Addr: ln.Addr().String(), Done: make(chan struct{})}
	var once sync.Once
	stop := func() { once.Do(func() { ln.Close(); close(p.Done) }) }
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello %s host=%s", r.URL.Path, r.Host)
	})
	mux.HandleFunc("/go", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/there", http.StatusFound) })
	mux.HandleFunc("/exit", func(w http.ResponseWriter, r *http.Request) { go stop() })
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "no upgrade", 400)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		rw.Flush()
		for {
			line, err := rw.ReadString('\n')
			if err != nil {
				return
			}
			rw.WriteString("echo " + line)
			rw.Flush()
		}
	})
	go http.Serve(ln, mux)
	fmt.Fprintln(out, "listening")
	p.Stop = stop
	p.Exit = func() string { <-p.Done; return "exited" }
	return p, nil
}

func testServer(t *testing.T, sb Sandbox, edit func(*config)) (*server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module docsamples\n\ngo 1.27\n\nreplace github.com/vuka-lang/vuka/v2 => ../../..\n\ntool x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.sum"), nil, 0o644)
	tmpl, err := loadTemplate(filepath.Join(dir, "go.mod"), "/opt/vuka")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{allowOrigins: []string{"https://docs.example"}, runOrigin: "http://*.run.test", sessions: 2,
		idle: time.Minute, maxLife: time.Hour, buildTimeout: 10 * time.Second, ratePerMinute: 100, dir: t.TempDir()}
	if edit != nil {
		edit(&cfg)
	}
	s, err := newServer(cfg, sb, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.shutdown)
	return s, newTestHTTP(t, s)
}

func newTestHTTP(t *testing.T, s *server) *httptest.Server {
	hs := httptest.NewServer(s)
	t.Cleanup(hs.Close)
	return hs
}

var counter = map[string]string{"main.vuka": "package main\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc main() { web.New().Run(\"\") }\n"}

func post(t *testing.T, hs *httptest.Server, files map[string]string, hdr map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"files": files})
	req, _ := http.NewRequest("POST", hs.URL+"/run", strings.NewReader(string(b)))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode(t *testing.T, resp *http.Response) runResult {
	t.Helper()
	var r runResult
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

// get requests path of the session's host on the test server.
func get(t *testing.T, hs *httptest.Server, sessionURL, path string) (int, string, http.Header) {
	t.Helper()
	u, _ := url.Parse(sessionURL)
	req, _ := http.NewRequest("GET", hs.URL+path, nil)
	req.Host = u.Host
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestRunAndProxy(t *testing.T) {
	_, hs := testServer(t, &fake{}, nil)
	res := decode(t, post(t, hs, counter, nil))
	if res.Session == "" || res.URL != "http://"+res.Session+".run.test/" || res.Error != "" {
		t.Fatalf("result %+v", res)
	}
	if !strings.Contains(res.BuildLog, "compiling") {
		t.Errorf("build log %q", res.BuildLog)
	}
	code, body, _ := get(t, hs, res.URL, "/pets?x=1")
	if code != 200 || body != "hello /pets host="+res.Session+".run.test" {
		t.Errorf("proxied: %d %q", code, body)
	}
	code, body, _ = get(t, hs, "http://"+newID()+".run.test/", "/")
	if code != 404 || !strings.Contains(body, "stopped") {
		t.Errorf("unknown session: %d %q", code, body)
	}
	code, _, _ = get(t, hs, "http://api.test/", "/s/"+res.Session+"/")
	if code == 200 {
		t.Error("a program is served on the API's host")
	}
}

func TestWebsocket(t *testing.T) {
	_, hs := testServer(t, &fake{}, nil)
	res := decode(t, post(t, hs, counter, nil))
	u, _ := url.Parse(res.URL)
	conn, err := net.Dial("tcp", strings.TrimPrefix(hs.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", u.Host)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", resp, err)
	}
	fmt.Fprintf(conn, "inc\n")
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := br.ReadString('\n')
	if err != nil || line != "echo inc\n" {
		t.Fatalf("echo %q %v", line, err)
	}
}

func TestPathMode(t *testing.T) {
	_, hs := testServer(t, &fake{}, func(c *config) { c.runOrigin = "http://run.test" })
	res := decode(t, post(t, hs, counter, nil))
	if res.URL != "http://run.test/s/"+res.Session+"/" {
		t.Fatalf("url %q", res.URL)
	}
	code, body, h := get(t, hs, res.URL, "/s/"+res.Session+"/pets")
	if code != 200 || !strings.HasPrefix(body, "hello /pets") {
		t.Errorf("proxied: %d %q", code, body)
	}
	if !strings.Contains(h.Get("Set-Cookie"), sessionCookie+"="+res.Session) {
		t.Errorf("cookie %q", h.Get("Set-Cookie"))
	}
	if code, _, h := get(t, hs, res.URL, "/s/"+res.Session+"/go"); code != 302 || h.Get("Location") != "/s/"+res.Session+"/there" {
		t.Errorf("redirect %d %q", code, h.Get("Location"))
	}
}

func TestBuildFailure(t *testing.T) {
	_, hs := testServer(t, &fake{failing: true}, nil)
	res := decode(t, post(t, hs, counter, nil))
	if res.Session != "" || len(res.Diagnostics) != 1 || res.Diagnostics[0].Span.File != "main.vuka" || res.Error == "" {
		t.Fatalf("result %+v", res)
	}
	if strings.Contains(res.BuildLog, "E0002") {
		t.Errorf("the diagnostics' JSON is in the log: %q", res.BuildLog)
	}
}

func TestBadImport(t *testing.T) {
	f := &fake{}
	_, hs := testServer(t, f, nil)
	files := map[string]string{
		"main.vuka":             "package main\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/google/uuid\"\n)\n\nfunc main() { fmt.Println(uuid.New()) }\n",
		"pages/index.vuka.html": "<script lang=\"go\">\npackage pages\n\nimport \"example.com/other\"\n</script>\n<p>x</p>\n",
	}
	res := decode(t, post(t, hs, files, nil))
	got := []string{}
	for _, d := range res.Diagnostics {
		got = append(got, fmt.Sprintf("%s:%d:%d %s", d.Span.File, d.Span.Line, d.Span.Col, d.Message))
	}
	want := []string{
		"main.vuka:6:2 cannot import github.com/google/uuid: " + knownMessage,
		"pages/index.vuka.html:4:8 cannot import example.com/other: " + knownMessage,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if f.started != 0 {
		t.Error("a project with a bad import ran")
	}
}

func TestLimits(t *testing.T) {
	_, hs := testServer(t, &fake{}, func(c *config) { c.ratePerMinute = 3 })
	for _, files := range []map[string]string{
		{"../x.go": "package x"},
		{"package.json": "{}"},
		{"main.go": strings.Repeat("x", maxBytes+1)},
	} {
		resp := post(t, hs, files, nil)
		if resp.StatusCode == 200 {
			if r := decode(t, resp); len(r.Diagnostics) == 0 {
				t.Errorf("%v was taken", keys(files))
			}
		}
	}
	if resp := post(t, hs, counter, nil); resp.StatusCode != 429 {
		t.Errorf("the fourth run a minute: %d", resp.StatusCode)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBusy(t *testing.T) {
	_, hs := testServer(t, &fake{}, nil)
	for i := 0; i < 2; i++ {
		if r := decode(t, post(t, hs, counter, nil)); r.Session == "" {
			t.Fatalf("run %d: %+v", i, r)
		}
	}
	resp := post(t, hs, counter, nil)
	if resp.StatusCode != 503 {
		t.Fatalf("a third session: %d", resp.StatusCode)
	}
	var e map[string]string
	json.NewDecoder(resp.Body).Decode(&e)
	if !strings.Contains(e["error"], "busy") {
		t.Errorf("error %q", e["error"])
	}
}

func TestCORS(t *testing.T) {
	_, hs := testServer(t, &fake{}, nil)
	if resp := post(t, hs, counter, map[string]string{"Origin": "https://evil.example"}); resp.StatusCode != 403 {
		t.Errorf("another origin: %d", resp.StatusCode)
	}
	resp := post(t, hs, counter, map[string]string{"Origin": "https://docs.example"})
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != "https://docs.example" {
		t.Errorf("the docs: %d %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestDeleteAndExit(t *testing.T) {
	s, hs := testServer(t, &fake{}, nil)
	res := decode(t, post(t, hs, counter, nil))
	req, _ := http.NewRequest("DELETE", hs.URL+"/s/"+res.Session, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("delete: %v %v", resp, err)
	}
	if s.lookup(res.Session) != nil {
		t.Error("the session outlived DELETE")
	}
	res = decode(t, post(t, hs, counter, nil))
	get(t, hs, res.URL, "/exit")
	waitGone(t, s, res.Session, 5*time.Second)
}

func waitGone(t *testing.T, s *server, id string, d time.Duration) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if s.lookup(id) == nil {
			return
		}
	}
	t.Fatalf("session %s still runs after %s", id, d)
}

func TestIdleAndMaxLife(t *testing.T) {
	s, hs := testServer(t, &fake{}, func(c *config) { c.idle = 200 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.loop(ctx)
	res := decode(t, post(t, hs, counter, nil))
	for i := 0; i < 4; i++ {
		time.Sleep(100 * time.Millisecond)
		get(t, hs, res.URL, "/")
	}
	if s.lookup(res.Session) == nil {
		t.Fatal("a session in use was ended as idle")
	}
	waitGone(t, s, res.Session, 3*time.Second)

	s2, hs2 := testServer(t, &fake{}, func(c *config) { c.maxLife = 300 * time.Millisecond })
	go s2.loop(ctx)
	res = decode(t, post(t, hs2, counter, nil))
	waitGone(t, s2, res.Session, 3*time.Second)
}

func TestStream(t *testing.T) {
	_, hs := testServer(t, &fake{}, nil)
	resp := post(t, hs, counter, map[string]string{"Accept": "text/event-stream"})
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	var names []string
	var result runResult
	for sc.Scan() {
		if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			names = append(names, name)
			sc.Scan()
			if name == "result" {
				json.Unmarshal([]byte(strings.TrimPrefix(sc.Text(), "data: ")), &result)
				break
			}
		}
	}
	if strings.Join(names, ",") != "log,result" || result.Session == "" {
		t.Fatalf("events %v, result %+v", names, result)
	}
}

func TestTemplate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("// x\nmodule docsamples\n\ngo 1.27\n\nreplace github.com/vuka-lang/vuka/v2 => ../../..\n\nrequire github.com/vuka-lang/ui/v2 v2.6.0\n\ntool github.com/vuka-lang/orm/v2/cmd/orm\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte("sum\n"), 0o644)
	tm, err := loadTemplate(filepath.Join(dir, "go.mod"), "/opt/vuka")
	if err != nil {
		t.Fatal(err)
	}
	want := "module example.com/play\n\ngo 1.27\n\nreplace github.com/vuka-lang/vuka/v2 => /opt/vuka\n\nrequire github.com/vuka-lang/ui/v2 v2.6.0\n\n"
	if string(tm.gomod) != want {
		t.Errorf("go.mod\n%s", tm.gomod)
	}
}
