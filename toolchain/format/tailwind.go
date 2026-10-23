package format

import (
	"regexp"
	"sort"
	"strings"
)

// Tailwind orders a class list's utilities the way Tailwind's stylesheet
// orders their rules, as its Prettier plugin does, close enough that a list
// sorted either way reads the same: classes it doesn't know first, as
// written; then utilities by the property they set (layout, box, flex and
// grid, spacing, sizing, typography, backgrounds, borders, effects,
// transitions, transforms); then those under a variant (hover:, md:), by
// variant, then likewise. The order is stable: utilities it ranks the same
// keep theirs.
func Tailwind(class string) string {
	fields := strings.Fields(class)
	if len(fields) < 2 {
		return class
	}
	type item struct {
		s       string
		variant int
		rank    int
		known   bool
		i       int
	}
	items := make([]item, len(fields))
	for i, f := range fields {
		v, base := splitVariant(f)
		r, ok := utilityRank(base)
		vr := 0
		if v != "" {
			vr = variantRank(v)
		}
		items[i] = item{f, vr, r, ok, i}
	}
	sort.SliceStable(items, func(a, b int) bool {
		x, y := items[a], items[b]
		if x.known != y.known {
			return !x.known
		}
		if !x.known {
			return x.i < y.i
		}
		if x.variant != y.variant {
			return x.variant < y.variant
		}
		return x.rank < y.rank
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.s
	}
	return strings.Join(out, " ")
}

// splitVariant splits md:hover:p-2 into md:hover and p-2, past any
// arbitrary value's brackets.
func splitVariant(c string) (string, string) {
	depth, last := 0, -1
	for i := 0; i < len(c); i++ {
		switch c[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ':':
			if depth == 0 {
				last = i
			}
		}
	}
	if last < 0 {
		return "", c
	}
	return c[:last], c[last+1:]
}

var variants = []string{
	"*", "first", "last", "only", "odd", "even", "first-of-type", "last-of-type", "empty", "visited", "target",
	"open", "default", "checked", "indeterminate", "placeholder-shown", "autofill", "optional", "required",
	"valid", "invalid", "in-range", "out-of-range", "read-only", "focus-within", "hover", "focus", "focus-visible",
	"active", "enabled", "disabled", "group", "peer", "has", "aria", "data", "before", "after", "placeholder",
	"file", "marker", "selection", "first-line", "first-letter", "backdrop", "motion-safe", "motion-reduce",
	"contrast-more", "contrast-less", "print", "portrait", "landscape", "rtl", "ltr", "sm", "md", "lg", "xl", "2xl",
	"max-sm", "max-md", "max-lg", "max-xl", "max-2xl", "dark",
}

// variantRank ranks a variant chain by its first variant's place among
// Tailwind's; one it doesn't know comes after them.
func variantRank(v string) int {
	name := v
	if i := strings.IndexByte(v, ':'); i >= 0 {
		name = v[:i]
	}
	name = strings.TrimPrefix(name, "@")
	for k := 0; k < 2; k++ {
		for j, n := range variants {
			if n == name {
				return 1 + j
			}
		}
		i := strings.IndexAny(name, "-/[")
		if i <= 0 {
			break
		}
		name = name[:i]
	}
	return len(variants) + 1
}

// utilityGroups are the utilities by the properties they set, in the order
// Tailwind writes them: each a pattern of class names.
var utilityGroups = func() []*regexp.Regexp {
	pats := []string{
		`container`, `sr-only|not-sr-only`, `pointer-events-.*`, `visible|invisible|collapse`,
		`static|fixed|absolute|relative|sticky`, `inset(-[xy])?-.*|top-.*|right-.*|bottom-.*|left-.*|start-.*|end-.*`,
		`isolate|isolation-auto`, `z-.*`, `order-.*`, `col-.*`, `row-.*`, `float-.*`, `clear-.*`,
		`m-.*|mx-.*|my-.*|mt-.*|mr-.*|mb-.*|ml-.*|ms-.*|me-.*`, `box-(border|content)`, `line-clamp-.*`,
		`block|inline-block|inline|flex|inline-flex|table|inline-table|table-.*|flow-root|grid|inline-grid|contents|list-item|hidden`,
		`aspect-.*`, `size-.*`, `h-.*`, `max-h-.*`, `min-h-.*`, `w-.*`, `min-w-.*`, `max-w-.*`,
		`flex-(1|auto|initial|none)`, `shrink(-.*)?|flex-shrink(-.*)?`, `grow(-.*)?|flex-grow(-.*)?`, `basis-.*`,
		`border-(collapse|separate)|border-spacing-.*`, `origin-.*`, `-?translate-.*`, `-?scale-.*`, `-?rotate-.*`,
		`-?skew-.*`, `transform(-.*)?`, `animate-.*`, `cursor-.*`, `touch-.*`, `select-.*`, `resize(-.*)?`,
		`snap-.*`, `scroll-[mp].*`, `list-.*`, `appearance-.*`, `columns-.*`, `break-(before|after|inside)-.*`,
		`auto-cols-.*`, `grid-flow-.*`, `auto-rows-.*`, `grid-cols-.*`, `grid-rows-.*`,
		`flex-(row|col|row-reverse|col-reverse)`, `flex-(wrap|wrap-reverse|nowrap)`, `place-content-.*`,
		`place-items-.*`, `content-.*`, `items-.*`, `justify-.*`, `gap-.*`, `space-[xy]-.*`, `divide-.*`,
		`place-self-.*`, `self-.*`, `justify-self-.*`, `overflow-.*`, `overscroll-.*`, `scroll-(auto|smooth)`,
		`truncate|text-ellipsis|text-clip`, `whitespace-.*`, `text-(wrap|nowrap|balance|pretty)`,
		`break-(normal|words|all|keep)`, `rounded(-.*)?`, `border(-[xytrblse])?(-\d+|-\[.*\])?`,
		`border-(solid|dashed|dotted|double|hidden|none)`, `border(-[xytrblse])?-.*`, `bg-.*`,
		`from-.*|via-.*|to-.*`, `fill-.*`, `stroke-.*`, `object-.*`,
		`p-.*|px-.*|py-.*|pt-.*|pr-.*|pb-.*|pl-.*|ps-.*|pe-.*`,
		`text-(left|center|right|justify|start|end)`, `indent-.*`, `align-.*`, `font-(sans|serif|mono)`,
		`text-(xs|sm|base|lg|xl|[2-9]xl)(/.*)?`, `font-.*`, `uppercase|lowercase|capitalize|normal-case`,
		`italic|not-italic`, `(normal|ordinal|slashed-zero|lining|oldstyle|proportional|tabular|diagonal|stacked)-nums|ordinal|slashed-zero`,
		`tracking-.*`, `leading-.*`, `text-.*`, `underline|overline|line-through|no-underline`, `decoration-.*`,
		`underline-offset-.*`, `antialiased|subpixel-antialiased`, `placeholder-.*`, `caret-.*`, `accent-.*`,
		`opacity-.*`, `bg-blend-.*`, `mix-blend-.*`, `shadow(-.*)?`, `outline(-.*)?`, `ring(-.*)?`,
		`blur(-.*)?|brightness-.*|contrast-.*|drop-shadow(-.*)?|grayscale(-.*)?|hue-rotate-.*|invert(-.*)?|saturate-.*|sepia(-.*)?|filter(-.*)?`,
		`backdrop-.*`, `transition(-.*)?`, `delay-.*`, `duration-.*`, `ease-.*`, `will-change-.*`,
	}
	out := make([]*regexp.Regexp, len(pats))
	for i, p := range pats {
		out[i] = regexp.MustCompile(`^(?:` + p + `)$`)
	}
	return out
}()

// utilityRank is a utility's place, and whether it is one Tailwind has.
func utilityRank(u string) (int, bool) {
	u = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSuffix(u, "!"), "!"), "-")
	for i, g := range utilityGroups {
		if g.MatchString(u) {
			return i, true
		}
	}
	return 0, false
}
