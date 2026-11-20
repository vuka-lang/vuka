package buildlock

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func skipWithoutLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no file locks")
	}
}

func vukaRoot(t *testing.T) string {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".vuka"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPruneWaitsForHolders(t *testing.T) {
	skipWithoutLocks(t)
	root := vukaRoot(t)
	if release, ok := TryPrune(root); !ok {
		t.Fatal("an idle module can't be pruned")
	} else {
		release()
	}
	a, b := Hold(root), Hold(root)
	if _, ok := TryPrune(root); ok {
		t.Fatal("pruned while held")
	}
	a()
	if _, ok := TryPrune(root); ok {
		t.Fatal("pruned while one holder is left")
	}
	b()
	release, ok := TryPrune(root)
	if !ok {
		t.Fatal("not pruned once every holder is done")
	}
	release()
}

func TestHoldNeverBlocks(t *testing.T) {
	skipWithoutLocks(t)
	root := vukaRoot(t)
	first := Hold(root)
	defer first()
	if _, ok := TryPrune(root); ok {
		t.Fatal("pruned while held")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Hold(root)()
		defer Hold(root)()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a shared hold waited behind a refused prune")
	}
}

func TestNoLockFileNoPrune(t *testing.T) {
	skipWithoutLocks(t)
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	Hold(root)()
	if _, ok := TryPrune(root); ok {
		t.Fatal("pruned a module whose lock can't be made")
	}
	if _, err := os.Stat(filepath.Join(root, ".vuka")); err == nil {
		t.Fatal("made .vuka in a read-only tree")
	}
	if _, ok := TryPrune(""); ok {
		t.Fatal("pruned no module")
	}
	Hold("")()
}

func TestReadersMakeNoVukaDir(t *testing.T) {
	skipWithoutLocks(t)
	root := t.TempDir()
	Hold(root)()
	if _, err := os.Stat(filepath.Join(root, ".vuka")); err == nil {
		t.Fatal("a reader made .vuka")
	}
	if _, ok := TryPrune(root); ok {
		t.Fatal("pruned in the round that made .vuka")
	}
	release, ok := TryPrune(root)
	if !ok {
		t.Fatal("not pruned once .vuka exists")
	}
	release()
}
