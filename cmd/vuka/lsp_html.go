package main

import (
	"sort"
	"strings"
)

// What the editor knows of HTML and SVG for markup in .vuka files: each
// element with a one-line description and the attributes it takes beyond the
// global ones, each attribute with its own line, and the values of the
// enumerated ones. Descriptions are short summaries; the links go to MDN.

// htmlElementData is name, description, attributes: one element a line.
const htmlElementData = `
a	A hyperlink to a URL, a file, an email address or a place on the page.	href target rel download hreflang type referrerpolicy ping
abbr	An abbreviation or acronym; title spells it out.
address	Contact information for the nearest article or the page.
area	A clickable area inside an image map.	alt coords shape href target rel download referrerpolicy
article	A self-contained composition: a post, a card, a comment.
aside	Content only indirectly related to the main content: a sidebar, a callout.
audio	Embedded sound content.	src controls autoplay loop muted preload crossorigin
b	Text drawn to attention without extra importance.
base	The base URL and default target for every relative URL of the page.	href target
bdi	Text isolated from the bidirectional layout around it.
bdo	Overrides the text direction of its content.
blockquote	An extended quotation.	cite
body	The content of the document.
br	A line break.
button	A clickable button.	type disabled name value form formaction formmethod formenctype formnovalidate formtarget popovertarget popovertargetaction autofocus
canvas	A drawing surface for scripts (2D or WebGL).	width height
caption	The title of a table.
cite	The title of a creative work.
code	A fragment of computer code.
col	A column of a table, inside a colgroup.	span
colgroup	A group of columns of a table.	span
data	Content tied to a machine-readable value.	value
datalist	Options suggested to an input with a list attribute.
dd	The description of a term in a dl.
del	Text removed from the document.	cite datetime
details	A disclosure widget: its summary, and content shown when open.	open name
dfn	The defining instance of a term.
dialog	A dialog box or modal.	open
div	A generic block container.
dl	A description list of dt terms and dd descriptions.
dt	A term in a description list.
em	Stressed emphasis.
embed	External content embedded at this point.	src type width height
fieldset	A group of controls of a form, captioned by a legend.	disabled form name
figcaption	The caption of a figure.
figure	Self-contained content, such as an image, with an optional caption.
footer	The footer of its nearest section or of the page.
form	A form: controls submitted together.	action method enctype target autocomplete novalidate name accept-charset rel
h1	A level 1 section heading, the most important.
h2	A level 2 section heading.
h3	A level 3 section heading.
h4	A level 4 section heading.
h5	A level 5 section heading.
h6	A level 6 section heading, the least important.
head	Machine-readable information about the document.
header	Introductory content: a heading, a logo, navigation.
hgroup	A heading with its subheadings or taglines.
hr	A thematic break between paragraphs.
html	The root element of a document.	lang
i	Text set off from the rest: a term, a thought, a foreign phrase.
iframe	Another page embedded in this one.	src srcdoc name width height allow allowfullscreen loading referrerpolicy sandbox
img	An image.	src alt width height srcset sizes loading decoding fetchpriority crossorigin referrerpolicy usemap ismap
input	A form control; type picks which.	type name value placeholder disabled required readonly checked autofocus autocomplete min max step minlength maxlength pattern size multiple accept list form inputmode capture dirname formaction formmethod formenctype formnovalidate formtarget src alt width height popovertarget
ins	Text added to the document.	cite datetime
kbd	Text input by the user: keys, voice, commands.
label	A caption for a form control.	htmlFor form
legend	The caption of a fieldset.
li	An item of a list.	value
link	A link to an external resource: a stylesheet, an icon, a preload.	rel href type media sizes as crossorigin integrity hreflang referrerpolicy fetchpriority
main	The dominant content of the page.
map	An image map, with area elements.	name
mark	Text highlighted for reference.
menu	A list of commands; like ul.
meta	Metadata other elements can't hold.	name content charset http-equiv media
meter	A scalar value within a known range.	value min max low high optimum form
nav	A section of navigation links.
noscript	Content for when scripting is off.
object	An external resource: an image, a plugin, a nested page.	data type name width height form
ol	An ordered list.	reversed start type
optgroup	A group of options in a select.	label disabled
option	An item of a select, an optgroup or a datalist.	value label selected disabled
output	The result of a calculation or a user action.	htmlFor form name
p	A paragraph.
picture	Sources of an image for different displays, and an img fallback.
pre	Preformatted text, whitespace kept.
progress	The progress of a task.	value max
q	A short inline quotation.	cite
rp	Fallback parentheses for ruby annotations.
rt	The text of a ruby annotation.
ruby	Text with ruby annotations, for East Asian typography.
s	Text no longer accurate or relevant.
samp	Sample output of a program.
script	An embedded or linked script.	src type async defer crossorigin integrity nomodule referrerpolicy fetchpriority
search	A section for search or filtering controls.
section	A generic standalone section, usually with a heading.
select	A control picking from options.	name multiple required disabled size form autocomplete autofocus
slot	A placeholder in a web component's shadow tree.	name
small	Side comments and small print.
source	A media resource of a picture, audio or video.	src srcset type media sizes width height
span	A generic inline container.
strong	Content of strong importance.
style	Style information (CSS) for the document.	media
sub	Subscript text.
summary	The visible summary of a details element.
sup	Superscript text.
table	Tabular data: rows and columns of cells.
tbody	The body rows of a table.
td	A data cell of a table.	colspan rowspan headers
template	Content not rendered, held for scripts to clone.	shadowrootmode
textarea	A multi-line plain-text control.	name rows cols placeholder disabled required readonly minlength maxlength wrap autocomplete autofocus form dirname
tfoot	The summary rows at the foot of a table.
th	A header cell of a table.	colspan rowspan headers scope abbr
thead	The header rows of a table.
time	A time or date, with a machine-readable datetime.	datetime
title	The document's title, in the browser tab.
tr	A row of table cells.
track	A timed text track (subtitles, captions) of a video or audio.	src kind srclang label default
u	Text with an unarticulated annotation, underlined.
ul	An unordered list.
var	A variable in a mathematical expression or a program.
video	An embedded video player.	src controls autoplay loop muted playsinline poster preload width height crossorigin
wbr	A line-break opportunity.
`

