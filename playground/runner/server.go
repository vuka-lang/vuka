package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type server struct {
	cfg   config
	sb    Sandbox
	tmpl  *template
	slots chan struct{}
	rate  *limiter

	wildcard bool   // sessions are hosts: <id>.<runHost>
	runHost  string // the run origin's host, without "*."
	runBase  *url.URL

	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
}

type session struct {
	id      string
	dir     string
	proc    *Process
	created time.Time
	proxy   *httputil.ReverseProxy
	tr      *http.Transport
	out     *ring

	mu     sync.Mutex
	last   time.Time
	active int
}

func newServer(cfg config, sb Sandbox, tmpl *template) (*server, error) {
	base, err := url.Parse(cfg.runOrigin)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("RUNNER_RUN_ORIGIN %q isn't an origin", cfg.runOrigin)
	}
	s := &server{cfg: cfg, sb: sb, tmpl: tmpl, slots: make(chan struct{}, cfg.sessions),
		rate: newLimiter(cfg.ratePerMinute), runBase: base, sessions: map[string]*session{}}
	s.runHost = base.Host
	if h, ok := strings.CutPrefix(base.Host, "*."); ok {
		s.wildcard, s.runHost = true, h
	}
	if err := os.MkdirAll(cfg.dir, 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

// sweep ends sessions idle for cfg.idle or older than cfg.maxLife.
func (s *server) sweep(now time.Time) {
	s.mu.Lock()
	var dead []*session
	for _, ss := range s.sessions {
		ss.mu.Lock()
		idle := ss.active == 0 && now.Sub(ss.last) > s.cfg.idle
		ss.mu.Unlock()
		if idle || now.Sub(ss.created) > s.cfg.maxLife {
			dead = append(dead, ss)
		}
	}
	s.mu.Unlock()
	for _, ss := range dead {
		s.end(ss)
	}
	s.rate.trim(now)
}

func (s *server) loop(ctx context.Context) {
	tick := min(s.cfg.idle/4, time.Second)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.sweep(now)
		}
	}
}

// shutdown ends every session.
func (s *server) shutdown() {
	s.mu.Lock()
	s.closed = true
	all := make([]*session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		all = append(all, ss)
	}
	s.mu.Unlock()
	for _, ss := range all {
		s.end(ss)
	}
}

func (s *server) end(ss *session) {
	s.mu.Lock()
	_, ok := s.sessions[ss.id]
	delete(s.sessions, ss.id)
	s.mu.Unlock()
	if !ok {
		return
	}
	ss.proc.Stop()
	ss.tr.CloseIdleConnections()
	os.RemoveAll(ss.dir)
	<-s.slots
}

func (s *server) lookup(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(r.Host)
	if s.wildcard {
		if id, ok := strings.CutSuffix(host, "."+s.runHost); ok && validID(id) {
			s.serveSession(w, r, id, "")
			return
		}
	} else if host == strings.ToLower(s.runHost) && !(r.Method == http.MethodDelete || r.Method == http.MethodOptions) {
		s.servePath(w, r)
		return
	}
	if s.cfg.apiHost != "" && host != strings.ToLower(s.cfg.apiHost) {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.URL.Path == "/run":
		s.serveRun(w, r)
	case strings.HasPrefix(r.URL.Path, "/s/"):
		s.serveDelete(w, r)
	case r.URL.Path == "/healthz":
		s.mu.Lock()
		n := len(s.sessions)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]int{"sessions": n, "cap": s.cfg.sessions})
	default:
		http.NotFound(w, r)
	}
}

var idRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

func validID(id string) bool { return idRE.MatchString(id) }

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// cors answers for the docs origin; false when the request's origin isn't
// one the runner serves.
func (s *server) cors(w http.ResponseWriter, r *http.Request, methods string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, o := range s.cfg.allowOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", methods)
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			return true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "the runner doesn't serve " + origin})
	return false
}

func (s *server) serveDelete(w http.ResponseWriter, r *http.Request) {
	if !s.cors(w, r, "DELETE") {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "DELETE a session", http.StatusMethodNotAllowed)
		return
	}
	ss := s.lookup(strings.Trim(strings.TrimPrefix(r.URL.Path, "/s/"), "/"))
	if ss == nil {
		http.NotFound(w, r)
		return
	}
	s.end(ss)
	w.WriteHeader(http.StatusNoContent)
}

