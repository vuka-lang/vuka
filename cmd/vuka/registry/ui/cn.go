// Package design is the app's own components, written by vuka ui add:
// Tailwind utilities over the theme's tokens (vuka ui init put them in the
// app's stylesheet). The files are the app's: change them as you like.
package design

import (
	"slices"
	"strings"
)

// cn joins class lists, a later class winning over an earlier one setting
// the same thing, so a component's Class overrides its defaults:
//
//	cn("px-4 py-2 bg-primary", "px-2 bg-red-600") // "py-2 px-2 bg-red-600"
//
// It knows a small part of Tailwind:
//
//   - padding and margin, per side: p-4 drops an earlier px-2 and pt-1;
//     px-2 after p-4 keeps both, as it refines it
//   - the background colour (bg-red-500, bg-primary/90; not bg-cover,
//     bg-center or the other bg-* utilities)
//   - text colour and text size, kept apart (text-sm, text-muted-foreground),
//     and text alignment
//   - rounded-*, all corners or a side or a corner
//   - font weight (font-medium) and family (font-mono)
//   - display (block, flex, inline-flex, grid, hidden…)
//   - width, height and size-* (size-4 drops an earlier w-2 and h-2)
//   - gap, gap-x and gap-y
//
// Classes conflict only under the same variants (hover:bg-accent and
// bg-primary are both kept) and the same importance (!p-0). Any other class
// is kept as written; of an exact repeat the last is kept.
func cn(classes ...string) string {
	var all []string
	for _, c := range classes {
		all = append(all, strings.Fields(c)...)
	}
	taken := map[string]bool{}
	keep := make([]bool, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		variants, base := splitVariants(all[i])
		if b, ok := strings.CutPrefix(base, "!"); ok {
			variants, base = variants+"!", b
		} else if b, ok := strings.CutSuffix(base, "!"); ok {
			variants, base = variants+"!", b
		}
		group, covers := classGroup(base)
		if group == "" {
			group = "=" + base
		}
		if taken[variants+"|"+group] {
			continue
		}
		keep[i] = true
		taken[variants+"|"+group] = true
		for _, c := range covers {
			taken[variants+"|"+c] = true
		}
	}
	out := all[:0]
	for i, c := range all {
		if keep[i] {
			out = append(out, c)
		}
	}
	return strings.Join(out, " ")
}

