package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// TestORMAnswers: the orm tool's answer comes back for the slot and shape
// it was given, and for no other; an answer unused for a week is pruned.
func TestORMAnswers(t *testing.T) {
	t.Setenv("VUKA_CACHE", t.TempDir())
	files := []load.Generated{{Target: "/app/models/orm_scanners_gen.go", Src: []byte("package models\n")}}
	saveORMAnswer("slot", "shape", files)
	got, ok := loadORMAnswer("slot", "shape")
	if !ok || len(got) != 1 || got[0].Target != files[0].Target || string(got[0].Src) != string(files[0].Src) {
		t.Fatalf("load: %v %+v", ok, got)
	}
	for _, k := range [][2]string{{"slot", "other"}, {"other", "shape"}, {"slot", ""}} {
		if _, ok := loadORMAnswer(k[0], k[1]); ok {
			t.Errorf("slot %q shape %q: an answer", k[0], k[1])
		}
	}
	empty := []load.Generated(nil)
	saveORMAnswer("slot", "none", empty)
	if got, ok := loadORMAnswer("slot", "none"); !ok || len(got) != 0 {
		t.Errorf("an empty answer: %v %+v", ok, got)
	}

	file := ormAnswerPath("slot", "shape")
	old := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(file, old, old)
	pruneORMAnswers(filepath.Dir(filepath.Dir(file)), time.Now().Add(48*time.Hour))
	if _, err := os.Stat(file); err == nil {
		t.Error("an answer unused for a week is kept")
	}
	if _, ok := loadORMAnswer("slot", "none"); !ok {
		t.Error("a recent answer is pruned")
	}
}
