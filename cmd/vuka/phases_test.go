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
	var out lockedBuffer
	p := &phases{w: &out, after: 20 * time.Millisecond}
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
