package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Completion while writing markup, beyond the elements and attributes of
// lsp_html.go: every ARIA attribute and role with their values, the event
// handlers each element fires, ref names for ui.Ref("…"), and a loop's first
// element keyed by its item's ID.

// htmlAriaMoreData is the ARIA attributes past the common ones, name and
// description.
const htmlAriaMoreData = `
aria-atomic	Whether a change in a live region is announced whole.
aria-relevant	Which changes in a live region are announced.
aria-activedescendant	The id of the focused descendant of a composite widget.
aria-level	The level of a heading or a tree item.
aria-posinset	The item's position in its set.
aria-setsize	The number of items in the set.
aria-readonly	The value can't be changed.
aria-multiline	The text box takes several lines.
aria-multiselectable	Several items can be selected.
aria-placeholder	A hint shown while the field is empty.
aria-keyshortcuts	Keyboard shortcuts that activate or focus the element.
aria-details	The id of the element that details this one.
aria-errormessage	The id of the element holding this one's error message.
aria-description	A description of the element, read by assistive technologies.
aria-colcount	The number of columns of a table or grid.
aria-colindex	The element's column index.
aria-colspan	The number of columns a cell spans.
aria-rowcount	The number of rows of a table or grid.
aria-rowindex	The element's row index.
aria-rowspan	The number of rows a cell spans.
aria-flowto	The ids of the next elements in another reading order.
`

// htmlAriaValueData is the values of the ARIA attributes the common ones
// leave out.
const htmlAriaValueData = `
aria-atomic	true false
aria-relevant	additions removals text all
aria-readonly	true false
aria-multiline	true false
aria-multiselectable	true false
`

// ariaRoles are the roles of WAI-ARIA 1.2 the common list leaves out.
var ariaRoles = strings.Fields(`article blockquote caption code definition deletion document emphasis feed generic group
	insertion marquee math menuitemcheckbox menuitemradio meter note paragraph radiogroup rowgroup scrollbar searchbox
	spinbutton strong subscript superscript term time timer toolbar treegrid application`)

// htmlEventMoreData is the event handlers past the common ones.
const htmlEventMoreData = `
onAuxClick	A click with a button other than the primary one.
onBeforeInput	A change of an input's value, before it is made.
onSelect	Text was selected in an input or a textarea.
onPointerMove	A pointer moved over the element.
onPointerEnter	A pointer entered the element.
onPointerLeave	A pointer left the element.
onPointerCancel	The browser took over a pointer (a scroll, a gesture).
onTouchMove	A touch moved on the element.
onTouchCancel	A touch was interrupted.
onDrag	The element is dragged.
onDragEnd	A drag of the element ended.
onDragEnter	Something dragged entered the element.
onDragLeave	Something dragged left the element.
onAnimationEnd	A CSS animation finished.
onTransitionEnd	A CSS transition finished.
onBeforeToggle	A details or popover is about to open or close.
onClose	A dialog closed.
onCancel	A dialog was dismissed (Escape).
onPlay	Playback started.
onPause	Playback paused.
onEnded	Playback reached the end.
onTimeUpdate	The playback position changed.
onVolumeChange	The volume or muting changed.
onLoadedData	The first frame of the media loaded.
onCanPlay	The media can start playing.
onSeeked	A seek finished.
`

// eventTags are the elements an event handler is offered on; an event not
// here is offered on every element.
var eventTags = map[string][]string{}

