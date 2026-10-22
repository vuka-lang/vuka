package transpile

import (
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/tsprops"
)

// An island page: a .vuka.html file whose markup is one tag, an island
// component the header imports, is the page that component backs:
//
//	package pages
//
//	import Applicants "../web/components/Applicants.vue"
//
//	type Page struct {
//		ui.Live
//		Q    string `json:"q" bind:"q"`
//		Rows []Applicant `json:"rows"`
//	}
//	<Applicants q={Q} rows={Rows} client:load />
//
// The struct is the island's props, as an @ui.Island struct's are: its
// exported fields the component reads, its exported methods what it calls.
// Embedding ui.Live makes it a live island page (its hooks load what the
// component shows); without, a stateless one, built per request.
// It lowers as @ui.Island(path) on the struct, the compiler writing its
// Render (ui.IslandPage), and the import declares nothing.

// islandPage is an island page's component.
type islandPage struct {
	path string
	imp  TemplateImport
}

var (
	islandTag  = regexp.MustCompile(`^<([A-Z]\w*)((?:\s+[^<>]*?)?)\s*/>$`)
	islandAttr = regexp.MustCompile(`^(?:client:\w+(?:=\{[^}]*\}|="[^"]*")?|([a-zA-Z]\w*)=\{\s*(\w+)\s*\})$`)
)

// islandOnly reads the markup lines[from:to] as an island page's: nil when
// it isn't one tag naming an imported component, an error when the tag or
// the struct breaks an island page's rules.
func islandOnly(shape *Shape, head TemplateHead, lines []string, from, to int) (*islandPage, *Error) {
	if shape == nil || from < 0 {
		return nil, nil
	}
	var b strings.Builder
	for _, l := range lines[from:to] {
		b.WriteString(strings.TrimSpace(l) + " ")
	}
	m := islandTag.FindStringSubmatch(strings.TrimSpace(b.String()))
	if m == nil {
		return nil, nil
	}
	var page *islandPage
	for _, imp := range head.Imports {
		if imp.Name == m[1] && tsprops.IsComponent(imp.Path) {
			page = &islandPage{path: imp.Path, imp: imp}
		}
	}
	if page == nil {
		return nil, nil
	}
	at := token.Position{Line: from + 1, Column: 1}
	if shape.Live && !shape.EmbedsLive {
		return nil, &Error{Code: "E0154", Pos: at,
			Msg:  shape.Type + "'s markup is the island " + m[1] + ", and " + shape.Type + " is live (" + shape.LiveWhy + ") without embedding ui.Live: an island page is live when its struct embeds ui.Live",
			Help: "embed ui.Live in " + shape.Type + " for a live island page, or drop its hooks for a stateless one"}
	}
	props := map[string]bool{}
	for _, f := range shape.Fields {
		if f.Role == Prop {
			props[f.Name] = true
		}
	}
	for _, a := range strings.Fields(m[2]) {
		am := islandAttr.FindStringSubmatch(a)
		if am == nil || am[1] != "" && (!props[am[2]] || !strings.EqualFold(am[1], am[2])) {
			return nil, &Error{Code: "E0154", Pos: at,
				Msg:  "the island page's tag passes " + a + ": its props are " + shape.Type + "'s exported fields, which the tag names",
				Help: "write each prop as its field, rows={Rows}, or leave it out: the component gets every exported field"}
		}
	}
	return page, nil
}

// islandDecorator is the attribute an island page's struct gets.
func (p *islandPage) decorator(ui string) string {
	return "@" + ui + "Island(" + strconv.Quote(p.path) + ") "
}
