package transpile

import "strings"

// htmlElements are the element names of HTML (with its obsolete ones), SVG
// and MathML. A lowercase tag naming one, or holding a hyphen (a custom
// element), is markup; any other lowercase tag is a template or function
// of the package.
var htmlElements = setOf(`
a abbr acronym address applet area article aside audio b base basefont bdi bdo big blink blockquote
body br button canvas caption center cite code col colgroup data datalist dd del details dfn dialog
dir div dl dt em embed fieldset figcaption figure font footer form frame frameset h1 h2 h3 h4 h5 h6
head header hgroup hr html i iframe image img input ins kbd keygen label legend li link listing main
map mark marquee math menu menuitem meta meter nav nobr noembed noframes noscript object ol optgroup
option output p param picture plaintext portal pre progress q rb rp rt rtc ruby s samp script search
section select selectedcontent slot small source span strike strong style sub summary sup svg table
tbody td template textarea tfoot th thead time title tr track tt u ul var video wbr xmp
animate animateMotion animateTransform circle clipPath defs desc discard ellipse feBlend
feColorMatrix feComponentTransfer feComposite feConvolveMatrix feDiffuseLighting feDisplacementMap
feDistantLight feDropShadow feFlood feFuncA feFuncB feFuncG feFuncR feGaussianBlur feImage feMerge
feMergeNode feMorphology feOffset fePointLight feSpecularLighting feSpotLight feTile feTurbulence
filter foreignObject g line linearGradient marker mask metadata mpath path pattern polygon polyline
radialGradient rect set stop switch symbol text textPath tspan use view
annotation annotation-xml maction menclose merror mfrac mglyph mi mlabeledtr mmultiscripts mn mo
mover mpadded mphantom mprescripts mroot mrow ms mspace msqrt mstyle msub msubsup msup mtable mtd
mtext mtr munder munderover semantics
`)

func setOf(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(s) {
		m[f] = true
	}
	return m
}

// IsElement reports whether a tag is markup's rather than a component's:
// an HTML, SVG or MathML element, or a custom element (it holds a hyphen),
// or a namespaced one (slot:x).
func IsElement(tag string) bool {
	return htmlElements[tag] || strings.ContainsAny(tag, "-:")
}

// IsElementName reports whether name is an HTML, SVG or MathML element's:
// a template of the package can't have it, its tag being the element.
func IsElementName(name string) bool { return htmlElements[name] }
