package load

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneKeys(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	age := func(name string, ago time.Duration) string {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(dir, "out"), 0o755); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-ago)
		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	keep := age("current", 30*24*time.Hour)
	recent := age("recent", 6*24*time.Hour)
	old := age("old", 8*24*time.Hour)
	other := age("other", 40*24*time.Hour)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	pruneKeys(base, keep, now)
	if !exists(keep) || !exists(recent) || exists(old) || exists(other) {
		t.Errorf("after the first prune: current %v, recent %v, old %v, other %v; want true true false false",
			exists(keep), exists(recent), exists(old), exists(other))
	}
	if !exists(filepath.Join(base, pruneStamp)) {
		t.Error("no stamp")
	}

	// Within a day of the last prune nothing is looked at.
	stale := age("stale", 20*24*time.Hour)
	pruneKeys(base, keep, now.Add(pruneEvery-time.Minute))
	if !exists(stale) {
		t.Error("pruned again within a day")
	}
	pruneKeys(base, keep, now.Add(pruneEvery+time.Minute))
	if exists(stale) || !exists(keep) {
		t.Errorf("a day later: stale %v, current %v; want false true", exists(stale), exists(keep))
	}
}

func TestSessionPruneMarksItsKey(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n")
	base := t.TempDir()
	s, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	s.UseCache(base)
	s.reset()
	dir := s.diskDir()
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(dir, old, old)
	s.pruneCache()
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the session's own directory was pruned: %v", err)
	}
	if time.Since(fi.ModTime()) > time.Hour {
		t.Errorf("the session's directory was not marked used: %v", fi.ModTime())
	}
}

// TestSaveEntryKeepsRecent: a package keeps keepOuts entries, the ones
// used last; reading one counts as a use.
func TestSaveEntryKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-time.Hour)
	var files []string
	for i := range keepOuts + 2 {
		f := entryFile(dir, "out", "example.com/app/lib", string(rune('a'+i)))
		saveEntry(f, []byte{byte(i)})
		at := start.Add(time.Duration(i) * time.Minute)
		os.Chtimes(f, at, at)
		files = append(files, f)
		if i == 0 {
			continue
		}
		if i == keepOuts-1 {
			readEntry(files[0]) // the oldest, used again: kept
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	entries, _ := os.ReadDir(filepath.Dir(files[0]))
	if len(entries) != keepOuts {
		t.Fatalf("%d entries, want %d", len(entries), keepOuts)
	}
	if !exists(files[0]) || exists(files[1]) || exists(files[2]) || !exists(files[len(files)-1]) {
		t.Errorf("kept: first %v, second %v, third %v, last %v; want true false false true",
			exists(files[0]), exists(files[1]), exists(files[2]), exists(files[len(files)-1]))
	}
}