func init() {
	for k, v := range tabMap(htmlAriaMoreData) {
		htmlAriaDocs[k] = v
	}
	htmlAriaNames = append(htmlAriaNames, tabKeys(htmlAriaMoreData)...)
	for k, v := range tabMap(htmlAriaValueData) {
		htmlValues[k] = strings.Fields(v)
	}
	htmlValues["role"] = append(htmlValues["role"], ariaRoles...)
	for k, v := range tabMap(htmlEventMoreData) {
		htmlEventDocs[k] = v
	}
	htmlEventNames = append(htmlEventNames, tabKeys(htmlEventMoreData)...)
	htmlAttrDocs["ref"] = `Names the element for ui.Ref("…") in the same component instance (vuka).`
	media := strings.Fields("audio video")
	loads := strings.Fields("img iframe script link style video audio object embed body input track source")
	for ev, tags := range map[string][]string{
		"onSubmit": {"form"}, "onReset": {"form"},
		"onClose": {"dialog"}, "onCancel": {"dialog", "input"},
		"onToggle": {"details", "dialog"}, "onBeforeToggle": {"details", "dialog"},
		"onInvalid": {"input", "select", "textarea"},
		"onSelect":  {"input", "textarea"},
		"onLoad":    loads, "onError": loads,
		"onPlay": media, "onPause": media, "onEnded": media, "onTimeUpdate": media,
		"onVolumeChange": media, "onLoadedData": media, "onCanPlay": media, "onSeeked": media,
	} {
		eventTags[ev] = tags
	}
}

// eventOn reports whether an element fires an event: a popover toggles too.
func eventOn(ev string, tag jsxTag) bool {
	tags, ok := eventTags[ev]
	if !ok {
		return true
	}
	for _, t := range tags {
		if t == tag.name {
			return true
		}
	}
	if ev == "onToggle" || ev == "onBeforeToggle" {
		for _, a := range tag.attrs {
			if a.name == "popover" {
				return true
			}
		}
	}
	return false
}

// eventForms is what an event item says of the handlers vuka takes.
const eventForms = "\n\nThe handler: a method (`{Save}`), a call (`{Add(2)}`), browser state (`{open = !open}`), " +
	"a func (`{func() { … }}`) or a `ui.JS(…)` command. Any case names the event: `onKeyDown`, `onkeydown`."

// authoringRequest answers a completion this file knows better than the rest:
// a ref name in ui.Ref("…"), a loop typed on a markup line, the first element
// of a loop, and an element's attributes. The answer runs without p.mu held.
func (p *proxy) authoringRequest(method string, vf *vfile, genPath string, off int) func() any {
	if method != "textDocument/completion" {
		return nil
	}
	src := vf.text()
	if from, ok := refSpot(src, off); ok {
		return func() any { return refItems(src, from, off) }
	}
	if from, ok := forSpot(src, off); ok && isShape(vf) {
		return func() any { return forItems(src, from, off) }
	}
	spot, ok := completionSpot(src, off)
	if !ok {
		return nil
	}
	switch {
	case spot.kind == "tag" && spot.qual == "":
		loop, ok := loopHead(src, spot.lt)
		if !ok || !tagEndsAt(src, off) {
			return nil
		}
		return func() any {
			v := p.completeJSX(vf, genPath, spot, off)
			if field := p.loopKey(vf, genPath, loop); field != "" {
				withLoopKey(v, loop.item+"."+field)
			}
			return v
		}
	case spot.kind == "attr" && !isComponentTag(spot.tag.name):
		return func() any { return withShapeAttrs(p.completeJSX(vf, genPath, spot, off), spot, src, off) }
	}
	return nil
}

// refSpot is, for an offset in the string of a ui.Ref("…") argument, where
// the string's text starts.
func refSpot(src []byte, off int) (int, bool) {
	ls := bytes.LastIndexByte(src[:off], '\n') + 1
	q := -1
	for i := ls; i < off; i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			if q < 0 {
				q = i
			} else {
				q = -1
			}
		}
	}
	if q < 0 || !refCall.Match(src[ls:q]) {
		return 0, false
	}
	return q + 1, true
}

var refCall = regexp.MustCompile(`\bRef\(\s*(?:"[^"\n]*"\s*,\s*)*$`)

// refAttr is a ref="…" attribute naming an element.
var refAttr = regexp.MustCompile(`\sref=(?:"([^"{}\n]+)"|\{"([^"\n]+)"\})`)

// refItems are the refs the file's markup declares.
func refItems(src []byte, from, off int) any {
	typed := lspRange{positionOf(src, from), positionOf(src, off)}
	items := []any{}
	seen := map[string]bool{}
	for _, m := range refAttr.FindAllSubmatch(src, -1) {
		name := string(m[1]) + string(m[2])
		if seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, map[string]any{"label": name, "kind": 18, "detail": `ref="` + name + `"`,
			"sortText": fmt.Sprintf("%03d", len(items)), "textEdit": map[string]any{"range": typed, "newText": name}})
	}
	return map[string]any{"isIncomplete": false, "items": items}
}

