package transpile

import (
	"go/types"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Modifiers on an on… attribute or bind:value are steps of the browser's
// chain before the handler, in a fixed order — the key filter, prevent,
// stop, once, then the pace:
//
//	onKeydown.ctrl.enter.prevent={Send()}  →  ui.JS.Key("ctrl+Enter").Prevent().Push(p.Send)
//	onInput.debounce.300ms={Search(this.value)}  →  ui.JS.Debounce(300ms).Push(p.Search, ui.This.Value())
//	onClick.stop={open = !open}            →  ui.JS.Stop().Then(uigen.Toggle(ui.JS, open))
//	bind:value.throttle.1s={Q}             →  ui.BindValue(p, &p.Q, ui.JS.Throttle(1s))
//
// A handler that is a func (a method value, a closure) is pushed; browser
// commands follow the steps.

// eventMods are an attribute's modifiers.
type eventMods struct {
	keys                []string // KeyboardEvent.key names, a modifier key's first: ctrl+Enter
	prevent, stop, once bool
	pace                string // "debounce" or "throttle"
	dur                 time.Duration
}

// modKeys are the key filters, by the names they are written with.
var modKeys = map[string]string{
	"enter": "Enter", "esc": "Escape", "tab": "Tab", "space": " ",
	"up": "ArrowUp", "down": "ArrowDown", "left": "ArrowLeft", "right": "ArrowRight",
}

// modHeld are the modifier keys a key filter may name before its key.
var modHeld = map[string]bool{"ctrl": true, "shift": true, "alt": true, "meta": true}

var modNames = []string{"debounce", "throttle", "prevent", "stop", "once", "enter", "esc", "tab", "space", "up", "down", "left", "right", "ctrl", "shift", "alt", "meta"}

// defaultPace is .debounce's and .throttle's wait without a duration.
const defaultPace = 300 * time.Millisecond

// eventMods reads the modifiers after an attribute's name, base its
// name before them: onKeydown or bind:value. A modifier the attribute
// can't take is E0173.
func (p *jsxParser) eventMods(a *jsxAttr, base, text string) *eventMods {
	fail := func(format string, args ...any) { p.failCode("E0173", a.off, format, args...) }
	m := &eventMods{}
	bind := base == "bind:value"
	var held []string
	toks := strings.Split(text, ".")
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if bind && t != "debounce" && t != "throttle" && !startsDigit(t) {
			if s := closestName(t, []string{"debounce", "throttle"}); s != "" && s != t {
				fail("bind:value.%s isn't a modifier; did you mean .%s?", t, s)
			}
			fail("bind:value.%s: bind:value takes .debounce and .throttle, which time when the value is sent", t)
		}
		switch {
		case t == "":
			fail("%s has an empty modifier: %s.prevent", base, base)
		case t == "debounce" || t == "throttle":
			if m.pace != "" {
				fail("%s is paced twice, .%s and .%s: keep one", base, m.pace, t)
			}
			m.pace, m.dur = t, defaultPace
			if i+1 < len(toks) && (startsDigit(toks[i+1]) || !slices.Contains(modNames, toks[i+1])) {
				i++
				d, err := time.ParseDuration(toks[i])
				if err != nil || d <= 0 {
					fail(".%s.%s isn't a duration: write it as Go does, .%s.300ms or .%s.1s", t, toks[i], t, t)
				}
				m.dur = d
			}
		case startsDigit(t):
			fail(".%s is a duration, which follows .debounce or .throttle: %s.debounce.%s", t, base, t)
		case t == "prevent":
			m.prevent = true
		case t == "stop":
			m.stop = true
		case t == "once":
			m.once = true
		case modHeld[t]:
			held = append(held, t)
		case modKeys[t] != "":
			if !isKeyEvent(base) {
				fail("%s.%s filters key presses: put it on a key event, onKeydown.%s", base, t, t)
			}
			key := modKeys[t]
			if len(held) > 0 {
				key = strings.Join(held, "+") + "+" + key
				held = nil
			}
			m.keys = append(m.keys, key)
		default:
			if s := closestName(t, modNames); s != "" {
				fail(".%s isn't a modifier of %s; did you mean .%s?", t, base, s)
			}
			fail(".%s isn't a modifier of %s: .debounce, .throttle, .prevent, .stop, .once and the keys (.enter, .esc, .ctrl.enter, …) are", t, base)
		}
	}
	if len(held) > 0 {
		fail(".%s is held with a key: name the key after it, onKeydown.%s.enter", held[0], strings.Join(held, "."))
	}
	return m
}

func startsDigit(s string) bool { return s != "" && '0' <= s[0] && s[0] <= '9' }

// isKeyEvent reports whether an event attribute names a key event.
func isKeyEvent(name string) bool {
	switch strings.ToLower(name) {
	case "onkeydown", "onkeyup", "onkeypress":
		return true
	}
	return false
}

// steps are the modifiers as a chain's steps after q's JS.
func (m *eventMods) steps(q string) string {
	var b strings.Builder
	b.WriteString(q + "JS")
	if len(m.keys) > 0 {
		quoted := make([]string, len(m.keys))
		for i, k := range m.keys {
			quoted[i] = strconv.Quote(k)
		}
		b.WriteString(".Key(" + strings.Join(quoted, ", ") + ")")
	}
	if m.prevent {
		b.WriteString(".Prevent()")
	}
	if m.stop {
		b.WriteString(".Stop()")
	}
	if m.once {
		b.WriteString(".Once()")
	}
	if m.pace != "" {
		b.WriteString(".")
		b.WriteString(strings.ToUpper(m.pace[:1]) + m.pace[1:])
		b.WriteString("(" + strconv.FormatInt(int64(m.dur), 10) + ")")
	}
	return b.String()
}

// modded writes an event attribute with modifiers: the steps, then the
// handler — browser commands joined on with Then, a func pushed.
func (w *jsxWriter) modded(a *jsxAttr, value func()) {
	w.gen(a.emods.steps(w.q))
	if w.isChainValue(a) {
		w.gen(".Then(")
	} else {
		w.gen(".Push(")
	}
	value()
	w.gen(")")
}

// isChainValue reports whether an event's value is browser commands rather
// than a handler: a lowered push or write, or a value of a type with the
// target's chain methods.
func (w *jsxWriter) isChainValue(a *jsxAttr) bool {
	if a.push != nil || a.write != nil {
		return true
	}
	e := w.f.eng
	if e.info == nil || a.stmts {
		return false
	}
	x := e.attrExpr(w.f, a)
	if x == nil {
		return false
	}
	t := e.info.TypeOf(x)
	if t == nil {
		return false
	}
	if isTargetNamed(w.f, t, "Commands") || isTargetNamed(w.f, t, "CommandsOn") {
		return true
	}
	chain, _ := e.targetObj(w.f.target, "JSChain").(*types.TypeName)
	if chain == nil {
		return false
	}
	it, ok := chain.Type().Underlying().(*types.Interface)
	return ok && types.Implements(t, it)
}