// svgElementData is the SVG most pages draw with, in the same form.
const svgElementData = `
svg	An SVG drawing; the root of SVG content.	viewBox xmlns width height fill stroke preserveAspectRatio
circle	A circle, from its center and radius.	cx cy r fill stroke stroke-width
clipPath	A clipping path, used by clip-path.	clipPathUnits
defs	Graphical objects for later reference.
ellipse	An ellipse, from its center and radii.	cx cy rx ry fill stroke stroke-width
foreignObject	Content from another namespace (HTML) in the drawing.	x y width height
g	A group of shapes.	fill stroke transform opacity
image	A raster or SVG image in the drawing.	href x y width height preserveAspectRatio
line	A straight line between two points.	x1 y1 x2 y2 stroke stroke-width stroke-linecap
linearGradient	A linear gradient, of stop elements.	x1 y1 x2 y2 gradientUnits gradientTransform
mask	A mask, used by the mask property.	x y width height maskUnits
path	A shape drawn by path commands.	d fill stroke stroke-width stroke-linecap stroke-linejoin fill-rule transform
pattern	A pattern tile for fills and strokes.	x y width height patternUnits viewBox
polygon	A closed shape of straight lines.	points fill stroke stroke-width fill-rule
polyline	An open shape of straight lines.	points fill stroke stroke-width stroke-linecap stroke-linejoin
radialGradient	A radial gradient, of stop elements.	cx cy r fx fy gradientUnits gradientTransform
rect	A rectangle, rounded with rx and ry.	x y width height rx ry fill stroke stroke-width
stop	A color stop of a gradient.	offset stop-color stop-opacity
symbol	A template object, drawn by use.	viewBox preserveAspectRatio
text	Text in the drawing.	x y dx dy text-anchor dominant-baseline fill font-size
tspan	A span of text inside text.	x y dx dy
use	A copy of another element of the drawing.	href x y width height
`

// htmlGlobalAttrs are the attributes every element takes.
var htmlGlobalAttrs = strings.Fields(`className id style title key hidden tabindex lang dir role draggable contenteditable
	spellcheck translate accesskey inert popover slot part enterkeyhint autocapitalize`)

