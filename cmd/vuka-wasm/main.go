//go:build !js

// Command vuka-wasm is the docs playground's compiler: built for js/wasm it
// transpiles and formats a project in the browser (main_js.go); run here it
// writes the types snapshot the browser's build reads. docs/playground/build.sh
// (npm run playground in docs/) does both with one Go.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
)

// libraries are the packages a playground project may import besides the
// standard library's.
var libraries = []string{
	"github.com/vuka-lang/vuka/v2",
	"github.com/vuka-lang/ui/v2",
	"github.com/vuka-lang/ui/v2/forms",
	"github.com/vuka-lang/ui/v2/live",
	"github.com/vuka-lang/ui/v2/uigen",
	"github.com/vuka-lang/ui/v2/validate",
	"github.com/vuka-lang/web/v2",
	"github.com/vuka-lang/web/v2/auth",
	"github.com/vuka-lang/web/v2/cron",
	"github.com/vuka-lang/web/v2/graphql",
	"github.com/vuka-lang/web/v2/mail",
	"github.com/vuka-lang/orm/v2",
	"github.com/vuka-lang/orm/v2/fn",
	"github.com/vuka-lang/orm/v2/migration",
	"github.com/vuka-lang/orm/v2/ormweb",
	"github.com/vuka-lang/orm/v2/sqlite",
}

// std are the standard packages the snapshot holds: those apps and the
// code vuka generates commonly import.
var std = []string{
	"bufio", "bytes", "cmp", "context", "crypto/rand", "crypto/sha256",
	"database/sql", "database/sql/driver", "embed", "encoding",
	"encoding/base64", "encoding/binary", "encoding/csv", "encoding/hex",
	"encoding/json", "errors", "fmt", "hash", "html", "html/template", "io",
	"io/fs", "iter", "log", "log/slog", "maps", "math", "math/big",
	"math/rand/v2", "mime", "net/http", "net/mail", "net/url", "os", "path",
	"path/filepath", "reflect", "regexp", "slices", "sort", "strconv",
	"strings", "sync", "sync/atomic", "text/template", "time", "unicode",
	"unicode/utf8",
}

func main() {
	out := flag.String("o", "types.bin", "the snapshot file to write")
	dir := flag.String("dir", "docs/testdata/gomode", "the module whose go.mod pins the libraries")
	flag.Parse()
	pkgs, err := buildSnapshot(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, encodeSnapshot(pkgs), 0o644); err != nil {
		log.Fatal(err)
	}
}

// buildSnapshot is the export data of the libraries and the standard
// packages, as the go command builds them in the module at dir.
func buildSnapshot(dir string) (map[string][]byte, error) {
	args := append([]string{"list", "-e", "-export", "-json=ImportPath,Export,Error"}, libraries...)
	cmd := exec.Command("go", append(args, std...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	pkgs := map[string][]byte{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			ImportPath, Export string
			Error              *struct{ Err string }
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list: %v", err)
		}
		if p.Error != nil {
			return nil, fmt.Errorf("%s: %s", p.ImportPath, p.Error.Err)
		}
		archive, err := os.ReadFile(p.Export)
		if err != nil {
			return nil, err
		}
		def, err := pkgdef(archive)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", p.ImportPath, err)
		}
		pkgs[p.ImportPath] = def
	}
	for _, p := range append(libraries, std...) {
		if pkgs[p] == nil {
			return nil, fmt.Errorf("%s: no export data", p)
		}
	}
	return pkgs, nil
}
