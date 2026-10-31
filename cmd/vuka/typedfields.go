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
	"slices"
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
		toolKey, err := ormToolKey(root)
		if err != nil {
			return "go", fallback
		}
		bin, err = cachedOrmTool(filepath.Join(dir, "orm"), toolKey, func(out string) error {
			c := exec.Command("go", "build", "-o", out, ormModule+"/cmd/orm")
			c.Dir = root
			return c.Run()
		})
		if err != nil {
			return "go", fallback
		}
	}
	if _, err := os.Stat(bin); err != nil || !filepath.IsAbs(bin) {
		return "go", fallback
	}
	ormTools.Store(key, bin)
	return bin, nil
}

// ormToolKey names the orm tool root's module builds, by what decides its
// bytes: the go command's target and the modules its packages come from.
// Those are named by version, so every project and temp copy on the same
// versions shares one binary; a module a replace or go.work points at a
// directory is hashed by its packages' files.
func ormToolKey(root string) (string, error) {
	c := exec.Command("go", "env", "GOVERSION", "GOOS", "GOARCH", "GOEXPERIMENT", "CGO_ENABLED", "GOARM64", "GOAMD64", "GOFLAGS")
	c.Dir = root
	env, err := c.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSuffix(string(env), "\n"), "\n")
	flags := strings.Fields(lines[len(lines)-1])
	flags = slices.DeleteFunc(flags, func(f string) bool { return strings.HasPrefix(f, "-p=") })
	h := sha256.New()
	fmt.Fprintf(h, "1\n%s\n%s\n", strings.Join(lines[:len(lines)-1], "\n"), strings.Join(flags, " "))

	c = exec.Command("go", "list", "-deps", "-json=ImportPath,Dir,GoFiles,CgoFiles,EmbedFiles,Module", ormModule+"/cmd/orm")
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p ormToolPackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return "", err
		}
		if p.Module == nil {
			continue // the standard library: the Go version
		}
		if p.Module.Main || p.Module.Replace != nil && p.Module.Replace.Version == "" {
			fmt.Fprintf(h, "package %s\n", p.ImportPath)
			for _, f := range slices.Concat(p.GoFiles, p.CgoFiles, p.EmbedFiles) {
				hashFile(h, filepath.Join(p.Dir, f))
			}
			continue
		}
		if !seen[p.Module.Path] {
			seen[p.Module.Path] = true
			fmt.Fprintf(h, "module %s %s", p.Module.Path, p.Module.Version)
			if r := p.Module.Replace; r != nil {
				fmt.Fprintf(h, " => %s %s", r.Path, r.Version)
			}
			fmt.Fprintln(h)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:24], nil
}

type ormToolPackage struct {
	ImportPath, Dir               string
	GoFiles, CgoFiles, EmbedFiles []string
	Module                        *struct {
		Path, Version string
		Main          bool
		Replace       *struct{ Path, Version string }
	}
}

// cachedOrmTool is base/key's binary, built once: build writes it to a
// temporary path and a rename puts it in place, so another process, or one
// interrupted here, sees the whole binary or none. Two building at once
// both succeed; the last rename wins, and the binaries are the same.
func cachedOrmTool(base, key string, build func(out string) error) (string, error) {
	dir := filepath.Join(base, key)
	bin := filepath.Join(dir, "orm"+exeSuffix())
	if _, err := os.Stat(bin); err == nil {
		markCacheUsed(dir)
		return bin, nil
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(base, cacheTmpPrefix)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, filepath.Base(bin))
	if err := build(out); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(out, bin); err != nil {
		// Windows won't replace a running binary: one is already there.
		if _, serr := os.Stat(bin); serr != nil {
			return "", err
		}
	}
	return bin, nil
}