type runResult struct {
	Session     string       `json:"session,omitempty"`
	URL         string       `json:"url,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	BuildLog    string       `json:"buildLog"`
	Output      string       `json:"output,omitempty"`
	Error       string       `json:"error,omitempty"`
}

func (s *server) clientIP(r *http.Request) string {
	if s.cfg.trustProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			ip, _, _ := strings.Cut(f, ",")
			return strings.TrimSpace(ip)
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (s *server) serveRun(w http.ResponseWriter, r *http.Request) {
	if !s.cors(w, r, "POST") {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST a project", http.StatusMethodNotAllowed)
		return
	}
	if !s.rate.allow(s.clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many runs from your address: try again in a minute"})
		return
	}
	var req struct {
		Files map[string]string `json:"files"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes*2)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body isn't a project: " + err.Error()})
		return
	}
	ds, err := checkFiles(req.Files)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.take() {
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the runner is busy: try again in a minute"})
		return
	}
	var ev *events
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		ev = newEvents(w)
	}
	res, ss := s.run(r.Context(), req.Files, ds, ev)
	if ev == nil {
		writeJSON(w, http.StatusOK, res)
		return
	}
	ev.send("result", res)
	if ss == nil {
		return
	}
	// The program's output keeps coming until it exits or the page goes.
	sub := ss.out.subscribe()
	defer ss.out.unsubscribe(sub)
	for {
		select {
		case line := <-sub:
			ev.send("output", line)
		case <-ss.proc.Done:
			for {
				select {
				case line := <-sub:
					ev.send("output", line)
					continue
				default:
				}
				break
			}
			ev.send("exit", map[string]string{"status": ss.proc.Exit()})
			return
		case <-r.Context().Done():
			return
		}
	}
}

// take takes one of the sessions' slots; false when none is free.
func (s *server) take() bool {
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// run builds files and starts the program, in the slot take took; a nil
// session when nothing runs (the result says why), the slot given back.
func (s *server) run(ctx context.Context, files map[string]string, ds []Diagnostic, ev *events) (res runResult, ss *session) {
	defer func() {
		if ss == nil {
			<-s.slots
		}
	}()
	res = runResult{Diagnostics: []Diagnostic{}}
	if len(ds) > 0 {
		res.Diagnostics, res.Error = ds, "the project imports what the runner hasn't"
		return res, nil
	}
	id := newID()
	dir := filepath.Join(s.cfg.dir, id)
	src, data := filepath.Join(dir, "src"), filepath.Join(dir, "data")
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(dir)
		}
	}()
	for _, d := range []string{src, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			res.Error = err.Error()
			return res, nil
		}
	}
	if err := s.tmpl.write(src, files); err != nil {
		res.Error = err.Error()
		return res, nil
	}
	var buf bytes.Buffer
	lw := &lineWriter{fn: func(line string) {
		buf.WriteString(line)
		if ev != nil {
			if t := strings.TrimSpace(line); t != "" && !(t[0] == '[' && json.Valid([]byte(t))) {
				ev.send("log", strings.TrimRight(line, "\n"))
			}
		}
	}}
	bctx, cancel := context.WithTimeout(ctx, s.cfg.buildTimeout)
	start := time.Now()
	err := s.sb.Build(bctx, src, lw)
	cancel()
	lw.flush()
	if ds := buildDiagnostics(buf.Bytes()); ds != nil {
		res.Diagnostics = ds
	}
	res.BuildLog = plainLog(buf.Bytes())
	if err != nil {
		res.Error = "the build failed"
		if errors.Is(bctx.Err(), context.DeadlineExceeded) {
			res.Error = fmt.Sprintf("the build took longer than %s", s.cfg.buildTimeout)
		}
		return res, nil
	}
	res.BuildLog += fmt.Sprintf("built in %.1fs\n", time.Since(start).Seconds())
	out := newRing(64 << 10)
	proc, err := s.sb.Start(context.Background(), filepath.Join(src, "app"), data, nil, out)
	if err != nil {
		res.Error = "the program didn't start: " + err.Error()
		return res, nil
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = ready(rctx, proc)
	cancel()
	if err != nil {
		proc.Stop()
		res.Output = out.String()
		if !strings.Contains(err.Error(), "exited") || proc.Exit() != "exited" {
			res.Error = err.Error()
		}
		return res, nil
	}
	ss = &session{id: id, dir: dir, proc: proc, created: time.Now(), last: time.Now(), out: out}
	ss.tr = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, proc.Network, proc.Addr)
		},
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConnsPerHost:   8,
	}
	ss.proxy = s.newProxy(ss)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		proc.Stop()
		res.Error = "the runner is stopping"
		return res, nil
	}
	s.sessions[id] = ss
	s.mu.Unlock()
	keep = true
	go func() {
		<-proc.Done
		s.end(ss)
	}()
	res.Session, res.URL = id, s.sessionURL(id)
	return res, ss
}

func (s *server) sessionURL(id string) string {
	u := *s.runBase
	if s.wildcard {
		u.Host = id + "." + s.runHost
		u.Path = "/"
	} else {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/s/" + id + "/"
	}
	return u.String()
}

const sessionCookie = "vkrun"

