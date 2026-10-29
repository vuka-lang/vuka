package format

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Options are what vuka fmt lays a .vuka.html file out by. A project sets
// them in the [fmt] table of vuka.toml, next to its go.mod:
//
//	[fmt]
//	width = 100      # the column markup wraps a tag's attributes at
//	tailwind = true  # order class="…" utilities as Tailwind does
type Options struct {
	Width    int  // 0 is 100
	Tailwind bool // order static class lists' utilities
}

// DefaultWidth is the column a tag's attributes wrap at.
const DefaultWidth = 100

func (o Options) width() int {
	if o.Width <= 0 {
		return DefaultWidth
	}
	return o.Width
}

// ConfigFile is the file a project's formatting options are in.
const ConfigFile = "vuka.toml"

// LoadOptions reads the options of the project holding dir: the [fmt]
// table of the vuka.toml next to the go.mod of dir's module, or the first
// one found walking up from dir to it. No file is the defaults.
func LoadOptions(dir string) (Options, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Options{}, err
	}
	for d := abs; ; {
		p := filepath.Join(d, ConfigFile)
		if b, err := os.ReadFile(p); err == nil {
			return ParseOptions(p, b)
		}
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return Options{}, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return Options{}, nil
		}
		d = up
	}
}

// ParseOptions reads vuka.toml's [fmt] table; file names it in errors. The
// file holds tables of key = value lines and # comments: [fmt], read here,
// where a key it doesn't know is an error, and [ui], which vuka ui reads.
func ParseOptions(file string, src []byte) (Options, error) {
	var o Options
	table := ""
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
				return o, at("a table's name ends with ]")
			}
			table = strings.TrimSpace(l[1 : len(l)-1])
			if table != "fmt" && table != "ui" {
				return o, at("unknown table [%s]: vuka.toml has [fmt] and [ui]", table)
			}
			continue
		}
		if table == "ui" {
			continue
		}
		key, value, ok := strings.Cut(l, "=")
		if !ok {
			return o, at("expected key = value")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if table != "fmt" {
			return o, at("%s is outside a table: write it under [fmt]", key)
		}
		switch key {
		case "width":
			w, err := strconv.Atoi(value)
			if err != nil || w < 40 {
				return o, at("width is a number of columns, 40 or more")
			}
			o.Width = w
		case "tailwind":
			b, err := strconv.ParseBool(value)
			if err != nil || value != "true" && value != "false" {
				return o, at("tailwind is true or false")
			}
			o.Tailwind = b
		default:
			hint := ""
			if s := closestKey(key, []string{"width", "tailwind"}); s != "" {
				hint = "; did you mean " + s + "?"
			}
			return o, at("unknown key %s in [fmt]%s", key, hint)
		}
	}
	return o, nil
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

func closestKey(k string, keys []string) string {
	for _, c := range keys {
		if strings.HasPrefix(c, k[:min(3, len(k))]) || strings.HasPrefix(k, c[:3]) {
			return c
		}
	}
	return ""
}
