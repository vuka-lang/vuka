package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// The orm tool's answers on disk, by the shape of the module they were
// given (sourceShapes.module, which leaves out comments, line directives
// and the bodies the tool doesn't read): a vuka command after a vuka
// command, an edit that moves no model, doesn't run the tool again. Running
// it loads, and so compiles for their export data, every package calling
// orm.For and the packages they import: after a comment in a widely
// imported models package, most of the module.

// ormShapes is the watch's shapes, or this command's own.
func ormShapes() *sourceShapes {
	if shapes != nil {
		return shapes
	}
	oneShotShapes.Do(func() { ownShapes = newSourceShapes() })
	return ownShapes
}

var (
	oneShotShapes sync.Once
	ownShapes     *sourceShapes
)

// ormAnswerVersion is the format of an answer on disk.
const ormAnswerVersion = "1"

type ormAnswer struct {
	Slot, Shape string
	Files       []ormAnswerFile
}

type ormAnswerFile struct {
	Target string
	Src    []byte
}

// ormAnswerPath is where the answer of slot for shape is; "" when vuka
// keeps no cache.
func ormAnswerPath(slot, shape string) string {
	dir, ok := load.CacheDir()
	if !ok || shape == "" {
		return ""
	}
	h := sha256.Sum256([]byte(ormAnswerVersion + "\n" + load.CompilerVersion + "\n" + slot + "\n" + shape))
	return filepath.Join(dir, "ormanswers", hex.EncodeToString(h[:1]), hex.EncodeToString(h[1:16]))
}

func loadORMAnswer(slot, shape string) ([]load.Generated, bool) {
	file := ormAnswerPath(slot, shape)
	if file == "" {
		return nil, false
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	now := time.Now()
	os.Chtimes(file, now, now)
	var a ormAnswer
	if gob.NewDecoder(bytes.NewReader(data)).Decode(&a) != nil || a.Slot != slot || a.Shape != shape {
		return nil, false
	}
	var missing []load.Generated
	for _, f := range a.Files {
		missing = append(missing, load.Generated{Target: f.Target, Src: f.Src})
	}
	return missing, true
}

func saveORMAnswer(slot, shape string, missing []load.Generated) {
	file := ormAnswerPath(slot, shape)
	if file == "" {
		return
	}
	a := ormAnswer{Slot: slot, Shape: shape}
	for _, g := range missing {
		a.Files = append(a.Files, ormAnswerFile{g.Target, g.Src})
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(a) != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(file), 0o755) != nil {
		return
	}
	pruneORMAnswers(filepath.Dir(filepath.Dir(file)), time.Now())
	tmp, err := os.CreateTemp(filepath.Dir(file), ".tmp-")
	if err != nil {
		return
	}
	_, err = tmp.Write(buf.Bytes())
	if cerr := tmp.Close(); err == nil && cerr == nil && os.Rename(tmp.Name(), file) == nil {
		return
	}
	os.Remove(tmp.Name())
}

// pruneORMAnswers removes the answers under base no command has read or
// written for a week, looking once a day.
func pruneORMAnswers(base string, now time.Time) {
	stamp := filepath.Join(base, ".pruned")
	if fi, err := os.Stat(stamp); err == nil && now.Sub(fi.ModTime()) < 24*time.Hour {
		return
	}
	if os.WriteFile(stamp, nil, 0o644) != nil {
		return
	}
	os.Chtimes(stamp, now, now)
	filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".pruned") {
			return nil
		}
		if fi, err := d.Info(); err == nil && now.Sub(fi.ModTime()) > 7*24*time.Hour {
			os.Remove(path)
		}
		return nil
	})
}
