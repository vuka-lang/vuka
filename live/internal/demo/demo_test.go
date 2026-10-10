package demo_test

import (
	"os"
	"os/exec"
	"testing"
)

// TestGenerated checks demo_vuka.go is what the compiler makes of demo.vuka.
func TestGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command")
	}
	cmd := exec.Command("go", "run", "../../../cmd/vuka", "gen", "-inplace", "-check", ".")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("demo_vuka.go is stale (go run ./cmd/vuka gen -inplace ./live/internal/demo):\n%s%v", out, err)
	}
}
