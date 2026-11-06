package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// vuka ui theme: a palette of the registry's written into the design
// tokens vuka ui init wrote, between their markers: the light and the
// dark values of the colours. A token whose value isn't the palette's it
// replaces is the app's, and is kept.

// palette is a theme's colours, a token's name → its value, light and
// dark, with the order the names come in.
type palette struct {
	light, dark map[string]string
	order       []string
}

// parsePalette reads a theme's stylesheet: its :root values are light,
// .dark's dark.
func parsePalette(css string) palette {
	p := palette{light: map[string]string{}, dark: map[string]string{}}
	for _, r := range cssRules(css) {
		var to map[string]string
		switch strings.Join(strings.Fields(r.prelude), " ") {
		case ":root":
			to = p.light
		case ".dark", ":root.dark", "html.dark":
			to = p.dark
		default:
			continue
		}
		for _, d := range customProps(r.body) {
			if _, ok := p.light[d.name]; !ok {
				if _, ok := p.dark[d.name]; !ok {
					p.order = append(p.order, d.name)
				}
			}
			to[d.name] = d.value
		}
	}
	return p
}

// tokensPalette is the palette the design tokens' block declares: :root's
// values light, those of :root[data-theme="dark"] dark.
func tokensPalette(block []byte) palette {
	p := palette{light: map[string]string{}, dark: map[string]string{}}
	walkTokens(block, func(section, name, value string) string {
		switch section {
		case "light":
			p.light[name] = value
		case "dark":
			p.dark[name] = value
		}
		if !slices.Contains(p.order, name) {
			p.order = append(p.order, name)
		}
		return value
	})
	return p
}

var (
	tokenDecl = regexp.MustCompile(`^(\s*)(--[\w-]+)(\s*:\s*)(.*?)(;\s*)$`)
	darkRoot  = regexp.MustCompile(`^:root(?::not\(\[data-theme=["']light["']\]\)|\[data-theme=["']dark["']\])$`)
)

// walkTokens calls f with each custom property of the tokens' block, the
// section it is in ("light" for :root, "dark" for the two dark blocks, ""
// elsewhere), and puts the value f returns in its place.
func walkTokens(block []byte, f func(section, name, value string) string) []byte {
	var out bytes.Buffer
	var stack []string
	for _, l := range strings.SplitAfter(string(block), "\n") {
		line := strings.TrimRight(l, "\n")
		t := strings.TrimSpace(line)
		section := ""
		if len(stack) > 0 {
			switch top := stack[len(stack)-1]; {
			case top == ":root":
				section = "light"
			case darkRoot.MatchString(top):
				section = "dark"
			}
		}
		switch {
		case strings.HasSuffix(t, "{"):
			stack = append(stack, strings.Join(strings.Fields(strings.TrimSuffix(t, "{")), " "))
		case t == "}" && len(stack) > 0:
			stack = stack[:len(stack)-1]
		case len(stack) > 0:
			if m := tokenDecl.FindStringSubmatch(line); m != nil {
				if v := f(section, m[2], m[4]); v != m[4] {
					l = m[1] + m[2] + m[3] + v + m[5] + strings.TrimPrefix(l, line)
				}
			}
		}
		out.WriteString(l)
	}
	return out.Bytes()
}

// applyPalette is the tokens' block with to's colours in place of from's:
// a token whose value isn't from's is the app's, kept (and named in kept).
func applyPalette(block []byte, from, to palette) (next []byte, kept []string) {
	next = walkTokens(block, func(section, name, value string) string {
		var was, want string
		var ok bool
		switch section {
		case "light":
			was, want, ok = from.light[name], to.light[name], to.light[name] != ""
		case "dark":
			was, want, ok = from.dark[name], to.dark[name], to.dark[name] != ""
		}
		switch {
		case !ok:
			return value
		case was != "" && value != was:
			if !slices.Contains(kept, name) {
				kept = append(kept, name)
			}
			return value
		}
		return want
	})
	return next, kept
}