// forSpot is, for a markup line holding only what may start a for loop
// (f, fo, for), where it starts.
func forSpot(src []byte, off int) (int, bool) {
	ls := bytes.LastIndexByte(src[:off], '\n') + 1
	le := bytes.IndexByte(src[off:], '\n')
	if le < 0 {
		le = len(src) - off
	}
	w := strings.TrimLeft(string(src[ls:off]), " \t")
	if w == "" || !strings.HasPrefix("for", w) || strings.TrimSpace(string(src[off:off+le])) != "" {
		return 0, false
	}
	from := off - len(w)
	if !inMarkupBody(src, from) {
		return 0, false
	}
	return from, true
}

// inMarkupBody reports whether off is inside the shape's markup, among an
// element's children: past the header, out of any tag and {…}.
func inMarkupBody(src []byte, off int) bool {
	if sp, ok := splitShape(src); !ok || off < sp.markup {
		return false
	}
	s := scanMarkup(src)
	if _, ok := s.unclosedAt(off); !ok {
		return false
	}
	for _, b := range s.braces {
		if b[0] < off && off <= b[1] {
			return false
		}
	}
	return true
}

// forItems is the loop snippet: its first element keyed by the item's ID.
func forItems(src []byte, from, off int) any {
	typed := lspRange{positionOf(src, from), positionOf(src, off)}
	text := "for _, ${1:item} := range ${2:items} {\n\t<${3:li} key={${1:item}.${4:ID}}>$0</${3:li}>\n}"
	return map[string]any{"isIncomplete": false, "items": []any{map[string]any{
		"label": "for", "kind": 15, "detail": "for loop: an element per item, keyed", "filterText": "for", "sortText": "!0",
		"insertTextFormat": 2, "textEdit": map[string]any{"range": typed, "newText": text},
		"documentation": map[string]any{"kind": "markdown", "value": "A keyed list: a patch matches each item by its key, " +
			"so state stays with its row ([W0102](https://vuka-lang.github.io/errors/W0102))."},
	}}}
}

// loopHeader is a range loop's head on a line of markup.
var loopHeader = regexp.MustCompile(`^\s*\{?for\s+(?:([A-Za-z_]\w*)\s*,\s*)?([A-Za-z_]\w*)\s*:?=\s*range\s+(.+?)\s*\{\s*$`)

type loopAt struct {
	item  string // the value's variable
	brace int    // the offset after the loop's {
}

// loopHead is the range loop whose body starts with the tag at lt: the
// previous line holds its head, and the loop names its value.
func loopHead(src []byte, lt int) (loopAt, bool) {
	ls := bytes.LastIndexByte(src[:lt], '\n') + 1
	if strings.TrimSpace(string(src[ls:lt])) != "" || ls == 0 {
		return loopAt{}, false
	}
	pe := ls - 1
	ps := bytes.LastIndexByte(src[:pe], '\n') + 1
	m := loopHeader.FindSubmatch(src[ps:pe])
	if m == nil {
		return loopAt{}, false
	}
	item := string(m[2])
	if len(m[1]) == 0 {
		return loopAt{}, false // for i := range: no item
	}
	if item == "_" {
		return loopAt{}, false
	}
	return loopAt{item: item, brace: ps + bytes.LastIndexByte(src[ps:pe], '{') + 1}, true
}

// tagEndsAt reports whether a tag's name being typed ends the tag so far:
// nothing but its end follows on the line.
func tagEndsAt(src []byte, off int) bool {
	le := bytes.IndexByte(src[off:], '\n')
	if le < 0 {
		le = len(src) - off
	}
	rest := strings.TrimSpace(string(src[off : off+le]))
	return rest == "" || rest[0] == '>' || strings.HasPrefix(rest, "/>")
}