// splitVariants splits a class into its variants (hover:, md:, [&>svg]:…),
// sorted so their order doesn't matter, and the utility after them.
func splitVariants(class string) (string, string) {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(class); i++ {
		switch class[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ':':
			if depth == 0 {
				parts = append(parts, class[start:i])
				start = i + 1
			}
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, ":"), class[start:]
}

// classGroup is the group a utility sets and the groups it covers, so that
// an earlier class of those is dropped too; "" for a class cn doesn't know.
func classGroup(base string) (string, []string) {
	base = strings.TrimPrefix(base, "-") // -mt-2
	name, value, _ := strings.Cut(base, "-")
	switch name {
	case "p", "px", "py", "pt", "pr", "pb", "pl", "ps", "pe",
		"m", "mx", "my", "mt", "mr", "mb", "ml", "ms", "me":
		if value == "" {
			return "", nil
		}
		return name, sideCovers[name[1:]](name[:1])
	case "rounded":
		side, _, _ := strings.Cut(value, "-")
		if value == "" || corners[side] == nil {
			return "rounded", corners[""]
		}
		return "rounded-" + side, corners[side]
	case "bg":
		if bgColor(value) {
			return "bg-color", nil
		}
	case "text":
		return textGroup(value), nil
	case "font":
		switch {
		case fontWeights[value] || strings.HasPrefix(value, "[") && isDigit(value[1:]):
			return "font-weight", nil
		case value == "sans" || value == "serif" || value == "mono":
			return "font-family", nil
		}
	case "w", "h":
		if value != "" {
			return name, nil
		}
	case "size":
		if value != "" {
			return "size", []string{"w", "h"}
		}
	case "gap":
		switch {
		case strings.HasPrefix(value, "x-"):
			return "gap-x", nil
		case strings.HasPrefix(value, "y-"):
			return "gap-y", nil
		case value != "":
			return "gap", []string{"gap-x", "gap-y"}
		}
	}
	if displays[base] {
		return "display", nil
	}
	return "", nil
}

// sideCovers are, for a padding or margin side, the sides it covers: p
// covers every side, px the left and the right.
var sideCovers = map[string]func(string) []string{
	"":  func(p string) []string { return prefixed(p, "x", "y", "t", "r", "b", "l", "s", "e") },
	"x": func(p string) []string { return prefixed(p, "r", "l", "s", "e") },
	"y": func(p string) []string { return prefixed(p, "t", "b") },
	"t": none, "r": none, "b": none, "l": none, "s": none, "e": none,
}

func none(string) []string { return nil }

func prefixed(p string, sides ...string) []string {
	out := make([]string, len(sides))
	for i, s := range sides {
		out[i] = p + s
	}
	return out
}

// corners are the groups rounded-<side> covers: rounded (the "" key) every
// side and corner, rounded-t the top corners.
var corners = map[string][]string{
	"":   {"rounded-t", "rounded-r", "rounded-b", "rounded-l", "rounded-s", "rounded-e", "rounded-tl", "rounded-tr", "rounded-br", "rounded-bl", "rounded-ss", "rounded-se", "rounded-es", "rounded-ee"},
	"t":  {"rounded-tl", "rounded-tr"},
	"r":  {"rounded-tr", "rounded-br"},
	"b":  {"rounded-br", "rounded-bl"},
	"l":  {"rounded-tl", "rounded-bl"},
	"s":  {"rounded-ss", "rounded-es"},
	"e":  {"rounded-se", "rounded-ee"},
	"tl": {}, "tr": {}, "br": {}, "bl": {}, "ss": {}, "se": {}, "es": {}, "ee": {},
}

// bgColor reports whether bg-<value> sets the background colour rather
// than its image, size, position, repeat, attachment, clip or origin.
func bgColor(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "[") {
		return !strings.Contains(value, "url(") && !strings.Contains(value, "gradient(")
	}
	switch value {
	case "auto", "cover", "contain", "fixed", "local", "scroll", "none", "center", "top", "bottom", "left", "right",
		"left-top", "left-bottom", "right-top", "right-bottom", "top-left", "top-right", "bottom-left", "bottom-right",
		"repeat", "no-repeat", "repeat-x", "repeat-y", "repeat-round", "repeat-space", "radial", "conic":
		return false
	}
	for _, p := range []string{"clip-", "origin-", "linear-", "radial-", "conic-", "gradient-", "size-", "position-", "repeat-", "blend-"} {
		if strings.HasPrefix(value, p) {
			return false
		}
	}
	return true
}

// textGroup is what text-<value> sets: its size, alignment, wrapping,
// overflow, or else its colour.
func textGroup(value string) string {
	size, _, _ := strings.Cut(value, "/") // text-sm/6: a size and its line height
	switch {
	case textSizes[size]:
		return "text-size"
	case strings.HasPrefix(value, "[length:") || strings.HasPrefix(value, "[") && isDigit(value[1:]):
		return "text-size"
	}
	switch value {
	case "":
		return ""
	case "left", "center", "right", "justify", "start", "end":
		return "text-align"
	case "wrap", "nowrap", "balance", "pretty":
		return "text-wrap"
	case "ellipsis", "clip":
		return "text-overflow"
	}
	return "text-color"
}

func isDigit(s string) bool { return s != "" && (s[0] >= '0' && s[0] <= '9' || s[0] == '.') }

var textSizes = map[string]bool{"xs": true, "sm": true, "base": true, "lg": true, "xl": true, "2xl": true, "3xl": true,
	"4xl": true, "5xl": true, "6xl": true, "7xl": true, "8xl": true, "9xl": true}

var fontWeights = map[string]bool{"thin": true, "extralight": true, "light": true, "normal": true, "medium": true,
	"semibold": true, "bold": true, "extrabold": true, "black": true}

var displays = map[string]bool{"block": true, "inline-block": true, "inline": true, "flex": true, "inline-flex": true,
	"grid": true, "inline-grid": true, "contents": true, "hidden": true, "table": true, "inline-table": true,
	"table-row": true, "table-cell": true, "flow-root": true, "list-item": true}
