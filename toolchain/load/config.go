package load

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConfigFile is a project's settings, next to its go.mod: the [fmt] table
// vuka fmt lays files out by (toolchain/format), the [compile] table
// the compiler reads, and the [ui] table vuka ui reads.
const ConfigFile = "vuka.toml"

// ConfigTables are the tables vuka.toml may hold.
var ConfigTables = []string{"fmt", "compile", "ui"}

// Compile are a module's compiler settings, vuka.toml's [compile] table:
//
//	[compile]
//	implicit_ctx = false  # a call in markup gets no implicit ctx (default true)
type Compile struct {
	// NoImplicitCtx is implicit_ctx = false: a call in markup one
	// context.Context short is E0162 (transpile.Options.NoImplicitCtx).
	NoImplicitCtx bool
}

// LoadCompile reads the compiler settings of the module holding dir: the
// [compile] table of the vuka.toml next to its go.mod, or the first one
// found walking up from dir to it. No file is the defaults.
func LoadCompile(dir string) (Compile, error) {
	p, b, err := FindConfig(dir)
	if err != nil || b == nil {
		return Compile{}, err
	}
	return ParseCompile(p, b)
}

// FindConfig is the vuka.toml of the module holding dir, and its text: nil
// when there is none.
func FindConfig(dir string) (string, []byte, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	for d := abs; ; {
		p := filepath.Join(d, ConfigFile)
		if b, err := os.ReadFile(p); err == nil {
			return p, b, nil
		}
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return "", nil, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return "", nil, nil
		}
		d = up
	}
}

// compileSettings are the modules' compiler settings a run has read, by
// the directory of a package of each.
type compileSettings map[string]Compile

func (m compileSettings) of(dir string) (Compile, error) {
	if c, ok := m[dir]; ok {
		return c, nil
	}
	c, err := LoadCompile(dir)
	if err != nil {
		return c, err
	}
	m[dir] = c
	return c, nil
}

// ParseCompile reads vuka.toml's [compile] table; file names it in errors.
func ParseCompile(file string, src []byte) (Compile, error) {
	var c Compile
	err := ParseConfig(file, src, "compile", func(key, value string, at func(string, ...any) error) error {
		switch key {
		case "implicit_ctx":
			b, err := strconv.ParseBool(value)
			if err != nil || value != "true" && value != "false" {
				return at("implicit_ctx is true or false")
			}
			c.NoImplicitCtx = !b
			return nil
		}
		return at("unknown key %s in [compile]: it has implicit_ctx", key)
	})
	return c, err
}

// ParseConfig reads vuka.toml — tables of key = value lines and # comments
// — calling set for each key of table; file names it in errors, which at
// makes with the key's line. A table not in ConfigTables, or a key outside
// a table, is an error.
func ParseConfig(file string, src []byte, table string, set func(key, value string, at func(string, ...any) error) error) error {
	in := ""
	for n, line := range strings.Split(string(src), "\n") {
		l := strings.TrimSpace(stripTOMLComment(line))
		if l == "" {
			continue
		}
		at := func(format string, args ...any) error {
			return fmt.Errorf("%s:%d: %s", file, n+1, fmt.Sprintf(format, args...))
		}
		if strings.HasPrefix(l, "[") {
			if !strings.HasSuffix(l, "]") {
				return at("a table's name ends with ]")
			}
			in = strings.TrimSpace(l[1 : len(l)-1])
			known := false
			for _, t := range ConfigTables {
				known = known || in == t
			}
			if !known {
				return at("unknown table [%s]: vuka.toml has [fmt], [compile] and [ui]", in)
			}
			continue
		}
		key, value, ok := strings.Cut(l, "=")
		if !ok {
			return at("expected key = value")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if in == "" {
			return at("%s is outside a table: write it under [%s]", key, table)
		}
		if in != table {
			continue
		}
		if err := set(key, value, at); err != nil {
			return err
		}
	}
	return nil
}

func stripTOMLComment(l string) string {
	in := false
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case '"':
			in = !in
		case '#':
			if !in {
				return l[:i]
			}
		}
	}
	return l
}
