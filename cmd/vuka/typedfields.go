package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const ormModule = "github.com/vuka-lang/orm/v2"

// typedFieldsUse matches generated code reading a model's typed fields:
// PostFields.Title.
var typedFieldsUse = regexp.MustCompile(`\b[A-Za-z_]\w*Fields\.[A-Z]`)

// typedFields adds what orm generate writes and the tree lacks — a model's
// typed fields (PostFields, in post_gen.go beside post.vuka) and its
// scanners — to the overlay, so a .vuka model's field references compile
// before anything is written: Post.Slug is PostFields.Slug. It runs the
// module's orm tool (go tool orm when go.mod declares it), only when the
// module requires the orm and the generated code reads typed fields. A
// failure is said and the build goes on: the compiler then names what is
// missing.
func typedFields(root, tmp, overlay string, gens []load.Generated, tests bool, stderr io.Writer) (string, []load.Generated) {
	missing, err := ormMissing(root, overlay, gens, tests, false)
	if err != nil {
		fmt.Fprintf(stderr, "vuka: the models' typed fields: %v\n  (go tool orm generate writes them)\n", err)
		return overlay, gens
	}
	if len(missing) == 0 {
		return overlay, gens
	}
	if overlay, err = addToOverlay(overlay, tmp, missing); err != nil {
		fmt.Fprintf(stderr, "vuka: the models' typed fields: %v\n", err)
		return overlay, gens
	}
	return overlay, append(gens, missing...)
}

// ormMissing is what orm generate would write that the tree lacks, read
// through overlay (with tests, for the models of test files too); unless
// all, none when the generated code doesn't read typed fields.
func ormMissing(root, overlay string, gens []load.Generated, tests, all bool) ([]load.Generated, error) {
	return ormMissingIn(root, ".", overlay, gens, tests, all)
}

// ormMissingIn is ormMissing for the models under dir, a directory of the
// module at root.
func ormMissingIn(root, dir, overlay string, gens []load.Generated, tests, all bool) ([]load.Generated, error) {
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !bytes.Contains(mod, []byte(ormModule+" ")) && !bytes.Contains(mod, []byte("module "+ormModule+"\n")) {
		return nil, nil
	}
	used := all
	for _, g := range gens {
		used = used || typedFieldsUse.Match(g.Src)
	}
	if !used {
		return nil, nil
	}
	args := []string{"run", ormModule + "/cmd/orm"}
	if bytes.Contains(mod, []byte("tool "+ormModule+"/cmd/orm")) {
		args = []string{"tool", "orm"}
	}
	args = append(args, "generate", "--missing")
	if tests {
		args = append(args, "--tests")
	}
	if overlay != "" {
		args = append(args, "--overlay", overlay)
	}
	var out, errOut bytes.Buffer
	c := exec.Command("go", append(args, dir)...)
	c.Dir, c.Stdout, c.Stderr = root, &out, &errOut
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("go %s: %v\n%s", args[0], err, bytes.TrimSpace(errOut.Bytes()))
	}
	var files map[string]string
	if err := json.Unmarshal(out.Bytes(), &files); err != nil {
		return nil, fmt.Errorf("orm generate --missing: %v", err)
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var missing []load.Generated
	for _, p := range paths {
		missing = append(missing, load.Generated{Target: p, Src: []byte(files[p])})
	}
	return missing, nil
}

func init() {
	load.AfterPackage = func(dir, overlay string, gens []load.Generated) []load.Generated {
		return append(packageTypedFields(dir, overlay, gens), routeStubs[dir]...)
	}
}

// packageTypedFields is what orm generate would write for a Vuka package
// just transpiled whose Go reads typed fields (User.Email is
// UserFields.Email): the packages importing it are type-checked with them.
func packageTypedFields(dir, overlay string, gens []load.Generated) []load.Generated {
	root, _, err := load.ModuleRoot(dir)
	if err != nil {
		return nil
	}
	missing, err := ormMissingIn(root, dir, overlay, gens, false, false)
	if err != nil {
		return nil // the build's own pass says so
	}
	var out []load.Generated
	for _, g := range missing {
		if filepath.Dir(g.Target) == dir {
			out = append(out, g)
		}
	}
	return out
}
