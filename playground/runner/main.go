// Command runner builds and runs playground projects, each in a sandbox,
// and serves each running program from its own origin.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type config struct {
	addr          string
	sandbox       string
	allowOrigins  []string
	apiHost       string
	runOrigin     string
	sessions      int
	idle, maxLife time.Duration
	buildTimeout  time.Duration
	ratePerMinute int
	trustProxy    bool
	dir           string

	gomod, vukaSrc, vuka, shim, nsjail  string
	goroot, gomodcache, gocache, cgroup string
	buildMemory, runMemory              int64
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func duration(k string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return d
	}
	return def
}

func number(k string, def int64) int64 {
	if n, err := strconv.ParseInt(os.Getenv(k), 10, 64); err == nil {
		return n
	}
	return def
}

func loadConfig() config {
	sandbox := "nsjail"
	if runtime.GOOS != "linux" {
		sandbox = "local"
	}
	addr := getenv("RUNNER_ADDR", ":8090")
	_, port, _ := net.SplitHostPort(addr)
	exe, _ := os.Executable()
	gomodcache := os.Getenv("GOMODCACHE")
	if gomodcache == "" {
		gomodcache = filepath.Join(gopath(), "pkg", "mod")
	}
	var origins []string
	for _, o := range strings.Split(getenv("RUNNER_ALLOW_ORIGIN", "http://localhost:5173"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return config{
		addr:          addr,
		sandbox:       getenv("RUNNER_SANDBOX", sandbox),
		allowOrigins:  origins,
		apiHost:       os.Getenv("RUNNER_API_HOST"),
		runOrigin:     getenv("RUNNER_RUN_ORIGIN", "http://*.run.localhost:"+port),
		sessions:      int(number("RUNNER_SESSIONS", 20)),
		idle:          duration("RUNNER_IDLE", 2*time.Minute),
		maxLife:       duration("RUNNER_MAX_LIFE", 10*time.Minute),
		buildTimeout:  duration("RUNNER_BUILD_TIMEOUT", 60*time.Second),
		ratePerMinute: int(number("RUNNER_RATE", 10)),
		trustProxy:    os.Getenv("RUNNER_TRUST_PROXY") == "1",
		dir:           getenv("RUNNER_DIR", filepath.Join(os.TempDir(), "vuka-runner")),
		gomod:         getenv("RUNNER_GOMOD", "../../docs/testdata/gomode/go.mod"),
		vukaSrc:       getenv("RUNNER_VUKA_SRC", absPath("../..")),
		vuka:          getenv("RUNNER_VUKA", "vuka"),
		shim:          getenv("RUNNER_SHIM", exe),
		nsjail:        getenv("RUNNER_NSJAIL", "/usr/local/bin/nsjail"),
		goroot:        getenv("RUNNER_GOROOT", runtime.GOROOT()),
		gomodcache:    getenv("RUNNER_GOMODCACHE", gomodcache),
		gocache:       getenv("RUNNER_GOCACHE", "/opt/gocache"),
		cgroup:        getenv("RUNNER_CGROUP", "/sys/fs/cgroup"),
		buildMemory:   number("RUNNER_BUILD_MEMORY", 1<<30),
		runMemory:     number("RUNNER_RUN_MEMORY", 256<<20),
	}
}

func gopath() string {
	if v := os.Getenv("GOPATH"); v != "" {
		return strings.Split(v, string(filepath.ListSeparator))[0]
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "go")
}

func absPath(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func newSandbox(c config) (Sandbox, error) {
	switch c.sandbox {
	case "nsjail":
		return newNsjail(c)
	case "local":
		return newLocal(c.vuka)
	}
	return nil, fmt.Errorf("RUNNER_SANDBOX %q: nsjail or local", c.sandbox)
}

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "shim":
			if len(os.Args) != 5 {
				log.Fatal("usage: runner shim <socket> <port> <program>")
			}
			os.Exit(shim(os.Args[2], os.Args[3], os.Args[4]))
		case "warm":
			if err := warm(loadConfig(), os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
	cfg := loadConfig()
	var err error
	if cfg.vuka, err = exec.LookPath(cfg.vuka); err != nil {
		log.Fatal(err)
	}
	sb, err := newSandbox(cfg)
	if err != nil {
		log.Fatal(err)
	}
	tmpl, err := loadTemplate(cfg.gomod, cfg.vukaSrc)
	if err != nil {
		log.Fatal(err)
	}
	s, err := newServer(cfg, sb, tmpl)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.loop(ctx)
	srv := &http.Server{Addr: cfg.addr, Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		s.shutdown()
		sctx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		srv.Shutdown(sctx)
	}()
	log.Printf("runner: %s sandbox, API on %s, programs at %s", cfg.sandbox, cfg.addr, cfg.runOrigin)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
