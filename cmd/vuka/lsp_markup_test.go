package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// benchSource is the page every editor feature is checked against: typing it
// must feel like TSX.
const benchHead = "package main\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/vuka-lang/ui\"\n\t\"lsptest/data\"\n)\n\n"
const benchBody = `func PetTable(pets []data.Pet) ui.Node {
	return <>
		<h1>Pets ({len(pets)})</h1>
		<table>
			<tr><th>Name</th><th>Kind</th><th>Age</th></tr>
			{for _, p := range pets {
				<tr>
					<td>
						<a href={fmt.Sprintf("/pets/%d", p.ID)}>{p.Name}</a>
					</td>
					<td>{p.Kind}</td>
					<td>{p.Age}</td>
				</tr>
			}}
		</table>
	</>
}
`
const benchTail = "\nfunc main() {}\n"
const benchSource = benchHead + benchBody + benchTail
const benchData = "package data\n\ntype Pet struct {\n\tID   int\n\tName string\n\tKind string\n\tAge  int\n}\n"

// nth is the offset of the nth (1-based) needle in src, plus delta.
func nth(t *testing.T, src, needle string, n, delta int) int {
	t.Helper()
	off := -1
	for i := 0; i < n; i++ {
		j := strings.Index(src[off+1:], needle)
		if j < 0 {
			t.Fatalf("no %d× %q", n, needle)
		}
		off += 1 + j
	}
	return off + delta
}

func lineOf(src string, off int) uint32 { return positionOf([]byte(src), off).Line }

func TestLinkedEditing(t *testing.T) {
	src := []byte(benchSource)
	for _, tc := range []struct {
		needle        string
		n, delta      int
		open, close   string
		openN, closeN int
	}{
		{"<td>", 1, 2, "<td>", "</td>", 1, 1},
		{"</td>", 1, 3, "<td>", "</td>", 1, 1},
		{"<td>", 2, 1, "<td>", "</td>", 2, 2},
		{"<th>", 3, 2, "<th>", "</th>", 3, 3},
		{"<tr>", 2, 3, "<tr>", "</tr>", 2, 2},
		{"<table>", 1, 6, "<table>", "</table>", 1, 1},
		{"<a href", 1, 1, "<a href", "</a>", 1, 1},
	} {
		got, _ := json.Marshal(linkedEditing(src, nth(t, benchSource, tc.needle, tc.n, tc.delta)))
		name := strings.Fields(strings.Trim(tc.open, "<>"))[0]
		o := nth(t, benchSource, tc.open, tc.openN, 1)
		c := nth(t, benchSource, tc.close, tc.closeN, 2)
		want, _ := json.Marshal([]lspRange{{positionOf(src, o), positionOf(src, o+len(name))}, {positionOf(src, c), positionOf(src, c+len(name))}})
		if !strings.Contains(string(got), string(want)) {
			t.Errorf("%s #%d: %s, want ranges %s", tc.needle, tc.n, got, want)
		}
	}
	// The fragment: empty names, typing one into <> names </> too.
	got, _ := json.Marshal(linkedEditing(src, nth(t, benchSource, "<>", 1, 1)))
	o, c := nth(t, benchSource, "<>", 1, 1), nth(t, benchSource, "</>", 1, 2)
	want, _ := json.Marshal([]lspRange{{positionOf(src, o), positionOf(src, o)}, {positionOf(src, c), positionOf(src, c)}})
	if !strings.Contains(string(got), string(want)) {
		t.Errorf("fragment: %s, want %s", got, want)
	}
	if v := linkedEditing(src, nth(t, benchSource, "pets {", 1, 1)); v != nil {
		t.Errorf("off a tag: %v", v)
	}
	// Mid-edit: a tag still being typed elsewhere doesn't unpair the rest.
	mid := strings.Replace(benchSource, "<td>{p.Kind}</td>", "<td>{p.Kind}</td>\n\t\t\t\t\t<sp", 1)
	if v := linkedEditing([]byte(mid), nth(t, mid, "</tr>", 2, 3)); v == nil {
		t.Error("no linked ranges mid-edit")
	}
	// Components and generic components pair too.
	comp := "func F() ui.Node {\n\treturn <List[User] items={x}>\n\t\t<Card>a</Card>\n\t</List>\n}\n"
	if v, _ := json.Marshal(linkedEditing([]byte(comp), nth(t, comp, "</List>", 1, 3))); !strings.Contains(string(v), `"character":7`) {
		t.Errorf("generic component: %s", v)
	}
}

func TestMarkupFolds(t *testing.T) {
	got := markupFolds([]byte(benchSource))
	src := benchSource
	want := map[uint32]uint32{
		lineOf(src, nth(t, src, "<>", 1, 0)):      lineOf(src, nth(t, src, "</>", 1, 0)) - 1,
		lineOf(src, nth(t, src, "<table>", 1, 0)): lineOf(src, nth(t, src, "</table>", 1, 0)) - 1,
		lineOf(src, nth(t, src, "{for", 1, 0)):    lineOf(src, nth(t, src, "}}", 1, 0)) - 1,
		lineOf(src, nth(t, src, "<tr>", 2, 0)):    lineOf(src, nth(t, src, "</tr>", 2, 0)) - 1,
		lineOf(src, nth(t, src, "<td>", 1, 0)):    lineOf(src, nth(t, src, "</td>", 1, 0)) - 1,
	}
	if len(got) != len(want) {
		t.Errorf("folds %v, want %v", got, want)
	}
	for l, e := range want {
		if got[l] != e {
			t.Errorf("fold at line %d ends at %d, want %d (%v)", l, got[l], e, got)
		}
	}
}