// htmlAttrData is name, description; el.name for an element's own meaning.
const htmlAttrData = `
className	The element's CSS classes, space-separated (written as class).
id	A unique identifier of the element in the page.
style	Inline CSS declarations; also takes a vuka.Style.
title	Advisory text, shown as a tooltip.
key	Names the element among its siblings for the live runtime's morph and stateful components (vuka; written as data-vk-key under a live session).
hidden	The element isn't relevant now and isn't rendered.
tabindex	Whether, and in what order, the element takes keyboard focus.
lang	The language of the element's content (a BCP 47 tag, en, sw).
dir	The direction of the element's text.
role	The ARIA role: what the element is, for assistive technologies.
draggable	Whether the element can be dragged.
contenteditable	Whether the user can edit the element's content.
spellcheck	Whether the element is checked for spelling errors.
translate	Whether the element's content is translated when the page is.
accesskey	A keyboard shortcut that focuses or activates the element.
inert	The element and its content can't be focused or clicked.
popover	Makes the element a popover, shown by a popovertarget button.
slot	The slot of a shadow tree the element goes in.
part	Names the element as a part of its shadow tree, for ::part().
enterkeyhint	The label of the Enter key of a virtual keyboard.
autocapitalize	How text typed in is capitalized.
href	The URL the link points to.
target	Where to open the URL: this tab, a new one, a frame.
rel	How the linked resource relates to this page, space-separated.
download	Download the URL instead of navigating; the value names the file.
hreflang	The language of the linked resource.
type	The MIME type of the linked or embedded resource.
input.type	The kind of control: text, email, checkbox, date, file…
button.type	What the button does: submit its form (the default), reset it, or nothing.
ol.type	The numbering: 1, a, A, i or I.
script.type	The kind of script: a module, an import map, classic JavaScript.
referrerpolicy	How much referrer information is sent with the request.
ping	URLs notified when the link is followed.
alt	Text replacing the image when it can't be shown; what it conveys.
coords	The coordinates of the area's shape.
shape	The shape of the area.
src	The URL of the resource.
controls	Shows the browser's playback controls.
autoplay	Starts playing as soon as it can.
loop	Starts over when it reaches the end.
muted	Starts muted.
playsinline	Plays inline on mobile instead of full screen.
poster	An image shown until the video plays.
preload	How much of the media to load before it is played.
crossorigin	Whether the resource is fetched with CORS, and with credentials.
cite	A URL explaining the quotation or the change.
disabled	The control can't be used and isn't submitted.
name	The name the control's value is submitted under.
meta.name	The kind of metadata content holds: description, viewport, theme-color…
value	The control's value, submitted with its name.
form	The id of the form the control belongs to, when not inside it.
formaction	The URL the submit goes to, overriding the form's action.
formmethod	The HTTP method of the submit, overriding the form's method.
formenctype	The encoding of the submit, overriding the form's enctype.
formnovalidate	Submits without validating, overriding the form.
formtarget	Where to show the submit's response, overriding the form's target.
popovertarget	The id of the popover the button controls.
popovertargetaction	What the button does to its popover.
autofocus	Focuses the element when the page loads.
width	The width, in CSS pixels.
height	The height, in CSS pixels.
span	The number of columns the element spans.
datetime	The date and time of the change or the time element.
open	Shown open.
details.name	Groups details elements: opening one closes the others.
action	The URL the form submits to.
method	The HTTP method of the submit.
enctype	How the submitted data is encoded.
autocomplete	Whether the browser fills the value in, and with what.
novalidate	Submits without validating the controls.
accept-charset	The character encodings the server accepts.
srcdoc	The HTML of the embedded page, inline.
allow	The features the embedded page may use (a permissions policy).
allowfullscreen	The embedded page may go full screen.
loading	Whether to load the resource lazily, when near the viewport.
sandbox	Restrictions on the embedded page, with exceptions listed.
srcset	Image candidates for different pixel densities or widths.
sizes	The image's display size per media condition, for srcset.
decoding	Whether to decode the image synchronously.
fetchpriority	The priority of the fetch relative to the page's others.
usemap	The image map (#name) of the image.
ismap	The image is part of a server-side image map.
placeholder	A hint shown while the control is empty.
required	The control must have a value to submit.
readonly	The value can't be edited but is submitted.
checked	The checkbox or radio is checked.
min	The lowest value allowed.
max	The highest value allowed.
step	The granularity of the value.
minlength	The fewest characters allowed.
maxlength	The most characters allowed.
pattern	A regular expression the value must match.
size	The width of the control, in characters (rows, for select).
multiple	Allows more than one value.
accept	The file types a file input takes.
list	The id of a datalist of suggestions.
inputmode	The virtual keyboard to show.
capture	The camera or microphone to capture a file with.
dirname	Also submits the text's direction, under this name.
htmlFor	The id of the control the label or output is for (written as for).
media	The media query the resource applies to.
as	The kind of resource a preload fetches.
integrity	A hash the fetched resource must match (subresource integrity).
content	The value of the metadata named by name or http-equiv.
charset	The character encoding of the document (utf-8).
http-equiv	A pragma directive, like an HTTP header.
low	The upper bound of the low part of the range.
high	The lower bound of the high part of the range.
optimum	The optimal value of the range.
data	The URL of the resource.
reversed	Numbers the list in descending order.
start	The number of the first item.
label	A user-readable title.
selected	The option is selected.
async	Runs the script as soon as it's fetched, not in document order.
defer	Runs the script after the document is parsed.
nomodule	Runs the script only where modules aren't supported.
colspan	The number of columns the cell spans.
rowspan	The number of rows the cell spans.
headers	The ids of the header cells that apply to the cell.
scope	The cells the header cell is a header for.
abbr	A short description of the header cell.
rows	The number of visible text lines.
cols	The visible width, in average characters.
wrap	How the text wraps when submitted.
kind	How the text track is used.
srclang	The language of the text track.
default	The track to enable unless the user prefers another.
shadowrootmode	Makes the template a declarative shadow root.
viewBox	The user-space rectangle (min-x min-y width height) the drawing maps onto its viewport.
xmlns	The SVG namespace, for SVG served outside HTML.
fill	The paint inside the shape.
stroke	The paint of the shape's outline.
stroke-width	The width of the outline.
stroke-linecap	The shape of the ends of open lines.
stroke-linejoin	The shape of the corners of lines.
fill-rule	Which parts of a shape count as inside.
preserveAspectRatio	How the viewBox fits a viewport of another aspect ratio.
transform	Transformations applied to the element and its children.
opacity	The element's opacity, 0 to 1.
cx	The x coordinate of the center.
cy	The y coordinate of the center.
r	The radius.
rx	The horizontal radius.
ry	The vertical radius.
fx	The x coordinate of the focal point.
fy	The y coordinate of the focal point.
x	The x coordinate.
y	The y coordinate.
dx	A shift along the x axis.
dy	A shift along the y axis.
x1	The x coordinate of the start.
y1	The y coordinate of the start.
x2	The x coordinate of the end.
y2	The y coordinate of the end.
d	The path: commands (M, L, C, Z…) and their coordinates.
points	The points of the shape, x,y pairs.
offset	Where the stop is along the gradient, 0 to 1 or a percentage.
stop-color	The color of the gradient stop.
stop-opacity	The opacity of the gradient stop.
gradientUnits	The coordinate system of the gradient's attributes.
gradientTransform	Transformations applied to the gradient.
clipPathUnits	The coordinate system of the clip path's contents.
maskUnits	The coordinate system of the mask's x, y, width and height.
patternUnits	The coordinate system of the pattern's x, y, width and height.
text-anchor	How the text aligns to its x, y point.
dominant-baseline	The baseline the text aligns to.
font-size	The size of the text.
`

