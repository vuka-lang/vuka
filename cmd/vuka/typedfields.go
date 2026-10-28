package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const ormModule = "github.com/vuka-lang/orm/v2"

// typedFieldsUse matches generated code reading a model's typed fields:
// PostFields.Title.
var typedFieldsUse = regexp.MustCompile(`\b[A-Za-z_]\w*(?:Fields|Columns)\.[A-Z]`)

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
	name, args := ormCommand(root, mod)
	args = append(args, "generate", "--missing")
	if tests {
		args = append(args, "--tests")
	}
	if overlay != "" {
		args = append(args, "--overlay", overlay)
	}
	var out, errOut bytes.Buffer
	c := exec.Command(name, append(args, dir)...)
	c.Dir, c.Stdout, c.Stderr = root, &out, &errOut
	if err := c.Run(); err != nil {
		what := "orm"
		if name == "go" {
			what = "go " + args[0]
		}
		return nil, fmt.Errorf("%s: %v\n%s", what, err, bytes.TrimSpace(errOut.Bytes()))
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
// UserFields.Email) or declares models: the packages importing it are
// type-checked with them, whether or not a package after it fails.
func packageTypedFields(dir, overlay string, gens []load.Generated) []load.Generated {
	root, _, err := load.ModuleRoot(dir)
	if err != nil {
		return nil
	}
	missing, err := ormMissingIn(root, dir, overlay, gens, false, declaresModels(gens))
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

// modelEmbed matches a struct embedding the orm's model base, as
// transpiled Go writes it: orm.Model[User], or orm.Base.
var modelEmbed = regexp.MustCompile(`(?m)^\s+\w+\.(?:Model\[\w+\]|Base)\s*(?:$|//|\x60)`)

// declaresModels reports whether gens, a package's Go, declare orm models.
func declaresModels(gens []load.Generated) bool {
	for _, g := range gens {
		if bytes.Contains(g.Src, []byte(`"`+ormModule+`"`)) && modelEmbed.Match(g.Src) {
			return true
		}
	}
	return false
}

// ormTools are the orm tool binaries found, by module root, go.mod and
// go.sum: a build runs the tool once for each package of models, and each
// go tool orm (or go run) checks the tool's whole build again first —
// seconds each on a busy machine.
var ormTools sync.Map // key → binary path

// ormCommand is how to run the module's orm tool: the binary go tool (or
// go build) made of it, found once a process, else go tool orm or go run.
func ormCommand(root string, mod []byte) (string, []string) {
	tool := bytes.Contains(mod, []byte("tool "+ormModule+"/cmd/orm"))
	fallback := []string{"run", ormModule + "/cmd/orm"}
	if tool {
		fallback = []string{"tool", "orm"}
	}
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n%s\n", root, mod, sum, os.Getenv("GOFLAGS"))
	key := hex.EncodeToString(h.Sum(nil))
	if bin, ok := ormTools.Load(key); ok {
		if _, err := os.Stat(bin.(string)); err == nil {
			return bin.(string), nil
		}
	}
	var bin string
	if tool {
		c := exec.Command("go", "tool", "-n", "orm")
		c.Dir = root
		out, err := c.Output()
		if err != nil {
			return "go", fallback
		}
		bin = strings.TrimSpace(string(out))
	} else {
		dir, ok := load.CacheDir()
		if !ok {
			return "go", fallback
		}
		bin = filepath.Join(dir, "orm", key[:24], "orm"+exeSuffix())
		c := exec.Command("go", "build", "-o", bin, ormModule+"/cmd/orm")
		c.Dir = root
		if err := c.Run(); err != nil {
			return "go", fallback
		}
	}
	if _, err := os.Stat(bin); err != nil || !filepath.IsAbs(bin) {
		return "go", fallback
	}
	ormTools.Store(key, bin)
	return bin, nil
}
