package format

import "testing"

func TestTemplate(t *testing.T) {
	src := "<p class={ cls }>{a+b} {{x} {/* note */}</p>   \n\n\n<script lang=\"ts\">\nconst x = {a:1}\n\n\n</script>\n// Row is a row.\nfunc row(o   Order)\n<li>{ o.Ref }</li>\n\n\n"
	want := "<p class={cls}>{a + b} {{x} {/* note */}</p>\n\n<script lang=\"ts\">\nconst x = {a:1}\n\n\n</script>\n\n// Row is a row.\nfunc row(o Order)\n<li>{o.Ref}</li>\n"
	got, err := Template([]byte(src))
	if err != nil || string(got) != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if again, _ := Template(got); string(again) != want {
		t.Fatalf("not idempotent:\n%q", again)
	}
}

func TestTemplateLongHeader(t *testing.T) {
	src := "func App(me *auth.Identity, path string, title string, description string, actions ui.Node, children ui.Node)\n<div>{title}</div>\n"
	if got, err := Template([]byte(src)); err != nil || string(got) != src {
		t.Fatalf("got\n%q", got)
	}
}
