package transpile

import "testing"

// TestDeclaresName: the markup's method takes a receiver of its own only
// when a let or a loop declaring the receiver's name holds a bare name
// the compiler would qualify with it.
func TestDeclaresName(t *testing.T) {
	for _, c := range []struct {
		markup, name string
		want         bool
	}{
		{"<ul>\n  for _, o := range recent {\n    <li key={o.ID}>{o.Number}</li>\n  }\n</ul>\n<p>{Title}</p>", "o", false},
		{"<ul>\n  for _, o := range recent {\n    <li>{o.Number} {Title}</li>\n  }\n</ul>", "o", true},
		{"<ul>\n  for _, o := range recent {\n    if Show {\n      <li>{o.Number}</li>\n    } else {\n      <li>none</li>\n    }\n  }\n</ul>", "o", true},
		{"<ul>\n  for i, o := range recent {\n    if i > 0 {\n      <li>{o.Number}</li>\n    }\n  }\n</ul>", "o", false},
		{"<Sheet for={For} let={t}><p>{t.Name}</p></Sheet>", "t", false},
		{"<Sheet for={For} let={t}><form for={Form}></form></Sheet>", "t", true},
		{"<Sheet for={For} let={t}><p>{t.Name}</p></Sheet><p>{Form.Note}</p>", "t", false},
		{"<List[Row] items={Rows} let={x, t}><b onClick={Save(t.ID)}>{x}</b></List[Row]>", "t", true},
		{"<Sheet let={u}>{Form}</Sheet>", "t", false},
	} {
		if got := declaresName(c.markup, c.name); got != c.want {
			t.Errorf("%q: %v, want %v", c.markup, got, c.want)
		}
	}
}