// servePath serves the run origin's /s/<id>/… by session. A path without
// the prefix (an absolute link the program wrote) goes to the session its
// page's Referer or the cookie names.
func (s *server) servePath(w http.ResponseWriter, r *http.Request) {
	if rest, ok := strings.CutPrefix(r.URL.Path, "/s/"); ok {
		id, tail, _ := strings.Cut(rest, "/")
		if validID(id) {
			if tail == "" && !strings.HasSuffix(r.URL.Path, "/") {
				http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true,
				Secure: s.runBase.Scheme == "https", SameSite: sameSite(s.runBase.Scheme)})
			s.serveSession(w, r, id, "/s/"+id)
			return
		}
	}
	if ref, err := url.Parse(r.Referer()); err == nil && strings.EqualFold(ref.Host, r.Host) {
		if rest, ok := strings.CutPrefix(ref.Path, "/s/"); ok {
			if id, _, _ := strings.Cut(rest, "/"); validID(id) {
				s.serveSession(w, r, id, "")
				return
			}
		}
	}
	if c, err := r.Cookie(sessionCookie); err == nil && validID(c.Value) {
		s.serveSession(w, r, c.Value, "")
		return
	}
	http.NotFound(w, r)
}

func sameSite(scheme string) http.SameSite {
	if scheme == "https" {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

type prefixKey struct{}

func (s *server) serveSession(w http.ResponseWriter, r *http.Request, id, prefix string) {
	ss := s.lookup(id)
	if ss == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "<!doctype html><title>Stopped</title><p style=\"font-family:system-ui;padding:2rem;color:#666\">This program has stopped. Run it again from the playground.</p>")
		return
	}
	ss.mu.Lock()
	ss.active++
	ss.last = time.Now()
	ss.mu.Unlock()
	defer func() {
		ss.mu.Lock()
		ss.active--
		ss.last = time.Now()
		ss.mu.Unlock()
	}()
	if prefix != "" {
		r = r.WithContext(context.WithValue(r.Context(), prefixKey{}, prefix))
	}
	ss.proxy.ServeHTTP(w, r)
}

func (s *server) newProxy(ss *session) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: ss.tr,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: "play"})
			pr.Out.Host = pr.In.Host
			if prefix, _ := pr.In.Context().Value(prefixKey{}).(string); prefix != "" {
				pr.Out.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(pr.In.URL.Path, prefix), "/")
				pr.Out.URL.RawPath = ""
			}
			pr.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			prefix, _ := resp.Request.Context().Value(prefixKey{}).(string)
			if loc := resp.Header.Get("Location"); prefix != "" && strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
				resp.Header.Set("Location", prefix+loc)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "the program didn't answer: "+err.Error(), http.StatusBadGateway)
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// events writes server-sent events.
type events struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newEvents(w http.ResponseWriter) *events {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	e := &events{w: w, rc: http.NewResponseController(w)}
	e.rc.Flush()
	return e
}

func (e *events) send(name string, v any) {
	b, _ := json.Marshal(v)
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", name, b)
	e.rc.Flush()
}

// lineWriter calls fn with each line written to it.
type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(string)
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.fn(string(l.buf[:i+1]))
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

func (l *lineWriter) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.fn(string(l.buf) + "\n")
		l.buf = nil
	}
}

// ring keeps the last of a program's output and hands new lines to its
// subscribers.
type ring struct {
	mu   sync.Mutex
	max  int
	buf  []byte
	part []byte
	subs map[chan string]bool
}

func newRing(max int) *ring { return &ring{max: max, subs: map[chan string]bool{}} }

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	r.part = append(r.part, p...)
	for {
		i := bytes.IndexByte(r.part, '\n')
		if i < 0 {
			break
		}
		line := string(r.part[:i])
		r.part = r.part[i+1:]
		for c := range r.subs {
			select {
			case c <- line:
			default:
			}
		}
	}
	if len(r.part) > 4096 {
		r.part = r.part[:0]
	}
	return len(p), nil
}

func (r *ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

func (r *ring) subscribe() chan string {
	c := make(chan string, 256)
	r.mu.Lock()
	r.subs[c] = true
	r.mu.Unlock()
	return c
}

func (r *ring) unsubscribe(c chan string) {
	r.mu.Lock()
	delete(r.subs, c)
	r.mu.Unlock()
}

// limiter allows each address n runs a minute.
type limiter struct {
	mu   sync.Mutex
	n    float64
	seen map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(n int) *limiter { return &limiter{n: float64(n), seen: map[string]*bucket{}} }

func (l *limiter) allow(ip string, now time.Time) bool {
	if l.n <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.seen[ip]
	if b == nil {
		b = &bucket{tokens: l.n, last: now}
		l.seen[ip] = b
	}
	b.tokens = min(l.n, b.tokens+now.Sub(b.last).Minutes()*l.n)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *limiter) trim(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, b := range l.seen {
		if now.Sub(b.last) > 2*time.Minute {
			delete(l.seen, ip)
		}
	}
}
