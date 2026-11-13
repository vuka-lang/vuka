package load

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelocatedPath(t *testing.T) {
	relErr := errors.New("Rel: can't make target relative to base")
	for _, c := range []struct {
		name        string
		target, rel string
		err         error
		want        string
	}{
		{"same volume, relative", "/proj/dep", "../dep", nil, "../dep"},
		{"same volume, needs dot prefix", "/proj/out/dep", "dep", nil, "./dep"},
		{"cross volume keeps absolute", "D:/proj/dep", "", relErr, "D:/proj/dep"},
		{"cross volume with space is quoted", "D:/My Proj/dep", "", relErr, `"D:/My Proj/dep"`},
		{"relative with space is quoted", "/My Proj/dep", "../My Proj/dep", nil, `"../My Proj/dep"`},
	} {
		if got := relocatedPath(c.target, c.rel, c.err); got != c.want {
			t.Errorf("%s: relocatedPath(%q, %q, err=%v) = %q, want %q", c.name, c.target, c.rel, c.err != nil, got, c.want)
		}
	}
}

// TestRelocateReplacesSameVolume: a go.mod's relative replace is rewritten
// relative to the output dir.
func TestRelocateReplacesSameVolume(t *testing.T) {
	mod := []byte("module example.com/app\n\ngo 1.27\n\nrequire example.com/dep v0.0.0\n\nreplace example.com/dep => ./dep\n")
	out := filepath.Join("mirror")
	got := string(relocateReplaces(mod, ".", out))
	if !strings.Contains(got, "=> ../dep") {
		t.Errorf("replace not relocated to ../dep:\n%s", got)
	}
}