// htmlValueData is name, values; el.name for an element's own.
const htmlValueData = `
input.type	text password email number tel url search date time datetime-local month week color checkbox radio file range hidden submit reset button image
button.type	button submit reset
ol.type	1 a A i I
script.type	module importmap text/javascript
target	_self _blank _parent _top
rel	noopener noreferrer nofollow external stylesheet icon preload prefetch preconnect dns-prefetch modulepreload manifest canonical alternate author help license next prev search
method	get post dialog
formmethod	get post dialog
enctype	application/x-www-form-urlencoded multipart/form-data text/plain
formenctype	application/x-www-form-urlencoded multipart/form-data text/plain
autocomplete	on off name email username new-password current-password one-time-code tel url street-address postal-code country organization bday cc-number
loading	lazy eager
decoding	async sync auto
fetchpriority	high low auto
dir	ltr rtl auto
draggable	true false
spellcheck	true false
contenteditable	true false plaintext-only
translate	yes no
crossorigin	anonymous use-credentials
referrerpolicy	no-referrer no-referrer-when-downgrade origin origin-when-cross-origin same-origin strict-origin strict-origin-when-cross-origin unsafe-url
preload	none metadata auto
wrap	soft hard
scope	row col rowgroup colgroup
inputmode	none text decimal numeric tel search email url
enterkeyhint	enter done go next previous search send
autocapitalize	off none on sentences words characters
kind	subtitles captions descriptions chapters metadata
sandbox	allow-forms allow-modals allow-popups allow-same-origin allow-scripts allow-downloads allow-top-navigation
popover	auto manual
popovertargetaction	show hide toggle
shape	rect circle poly default
as	script style font image fetch document audio video worker
http-equiv	content-security-policy content-type default-style refresh x-ua-compatible
meta.name	description viewport theme-color color-scheme author keywords robots referrer generator application-name
role	button link navigation main banner contentinfo complementary region search form dialog alertdialog alert status log progressbar tooltip menu menubar menuitem tab tablist tabpanel list listitem grid row cell gridcell columnheader rowheader table heading img figure checkbox radio switch textbox combobox listbox option slider separator tree treeitem presentation none
shadowrootmode	open closed
stroke-linecap	butt round square
stroke-linejoin	miter round bevel
fill-rule	nonzero evenodd
xmlns	http://www.w3.org/2000/svg
text-anchor	start middle end
gradientUnits	userSpaceOnUse objectBoundingBox
aria-hidden	true false
aria-expanded	true false
aria-pressed	true false mixed
aria-checked	true false mixed
aria-selected	true false
aria-disabled	true false
aria-busy	true false
aria-modal	true false
aria-required	true false
aria-invalid	true false grammar spelling
aria-live	off polite assertive
aria-current	page step location date time true false
aria-haspopup	true false menu listbox tree grid dialog
aria-autocomplete	none inline list both
aria-orientation	horizontal vertical
aria-sort	ascending descending none other
`