func TestUnclosedAt(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"\treturn <>\n\t\t<p>a</p>\n\t</", ""},
		{"\treturn <div><p>hi</", "p"},
		{"\treturn <div><p>hi</p></", "div"},
		{"\treturn <table>{for _, p := range ps {\n\t<tr><td>{p.N}</", "td"},
		{"\treturn <table>{for _, p := range ps {\n\t<tr><td>{p.N}</td></", "tr"},
		{"\treturn <ul>{if ok { <li>x</li> }}</", "ul"},
		{"\ts := \"<b>\"\n\treturn <i></", "i"},
	} {
		src := []byte(tc.src)
		e, ok := scanMarkup(src).unclosedAt(len(src) - 2)
		if !ok || e.open.name != tc.want {
			t.Errorf("%q: %q %v, want %q", tc.src, e.open.name, ok, tc.want)
		}
	}
}

func TestBlockSpot(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{"\treturn <ul>\n\t\t{fo}\n\t</ul>", true},
		{"\treturn <ul>\n\t\t{}\n\t</ul>", true},
		{"\treturn <ul>\n\t\t{ma\n\t</ul>", true},
		{"\treturn <ul>\n\t\t{len}\n\t</ul>", false},
		{"\treturn <ul className={fo}>\n\t</ul>", false},
		{"\tif ok {fo}", false},
	} {
		off := strings.Index(tc.src, "{") + 1
		for off < len(tc.src) && isIdentByte(tc.src[off]) {
			off++
		}
		if _, ok := blockSpot([]byte(tc.src), off); ok != tc.want {
			t.Errorf("%q: %v", tc.src, ok)
		}
	}
}

func TestCompletionSpotValue(t *testing.T) {
	for _, tc := range []struct{ src, kind, attr string }{
		{`	return <input type="te" />`, "value", "type"},
		{`	return <input type="`, "value", "type"},
		{`	return <a target="_b`, "value", "target"},
		{`	return <a href={x} rel="noopener n">`, "value", "rel"},
		{`	return <Card variant="pr" />`, "", ""},
		{`	return <a href={"x`, "", ""},
	} {
		off := strings.LastIndexByte(tc.src, '"') + 1
		if i := strings.Index(tc.src, `" />`); i > 0 {
			off = i
		}
		if i := strings.Index(tc.src, `">`); i > 0 {
			off = i
		}
		spot, ok := completionSpot([]byte(tc.src), off)
		if tc.kind == "" && ok && spot.kind == "value" || tc.kind != "" && (spot.kind != tc.kind || spot.attr != tc.attr) {
			t.Errorf("%q: %+v %v", tc.src, spot, ok)
		}
	}
}

func TestHTMLData(t *testing.T) {
	for _, el := range []string{"a", "table", "td", "input", "dialog", "template", "svg", "path", "h1", "search", "picture"} {
		if htmlElements[el] == nil {
			t.Errorf("no <%s>", el)
		}
	}
	if len(htmlElemNames) < 130 {
		t.Errorf("only %d elements", len(htmlElemNames))
	}
	// Every attribute offered has a description, and every enumerated one values.
	for _, name := range htmlElemNames {
		own, global := elementAttrs(name)
		for _, a := range append(append([]string{}, own...), global...) {
			if attrDoc(name, a) == "" {
				t.Errorf("<%s %s>: no description", name, a)
			}
		}
	}
	for _, tc := range []struct{ tag, attr, want string }{
		{"input", "type", "checkbox"}, {"button", "type", "submit"}, {"a", "target", "_blank"}, {"a", "rel", "noopener"},
		{"form", "method", "post"}, {"th", "scope", "col"}, {"div", "aria-live", "polite"}, {"img", "loading", "lazy"},
	} {
		if !strings.Contains(strings.Join(attrValues(tc.tag, tc.attr), " "), tc.want) {
			t.Errorf("%s %s: %v", tc.tag, tc.attr, attrValues(tc.tag, tc.attr))
		}
	}
	for _, tc := range []struct{ tag, attr, want string }{
		{"a", "href", "Element/a#href"}, {"td", "colspan", "Element/td#colspan"}, {"label", "htmlFor", "Element/label#for"},
		{"div", "className", "Global_attributes/class"}, {"div", "onClick", "Element/click_event"},
		{"div", "data-user-id", "dataset.userId"}, {"div", "aria-label", "ARIA/Attributes/aria-label"},
		{"path", "d", "SVG/Attribute/d"},
	} {
		if d := attrDoc(tc.tag, tc.attr); !strings.Contains(d, tc.want) {
			t.Errorf("%s %s: %q", tc.tag, tc.attr, d)
		}
	}
	if d := elementDoc(htmlElements["h2"]); !strings.Contains(d, "Heading_Elements") {
		t.Errorf("h2: %s", d)
	}
}
