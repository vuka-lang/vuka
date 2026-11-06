package transpile_test

import (
	"strings"
	"testing"
)

const modsPage = `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Notes struct {
	ui.Live
	q string
}

func (p *Notes) Search(q string) { p.q = q }
func (p *Notes) Add(s string)    {}
func (p *Notes) Save() error     { return nil }
func (p *Notes) Track(s string)  {}
func (p *Notes) Start()          {}
</script>

<main>
  <input onInput.debounce={Search(this.value)} />
  <input onInput.throttle.1s={Track(this.value)} />
  <form onSubmit.prevent={Save()}></form>
  <div onClick.stop={Start}></div>
  <input onKeydown.enter={Add(this.value)} />
  <textarea onKeydown.ctrl.enter.esc.prevent={Add(this.value)}></textarea>
  <button onClick.once={ui.JS.Show("#m")}>start</button>
</main>
`

// TestEventModifiers: an event's modifiers are the chain's steps before its
// handler, in their order: key filter, prevent, stop, once, pace (vuka#354).
func TestEventModifiers(t *testing.T) {
	res, err := compileShapes(map[string]string{"notes.vuka.html": modsPage})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Files[0].Src)
	for _, want := range []string{
		`ui.JS.Debounce(300000000).Then(ui.JS.Push(p.Search, ui.This.Value()))`,
		`ui.JS.Throttle(1000000000).Then(ui.JS.Push(p.Track, ui.This.Value()))`,
		`ui.JS.Prevent().Then(ui.JS.Push(p.Save))`,
		`ui.JS.Stop().Push(p.Start)`,
		`ui.JS.Key("Enter").Then(ui.JS.Push(p.Add, ui.This.Value()))`,
		`ui.JS.Key("ctrl+Enter", "Escape").Prevent().Then(ui.JS.Push(p.Add, ui.This.Value()))`,
		`ui.JS.Once().Then(ui.JS.Show("#m"))`,
		`onInput={`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %s in\n%s", want, src)
		}
	}
}

// TestEventModifierErrors: an unknown modifier, a misspelt one, a bad
// duration, a key filter on another event, a modifier key without its key
// and a modifier bind:value doesn't take are E0173.
func TestEventModifierErrors(t *testing.T) {
	for _, c := range []struct{ old, new, want string }{
		{"onInput.debounce=", "onInput.debounse=", ".debounse isn't a modifier of onInput; did you mean .debounce?"},
		{"onInput.debounce=", "onInput.zzzzz=", ".zzzzz isn't a modifier of onInput: .debounce, .throttle"},
		{"onInput.throttle.1s=", "onInput.throttle.1x=", ".throttle.1x isn't a duration: write it as Go does, .throttle.300ms or .throttle.1s"},
		{"onInput.throttle.1s=", "onInput.300ms=", ".300ms is a duration, which follows .debounce or .throttle"},
		{"onClick.stop=", "onClick.enter=", "onClick.enter filters key presses: put it on a key event, onKeydown.enter"},
		{"onKeydown.enter=", "onKeydown.ctrl=", ".ctrl is held with a key: name the key after it, onKeydown.ctrl.enter"},
		{"onInput.debounce=", "onInput.debounce.throttle=", "onInput is paced twice, .debounce and .throttle: keep one"},
	} {
		t.Run(c.new, func(t *testing.T) {
			src := strings.Replace(modsPage, c.old, c.new, 1)
			_, err := compileShapes(map[string]string{"notes.vuka.html": src})
			if err == nil || !strings.Contains(codes(err), "E0173") || !strings.Contains(codes(err), c.want) {
				t.Errorf("no E0173 %q in %v", c.want, err)
			}
		})
	}
}

const bindPage = `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Search struct {
	ui.Live
	Q string @ui.Bind
}
</script>

<input bind:value.throttle.1s={Q} />
`

// TestBindModifiers: a pace on bind:value times the sets; nothing else
// is one of its modifiers.
func TestBindModifiers(t *testing.T) {
	res, err := compileShapes(map[string]string{"search.vuka.html": bindPage})
	if err != nil {
		t.Fatal(err)
	}
	if src := string(res.Files[0].Src); !strings.Contains(src, `ui.BindValue(s, &s.Q, ui.JS.Throttle(1000000000))`) {
		t.Errorf("no paced BindValue in\n%s", src)
	}
	for bad, want := range map[string]string{
		"bind:value.prevent={Q}":    "bind:value.prevent: bind:value takes .debounce and .throttle, which time when the value is sent",
		"bind:value.debunce={Q}":    "bind:value.debunce isn't a modifier; did you mean .debounce?",
		"bind:value.debounce.x={Q}": ".debounce.x isn't a duration",
	} {
		src := strings.Replace(bindPage, "bind:value.throttle.1s={Q}", bad, 1)
		_, err := compileShapes(map[string]string{"search.vuka.html": src})
		if err == nil || !strings.Contains(codes(err), "E0173") || !strings.Contains(codes(err), want) {
			t.Errorf("%s: no E0173 %q in %v", bad, want, err)
		}
	}
}

// TestComponentModifiers: modifiers on an event a component passes on to
// its element; a func prop takes none.
func TestComponentModifiers(t *testing.T) {
	page := `<script lang="go">
package main

import ui "vukatest.local/opttarget"

type Index struct {
	ui.Live
	status string
}

func (p *Index) Choose(s string) { p.status = s }
</script>

<Combobox name="status" onChange.debounce.1s={Choose(this.value)} />
`
	res, err := compileShapes(map[string]string{"index.vuka.html": page, "combobox.vuka.html": eventCombobox})
	if err != nil {
		t.Fatal(err)
	}
	var src string
	for _, o := range res.Files {
		src += string(o.Src)
	}
	if want := `Value: ui.JS.Debounce(1000000000).Then(ui.JS.Push(p.Choose, ui.This.Value()))`; !strings.Contains(src, want) {
		t.Errorf("no %s in\n%s", want, src)
	}
	picker := `<script lang="go">
package main

type Picker struct{ OnPick func(string) }
</script>

<span></span>
`
	page = strings.Replace(page, `<Combobox name="status" onChange.debounce.1s={Choose(this.value)} />`, `<Picker onPick.once={Choose} />`, 1)
	_, err = compileShapes(map[string]string{"index.vuka.html": page, "picker.vuka.html": picker})
	if want := "<Picker>'s onPick takes no modifiers"; err == nil || !strings.Contains(codes(err), "E0173 ") || !strings.Contains(codes(err), want) {
		t.Errorf("no %q in %v", want, err)
	}
}
