package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A step that takes longer than the threshold is named while it runs and
// timed when it ends; a quick one says nothing.
func TestPhasesNameSlowSteps(t *testing.T) {
	t.Parallel()
	var out lockedBuffer
	p := &phases{w: &out, after: 20 * time.Millisecond, every: time.Hour}
	p.step("scanning the module")
	p.step("transpiling 3 packages")
	time.Sleep(80 * time.Millisecond)
	p.step("compiling")
	p.done()
	got := out.String()
	if strings.Contains(got, "scanning") || strings.Contains(got, "compiling") {
		t.Errorf("quick steps named:\n%s", got)
	}
	if !strings.Contains(got, "● transpiling 3 packages… (") || !strings.Contains(got, "● transpiling 3 packages took ") {
		t.Errorf("the slow step isn't named and timed:\n%s", got)
	}
}

// A step still running is named again every so often, and timed once.
func TestPhasesNameLongStepsAgain(t *testing.T) {
	t.Parallel()
	var out lockedBuffer
	p := &phases{w: &out, after: 20 * time.Millisecond, every: 30 * time.Millisecond}
	p.step("transpiling 203 packages")
	time.Sleep(150 * time.Millisecond)
	p.step("compiling")
	p.done()
	time.Sleep(60 * time.Millisecond) // no timer left to name a step
	got := out.String()
	if n := strings.Count(got, "● transpiling 203 packages… ("); n < 3 {
		t.Errorf("named %d times while it ran, want 3 or more:\n%s", n, got)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if strings.Count(got, " took ") != 1 || !strings.HasPrefix(lines[len(lines)-1], "● transpiling 203 packages took ") {
		t.Errorf("not timed once, at the end:\n%s", got)
	}
	if strings.Contains(got, "compiling") {
		t.Errorf("a quick step named:\n%s", got)
	}
}
