package main

import (
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/htmldata"
)

// What the editor knows of HTML and SVG for markup in .vuka files: each
// element with a one-line description and the attributes it takes beyond the
// global ones, each attribute with its own line, and the values of the
// enumerated ones. Descriptions are short summaries; the links go to MDN.

// htmlElementData is name, description, attributes: one element a line.
const htmlElementData = htmldata.Elements

// svgElementData is the SVG most pages draw with, in the same form.
const svgElementData = htmldata.SVGElements

// htmlGlobalAttrs are the attributes every element takes.
var htmlGlobalAttrs = strings.Fields(htmldata.GlobalAttrs)

// htmlAttrData is name, description; el.name for an element's own meaning.
const htmlAttrData = htmldata.Attrs

// htmlValueData is name, values; el.name for an element's own.
const htmlValueData = htmldata.Values

// htmlAriaAttrs are the ARIA attributes offered on every element.
const htmlAriaData = htmldata.Aria

// htmlEventData is the event handlers offered on an element, onXxx, and what
// fires them.
const htmlEventData = htmldata.Events

// htmlBooleans are the attributes written bare: present is true.
var htmlBooleans = setOf(htmldata.Booleans)

type htmlElement struct {
	name, doc string
	svg       bool
	attrs     []string
}

var (
	htmlElements   = map[string]*htmlElement{}
	htmlElemNames  []string // HTML, then SVG, each sorted
	htmlAttrDocs   = tabMap(htmlAttrData)
	htmlAriaDocs   = tabMap(htmlAriaData)
	htmlEventDocs  = tabMap(htmlEventData)
	htmlAriaNames  = tabKeys(htmlAriaData)
	htmlEventNames = tabKeys(htmlEventData)
	htmlValues     = map[string][]string{}
)

func init() {
	for _, src := range []struct {
		data string
		svg  bool
	}{{htmlElementData, false}, {svgElementData, true}} {
		var names []string
		for _, line := range strings.Split(strings.TrimSpace(src.data), "\n") {
			f := strings.Split(line, "\t")
			e := &htmlElement{name: f[0], doc: f[1], svg: src.svg}
			if len(f) > 2 {
				e.attrs = strings.Fields(f[2])
			}
			if _, dup := htmlElements[e.name]; !dup {
				htmlElements[e.name] = e
				names = append(names, e.name)
			}
		}
		sort.Strings(names)
		htmlElemNames = append(htmlElemNames, names...)
	}
	for k, v := range tabMap(htmlValueData) {
		htmlValues[k] = strings.Fields(v)
	}
}

func setOf(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(s) {
		m[f] = true
	}
	return m
}

func tabMap(data string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if k, v, ok := strings.Cut(line, "\t"); ok {
			m[k] = v
		}
	}
	return m
}

func tabKeys(data string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		k, _, _ := strings.Cut(line, "\t")
		out = append(out, k)
	}
	return out
}

// elementURL is an element's MDN page.
func elementURL(e *htmlElement) string {
	if e.svg {
		return "https://developer.mozilla.org/docs/Web/SVG/Element/" + e.name
	}
	if len(e.name) == 2 && e.name[0] == 'h' && '1' <= e.name[1] && e.name[1] <= '6' {
		return "https://developer.mozilla.org/docs/Web/HTML/Element/Heading_Elements"
	}
	return "https://developer.mozilla.org/docs/Web/HTML/Element/" + e.name
}

// elementDoc is an element's description, as Markdown.
func elementDoc(e *htmlElement) string {
	return e.doc + "\n\n[MDN Reference](" + elementURL(e) + ")"
}

// attrInfo is what the editor says of an attribute on an element: its
// description and MDN page; false for an attribute it doesn't know.
func attrInfo(tag, attr string) (doc, url string, ok bool) {
	e := htmlElements[tag]
	global := false
	for _, g := range htmlGlobalAttrs {
		global = global || g == attr
	}
	switch {
	case htmlEventDocs[attr] != "":
		ev := strings.ToLower(attr[2:])
		api := "Element"
		switch ev {
		case "input", "change", "load", "error", "toggle", "invalid", "dragstart", "dragover", "drop":
			api = "HTMLElement"
		case "submit", "reset":
			api = "HTMLFormElement"
		case "close", "cancel":
			api = "HTMLDialogElement"
		case "play", "pause", "ended", "timeupdate", "volumechange", "loadeddata", "canplay", "seeked":
			api = "HTMLMediaElement"
		case "select", "beforetoggle":
			api = "HTMLElement"
		}
		return htmlEventDocs[attr] + " The handler runs on the server (vuka.On).", "https://developer.mozilla.org/docs/Web/API/" + api + "/" + ev + "_event", true
	case htmlAriaDocs[attr] != "":
		return htmlAriaDocs[attr], "https://developer.mozilla.org/docs/Web/Accessibility/ARIA/Attributes/" + attr, true
	case strings.HasPrefix(attr, "data-"):
		camel := []byte(attr[5:])
		for i := 0; i < len(camel); i++ {
			if camel[i] == '-' && i+1 < len(camel) {
				camel = append(camel[:i], camel[i+1:]...)
				camel[i] = byte(strings.ToUpper(string(camel[i]))[0])
			}
		}
		return "Custom data, read by scripts as element.dataset." + string(camel) + ".", "https://developer.mozilla.org/docs/Web/HTML/Global_attributes/data-*", true
	}
	doc = htmlAttrDocs[tag+"."+attr]
	if doc == "" {
		doc = htmlAttrDocs[attr]
	}
	if doc == "" {
		return "", "", false
	}
	html := map[string]string{"className": "class", "htmlFor": "for"}[attr]
	if html == "" {
		html = attr
	}
	switch {
	case attr == "key" || attr == "ref":
		return doc, "", true
	case attr == "role":
		return doc, "https://developer.mozilla.org/docs/Web/Accessibility/ARIA/Roles", true
	case global:
		return doc, "https://developer.mozilla.org/docs/Web/HTML/Global_attributes/" + html, true
	case e != nil && e.svg:
		return doc, "https://developer.mozilla.org/docs/Web/SVG/Attribute/" + html, true
	case e != nil:
		return doc, elementURL(e) + "#" + html, true
	}
	return doc, "", true
}

// attrDoc is an attribute's description, as Markdown.
func attrDoc(tag, attr string) string {
	doc, url, ok := attrInfo(tag, attr)
	if !ok {
		return ""
	}
	if vs := attrValues(tag, attr); len(vs) > 0 && len(vs) <= 12 {
		doc += "\n\nValues: `" + strings.Join(vs, "` `") + "`"
	}
	if url != "" {
		doc += "\n\n[MDN Reference](" + url + ")"
	}
	return doc
}

// attrValues are the values an enumerated attribute takes.
func attrValues(tag, attr string) []string {
	if v, ok := htmlValues[tag+"."+attr]; ok {
		return v
	}
	return htmlValues[attr]
}

// elementAttrs are the attributes offered on an element: its own, then the
// global ones (fewer on SVG), the ARIA ones and the event handlers.
func elementAttrs(tag string) (own, global []string) {
	e := htmlElements[tag]
	if e != nil {
		own = e.attrs
	}
	if e != nil && e.svg {
		return own, []string{"id", "className", "style", "key"}
	}
	return own, htmlGlobalAttrs
}
