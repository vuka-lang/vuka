package transpile

import "testing"

func TestTemplateReadsContext(t *testing.T) {
	for _, tc := range []struct {
		name, body, header string
		want               bool
	}{
		{"hole", `<b>{ctx.Value(key{})}</b>`, "", true},
		{"statement", "me := auth.Current(ctx)\n<p>{me}</p>", "", true},
		{"condition", "if auth.Can(ctx, \"view\") {\n<p>yes</p>\n}", "", true},
		{"literal", `<p>{"ctx"}</p>`, "", false},
		{"text", `<p>ctx</p>`, "", false},
		{"selector", `<p>{page.ctx}</p>`, "", false},
		{"parameter", `<p>{ctx.Value(key{})}</p>`, "func Card(ctx context.Context)", false},
		{"aliased parameter", `<p>{ctx.Value(key{})}</p>`, "func Card(ctx c.Context)", false},
		{"receiver", `<p>{ctx.Title}</p>`, "func (ctx Card) View()", false},
		{"local", "ctx := \"local\"\n<p>{ctx}</p>", "", false},
		{"variable", "var ctx string\n<p>{ctx}</p>", "", false},
		// A block body's apostrophe (in plain text) must not swallow the
		// holes after it: the ctx read past it is still seen.
		{"apostrophe before ctx", "if ok {\n<p>Single Window's name</p>\n}\n<a href={u.URL(ctx)}>go</a>", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := templateReadsContext([]byte(tc.body), tc.header, nil); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
