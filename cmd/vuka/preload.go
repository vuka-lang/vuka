package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// A rebuild's link reads every package archive the program is made of, and
// the first one after an idle pause finds many evicted from the page cache.
// preloader reads the last good build's archives, and the compiler and
// linker, back in while the change is transpiled.
type preloader struct {
	mu     sync.Mutex
	env    []string
	dir    string
	tools  []string // nil until asked; empty when go env can't say
	files  []string
	cancel context.CancelFunc
}

const preloadWorkers = 4

// set records what the build just made links; exports is go list's
// import path → archive.
func (p *preloader) set(exports map[string]string) {
	if p == nil {
		return
	}
	tools := p.toolFiles()
	files := make([]string, 0, len(exports))
	for _, f := range exports {
		files = append(files, f)
	}
	sort.Strings(files)
	p.mu.Lock()
	p.files = slices.Concat(tools, files)
	p.mu.Unlock()
}

func (p *preloader) toolFiles() []string {
	p.mu.Lock()
	tools := p.tools
	p.mu.Unlock()
	if tools != nil {
		return tools
	}
	tools = []string{}
	c := exec.Command("go", "env", "GOTOOLDIR")
	c.Dir, c.Env = p.dir, p.env
	if out, err := c.Output(); err == nil {
		if d := strings.TrimSpace(string(out)); filepath.IsAbs(d) {
			tools = []string{exe(filepath.Join(d, "compile")), exe(filepath.Join(d, "link"))}
		}
	}
	p.mu.Lock()
	p.tools = tools
	p.mu.Unlock()
	return tools
}

// start reads the recorded files in the background, unless a read runs.
func (p *preloader) start() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil || len(p.files) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	files := p.files
	go func() {
		t0 := time.Now()
		n := warmFiles(ctx, files, preloadWorkers)
		traceStep("preload read %d MB of %d files %v", n>>20, len(files), time.Since(t0))
		p.mu.Lock()
		if ctx.Err() == nil {
			p.cancel = nil
		}
		p.mu.Unlock()
		cancel()
	}()
}

// stop abandons a read that runs: the build it was for has linked.
func (p *preloader) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
}

// warmFiles reads files through, in order, with workers at a time, and
// returns the bytes read.
func warmFiles(ctx context.Context, files []string, workers int) int64 {
	next := make(chan string)
	var total int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(workers, len(files)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1<<20)
			var n int64
			for f := range next {
				n += warmFile(ctx, f, buf)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
feed:
	for _, f := range files {
		select {
		case next <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	return total
}

func warmFile(ctx context.Context, name string, buf []byte) int64 {
	f, err := os.Open(name)
	if err != nil {
		return 0
	}
	defer f.Close()
	var n int64
	for ctx.Err() == nil {
		m, err := f.Read(buf)
		n += int64(m)
		if err != nil {
			break
		}
	}
	return n
}