// htmlAriaAttrs are the ARIA attributes offered on every element.
const htmlAriaData = `
aria-label	A label for the element, read by assistive technologies.
aria-labelledby	The ids of the elements that label this one.
aria-describedby	The ids of the elements that describe this one.
aria-hidden	Hides the element from assistive technologies.
aria-expanded	Whether the element it controls is expanded.
aria-controls	The ids of the elements this one controls.
aria-current	The current item of a set: the page, a step.
aria-live	How updates of the element are announced.
aria-pressed	The state of a toggle button.
aria-checked	The state of a checkbox, radio or switch.
aria-selected	Whether the element is selected.
aria-disabled	The element is perceivable but disabled.
aria-haspopup	The kind of popup the element opens.
aria-busy	The element is being updated.
aria-modal	The dialog is modal.
aria-required	A value is required.
aria-invalid	The value is invalid.
aria-autocomplete	The kind of suggestions the input makes.
aria-orientation	The orientation of the element.
aria-sort	How the column is sorted.
aria-valuenow	The current value of a range widget.
aria-valuemin	The lowest value of a range widget.
aria-valuemax	The highest value of a range widget.
aria-valuetext	A readable form of aria-valuenow.
aria-owns	The ids of elements this one owns outside its subtree.
aria-roledescription	A readable description of the role.
`

// htmlEventData is the event handlers offered on an element, onXxx, and what
// fires them.
const htmlEventData = `
onClick	A click (or tap, or Enter on a button) on the element.
onDblClick	A double click.
onInput	Each change of an input's value, as it is typed.
onChange	A committed change of a control's value.
onSubmit	A form's submit.
onReset	A form's reset.
onKeyDown	A key pressed; the handler gets the key.
onKeyUp	A key released.
onFocus	The element took focus.
onBlur	The element lost focus.
onFocusIn	The element or one inside it took focus.
onFocusOut	The element or one inside it lost focus.
onMouseDown	A mouse button pressed on the element.
onMouseUp	A mouse button released on the element.
onMouseEnter	The pointer entered the element.
onMouseLeave	The pointer left the element.
onMouseOver	The pointer moved onto the element or a child.
onMouseOut	The pointer moved off the element or a child.
onMouseMove	The pointer moved over the element.
onPointerDown	A pointer (mouse, pen, touch) went down on the element.
onPointerUp	A pointer was released on the element.
onContextMenu	The context menu was asked for.
onWheel	A wheel turned over the element.
onScroll	The element scrolled.
onDragStart	A drag started on the element.
onDragOver	Something is dragged over the element.
onDrop	Something was dropped on the element.
onTouchStart	A touch started on the element.
onTouchEnd	A touch ended on the element.
onLoad	The resource finished loading.
onError	The resource failed to load.
onToggle	A details or popover opened or closed.
onInvalid	A control failed validation.
onCopy	The user copied.
onCut	The user cut.
onPaste	The user pasted.
`

// htmlBooleans are the attributes written bare: present is true.
var htmlBooleans = setOf(`disabled checked selected readonly required multiple autofocus autoplay controls loop muted
	playsinline hidden open novalidate formnovalidate async defer nomodule reversed ismap default allowfullscreen inert`)

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
	case attr == "key":
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