// idFields are the names of a field that identifies an item, best first.
var idFields = []string{"ID", "Id", "Key", "UUID", "Slug", "Code"}

// loopKey is the field of the loop's item that keys it, or "": asked of gopls
// at the loop's body in the generated Go.
func (p *proxy) loopKey(vf *vfile, genPath string, loop loopAt) string {
	src := vf.text()
	gp := vf.toGen(positionOf(src, loop.brace))
	fields := map[string]bool{}
	var suffixed []string
	for _, it := range p.probeAt(genPath, gp, "\n_ = "+loop.item+".") {
		label, _ := it["label"].(string)
		if kind, _ := it["kind"].(float64); kind == 5 && isExported(label) && !strings.Contains(label, ".") {
			fields[label] = true
			if strings.HasSuffix(label, "ID") {
				suffixed = append(suffixed, label)
			}
		}
	}
	for _, f := range idFields {
		if fields[f] {
			return f
		}
	}
	if len(suffixed) > 0 {
		return suffixed[0]
	}
	return ""
}

// probeAt asks gopls to complete code typed into the generated Go at gp; the
// file is put back after.
func (p *proxy) probeAt(genPath string, gp lspPosition, insert string) []map[string]any {
	p.genMu.Lock()
	defer p.genMu.Unlock()
	p.mu.Lock()
	vf := p.vfileOf(genPath)
	if vf == nil || vf.pending {
		p.mu.Unlock()
		return nil
	}
	at := offsetOf(vf.gen, gp)
	text := string(vf.gen[:at]) + insert
	pos := positionOf([]byte(text), len(text))
	text += string(vf.gen[at:])
	vf.version += 2
	vf.probed = vf.version - 1
	v, gen := vf.version, string(vf.gen)
	p.mu.Unlock()
	uri := pathToURI(genPath)
	change := func(version int, s string) {
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{map[string]any{"text": s}}}})
	}
	change(v-1, text)
	res, ok := p.ask("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos})
	change(v, gen)
	if !ok {
		return nil
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(res, &list) != nil {
		_ = json.Unmarshal(res, &list.Items)
	}
	return list.Items
}

// withLoopKey makes each element and component a tag completion offers open
// with the loop's key.
func withLoopKey(v any, key string) {
	list, _ := v.(map[string]any)
	items, _ := list["items"].([]any)
	attr := " key={" + key + "}"
	for _, it := range items {
		m, _ := it.(map[string]any)
		switch m["kind"] {
		case 14, 3, 7:
		default:
			continue
		}
		te, _ := m["textEdit"].(map[string]any)
		text, _ := te["newText"].(string)
		te["newText"] = text + attr
		m["labelDetails"] = map[string]any{"detail": attr}
	}
}

// withShapeAttrs adjusts an element's attribute completion: the event
// handlers it fires, with the handler forms, and ref.
func withShapeAttrs(v any, spot jsxSpot, src []byte, off int) any {
	list, _ := v.(map[string]any)
	items, _ := list["items"].([]any)
	kept := items[:0]
	used := map[string]bool{}
	for _, a := range spot.tag.attrs {
		if off < a.start || off > a.end {
			used[a.name] = true
		}
	}
	for _, it := range items {
		m, _ := it.(map[string]any)
		label, _ := m["label"].(string)
		if m["kind"] == 23 {
			if !eventOn(label, spot.tag) {
				continue
			}
			if d, ok := m["documentation"].(map[string]any); ok {
				d["value"] = strings.Replace(d["value"].(string), "\n\n[MDN", eventForms+"\n\n[MDN", 1)
			}
		}
		kept = append(kept, it)
	}
	if !used["ref"] {
		kept = append(kept, map[string]any{"label": "ref", "kind": 5, "sortText": "1999", "filterText": "ref",
			"detail": `a name for ui.Ref("…")`, "insertTextFormat": 2,
			"textEdit":      map[string]any{"range": lspRange{positionOf(src, spot.from), positionOf(src, off)}, "newText": `ref="$1"`},
			"documentation": map[string]any{"kind": "markdown", "value": attrDoc(spot.tag.name, "ref")}})
	}
	list["items"] = kept
	return list
}
