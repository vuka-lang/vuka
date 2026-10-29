package format

import (
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
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
const ConfigFile = load.ConfigFile

// LoadOptions reads the options of the project holding dir: the [fmt]
// table of the vuka.toml next to the go.mod of dir's module, or the first
// one found walking up from dir to it. No file is the defaults.
func LoadOptions(dir string) (Options, error) {
	p, b, err := load.FindConfig(dir)
	if err != nil || b == nil {
		return Options{}, err
	}
	return ParseOptions(p, b)
}

// ParseOptions reads vuka.toml's [fmt] table; file names it in errors. The
// file holds tables of key = value lines and # comments; vuka fmt reads
// [fmt], the compiler [compile] (load.ParseCompile), and a key it doesn't
// know is an error.
func ParseOptions(file string, src []byte) (Options, error) {
	var o Options
	err := load.ParseConfig(file, src, "fmt", func(key, value string, at func(string, ...any) error) error {
		switch key {
		case "width":
			w, err := strconv.Atoi(value)
			if err != nil || w < 40 {
				return at("width is a number of columns, 40 or more")
			}
			o.Width = w
		case "tailwind":
			b, err := strconv.ParseBool(value)
			if err != nil || value != "true" && value != "false" {
				return at("tailwind is true or false")
			}
			o.Tailwind = b
		default:
			hint := ""
			if s := closestKey(key, []string{"width", "tailwind"}); s != "" {
				hint = "; did you mean " + s + "?"
			}
			return at("unknown key %s in [fmt]%s", key, hint)
		}
		return nil
	})
	return o, err
}

func closestKey(k string, keys []string) string {
	for _, c := range keys {
		if strings.HasPrefix(c, k[:min(3, len(k))]) || strings.HasPrefix(k, c[:3]) {
			return c
		}
	}
	return ""
}