// tokensBlock is where the design tokens are in css: their start and end;
// ok is false when css has none.
func tokensBlock(css []byte) (start, end int, ok bool) {
	start, end = bytes.Index(css, []byte(tokensStart)), bytes.Index(css, []byte(tokensEnd))
	if start < 0 || end < start {
		return 0, 0, false
	}
	return start, end + len(tokensEnd), true
}

// themes are the registry's themes' names.
func (r *registry) themes() []string { return slices.Sorted(mapKeys(r.Themes)) }

// palette is the registry's theme name.
func (r *registry) palette(cmd, name string) (palette, error) {
	f, ok := r.Themes[name]
	if !ok {
		if len(r.Themes) == 0 {
			return palette{}, fmt.Errorf("vuka ui %s: the registry has no themes", cmd)
		}
		return palette{}, fmt.Errorf("vuka ui %s: no theme %q%s; the themes are %s", cmd, name, suggest(name, r.themes(), ""), strings.Join(r.themes(), ", "))
	}
	b, err := r.file(f)
	if err != nil {
		return palette{}, err
	}
	return parsePalette(string(b)), nil
}

// basePalette is the palette the app's tokens were written with: [ui]
// theme's, else the style's own.
func (r *registry) basePalette(cmd string, cfg uiConfig) (palette, error) {
	if cfg.Theme != "" {
		if _, ok := r.Themes[cfg.Theme]; ok {
			return r.palette(cmd, cfg.Theme)
		}
	}
	s, ok := r.Styles[cfg.Style]
	if !ok {
		return palette{}, nil
	}
	b, err := r.file(s)
	if err != nil {
		return palette{}, err
	}
	return tokensPalette(b), nil
}

// themeOnto queues the tokens of the app's stylesheet (as out leaves it)
// with the theme name's colours on out; cfg is [ui] as it will be.
func themeOnto(out *uiWriter, reg *registry, cfg uiConfig, was uiConfig, name string) error {
	to, err := reg.palette("theme", name)
	if err != nil {
		return err
	}
	from, err := reg.basePalette("theme", was)
	if err != nil {
		return err
	}
	css, err := out.read(cfg.CSS)
	if err != nil {
		return err
	}
	start, end, ok := tokensBlock(css)
	if !ok {
		return fmt.Errorf("vuka ui theme: %s hasn't the design tokens vuka ui init writes (vuka ui init -f writes them)", cfg.CSS)
	}
	block, kept := applyPalette(css[start:end], from, to)
	if len(kept) > 0 {
		out.note("kept the app's %s (its value isn't the theme's it replaces)", strings.Join(kept, ", "))
	}
	if !bytes.Equal(block, css[start:end]) {
		out.write(cfg.CSS, slices.Concat(css[:start], block, css[end:]), "the "+name+" theme's colours")
	} else {
		out.note("%s has the %s theme's colours", cfg.CSS, name)
	}
	return nil
}

// uiThemeCmd is vuka ui theme [name].
func uiThemeCmd(root string, f *uiFlags, w io.Writer) error {
	cfg, ok, err := readUIConfig(root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("vuka ui theme: vuka.toml has no [ui]: run vuka ui init first (vuka ui init --theme <name> writes the tokens with a theme)")
	}
	reg, err := openRegistry(f, cfg, root)
	if err != nil {
		return err
	}
	if len(f.pos) == 0 {
		current := cfg.Theme
		if current == "" {
			current = "the style's own"
		}
		fmt.Fprintf(w, "the themes: %s (the app's: %s)\n", strings.Join(reg.themes(), ", "), current)
		return nil
	}
	if len(f.pos) > 1 {
		return errors.New("usage: vuka ui theme [name]")
	}
	name := strings.ToLower(f.pos[0])
	next := cfg
	next.Theme = name
	out := &uiWriter{root: root, dryRun: f.dryRun}
	if reg.note != "" {
		out.note("%s", reg.note)
	}
	if err := themeOnto(out, reg, next, cfg, name); err != nil {
		return err
	}
	toml, err := os.ReadFile(filepath.Join(root, "vuka.toml"))
	if err != nil {
		return err
	}
	if t := replaceUITable(toml, []byte(next.table())); !bytes.Equal(t, toml) {
		out.write("vuka.toml", t, "[ui] theme")
	}
	return out.commit(w)
}
