package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"

	"github.com/evanw/esbuild/pkg/api"
)

// A component's scripts (web.Script, web.JS) are TypeScript or JavaScript,
// bundled by esbuild into one ES module whose default export is the list of
// their default exports, in order: each a hook object, { mounted(el, ctx),
// updated(el, ctx), destroyed(el, ctx), …methods }. Relative imports are
// bundled, and packages from node_modules; vuka:component is componentModule.

// componentModule is vuka:component, what a template's script imports:
// component(fn) makes the loader's hooks of a function run once per
// instance of a component or a page — fn(el, props, ctx), its props the
// data-vk-p its root carries (ui.Props), update(next) when a patch changes
// them — and destroy() when it leaves; page(fn) is the same as
// fn(el, ctx.live, ctx).
// ctx.live and live call the instance's exported Go methods: live.reorder(ids)
// is Reorder; live.q is the field Q's handle, get() and set(v) (a bind:
// field); ctx.state(name) a ui.State's, from the root's data-vk-st
// (ui.States); navigate and patch move the page.
const componentModule = `
const attr = (el, a) => { try { return JSON.parse(el.getAttribute(a) || "{}"); } catch { return {}; } };
const read = (el) => attr(el, "data-vk-p");
const go = (href, patch, opts) => {
  if (window.vuka && window.vuka.navigate) return window.vuka.navigate(href, { ...(opts || {}), patch });
  const u = new URL(String(href), location.href);
  if (u.protocol !== "http:" && u.protocol !== "https:") return Promise.reject(new Error("vuka: refused to load a URL that isn't http or https"));
  opts && opts.replace ? location.replace(u.href) : location.assign(u.href);
  return new Promise(() => {});
};
const nav = { navigate: (href, opts) => go(href, false, opts), patch: (href, opts) => go(href, true, opts) };
const states = () => window.vuka && window.vuka.states;
const state = (el) => (name) => {
  const r = attr(el, "data-vk-st")[name];
  if (!r) throw new Error("vuka: no state " + name);
  const x = r.x !== undefined ? r.x : r;
  return {
    get: () => states() ? states().read(x) : r.i,
    set: (v) => { if (r.x === undefined && states()) states().write(r, v); },
    // A script can mount before the live runtime keeps the states: its
    // watch starts once it does.
    watch: (fn) => {
      let off = null, done = false;
      const arm = () => { if (done) return; if (states()) off = states().watch(x, fn, el); else setTimeout(arm, 20); };
      arm();
      return () => { done = true; if (off) off(); };
    },
  };
};
const key = (p, k) => k in p ? k : k[0].toUpperCase() + k.slice(1) in p ? k[0].toUpperCase() + k.slice(1) : "";
const live = (el, ctx) => new Proxy({}, { get: (_, k) => {
  if (typeof k !== "string") return undefined;
  if (k in nav) return nav[k];
  const f = key(read(el), k);
  if (f) return { get: () => read(el)[f], get value() { return read(el)[f]; }, set: (v) => ctx.push("_set", { [f]: v }) };
  return (...a) => ctx.push(k[0].toUpperCase() + k.slice(1), a.length > 1 ? a : a[0]);
} });
function hooks(start) {
  const insts = new WeakMap();
  return {
    mounted(el, ctx) { insts.set(el, { props: el.getAttribute("data-vk-p"), h: start(el, ctx) || {} }); },
    updated(el) {
      const i = insts.get(el), p = el.getAttribute("data-vk-p");
      if (i && p !== i.props) { i.props = p; if (i.h.update) i.h.update(read(el)); }
    },
    destroyed(el) { const i = insts.get(el); insts.delete(el); if (i && i.h.destroy) i.h.destroy(); },
  };
}
const context = (el, ctx) => ({ live: live(el, ctx), state: state(el), ...nav, push: ctx.push, emit: ctx.emit, on: ctx.on, ref: ctx.ref });
export const component = (fn) => hooks((el, ctx) => fn(el, read(el), context(el, ctx)));
export const page = (fn) => hooks((el, ctx) => { const c = context(el, ctx); return fn(el, c.live, c); });
`

// hookNames are the hooks the loader runs, not methods ui.JS.Call reaches.
var hookNames = []string{"mounted", "updated", "destroyed"}

// bundle is a component's module.
type bundle struct {
	name    string // its file name under /_vuka/c/: <scope>.<hash>.js
	js      []byte
	methods []string
	pushes  []string // the Go methods its scripts push by name
}

// bundleScripts bundles c's scripts, minified for a build, with an inline
// source map in development.
func bundleScripts(root string, c *assetComp, dev bool) (*bundle, error) {
	if len(c.scripts) == 0 {
		return nil, nil
	}
	methods, err := scriptMethods(root, c)
	if err != nil {
		return nil, err
	}
	pushes, err := scriptPushes(c)
	if err != nil {
		return nil, err
	}
	var entry strings.Builder
	var names []string
	for i := range c.scripts {
		fmt.Fprintf(&entry, "import * as h%d from %q;\n", i, "vuka-src:"+strconv.Itoa(i))
		names = append(names, fmt.Sprintf("h%d.default", i))
	}
	entry.WriteString("export default [" + strings.Join(names, ", ") + "];\n")
	opts := api.BuildOptions{
		Stdin:         &api.StdinOptions{Contents: entry.String(), ResolveDir: root, Sourcefile: "vuka-entry.js", Loader: api.LoaderJS},
		Bundle:        true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformBrowser,
		Target:        api.ES2020,
		Write:         false,
		AbsWorkingDir: root,
		LogLevel:      api.LogLevelSilent,
		Charset:       api.CharsetUTF8,
		Plugins:       []api.Plugin{sourcesPlugin(c)},
	}
	if dev {
		opts.Sourcemap = api.SourceMapInline
	} else {
		opts.MinifyWhitespace, opts.MinifyIdentifiers, opts.MinifySyntax = true, true, true
	}
	res := api.Build(opts)
	if len(res.Errors) > 0 {
		return nil, esbuildError(root, c, res.Errors)
	}
	if len(res.OutputFiles) == 0 {
		return nil, fmt.Errorf("vuka: esbuild wrote nothing for %s", c.goName())
	}
	js := res.OutputFiles[0].Contents
	sum := sha256.Sum256(js)
	id := c.id
	if id == "" {
		id = "app"
	}
	return &bundle{name: id + "." + hex.EncodeToString(sum[:4]) + ".js", js: js, methods: methods, pushes: pushes}, nil
}

// sourcesPlugin resolves the entry's imports to c's scripts, inline ones
// from memory, and refuses packages.
func sourcesPlugin(c *assetComp) api.Plugin {
	return api.Plugin{Name: "vuka", Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `^vuka-src:\d+$`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			i, _ := strconv.Atoi(strings.TrimPrefix(a.Path, "vuka-src:"))
			s := c.scripts[i]
			if s.path != "" {
				return api.OnResolveResult{Path: s.path}, nil
			}
			return api.OnResolveResult{Path: strconv.Itoa(i), Namespace: "vuka-inline"}, nil
		})
		b.OnResolve(api.OnResolveOptions{Filter: `^vuka:component$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: "component", Namespace: "vuka"}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "vuka"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
			src := componentModule
			return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
		})
		b.OnResolve(api.OnResolveOptions{Filter: `^\.\.?/`, Namespace: "vuka-inline"}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			i, _ := strconv.Atoi(a.Importer)
			return api.OnResolveResult{Path: filepath.Join(c.scripts[i].dir, a.Path)}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "vuka-inline"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
			i, _ := strconv.Atoi(a.Path)
			src := c.scripts[i].inline
			return api.OnLoadResult{Contents: &src, Loader: api.LoaderTS, ResolveDir: c.scripts[i].dir}, nil
		})
	}}
}

// esbuildError reports esbuild's errors at the scripts' lines.
func esbuildError(root string, c *assetComp, msgs []api.Message) error {
	var lines []string
	for _, m := range msgs {
		text := m.Text
		for _, n := range m.Notes {
			if n.Text != "" && !strings.Contains(n.Text, "mark the path") {
				text += " (" + n.Text + ")"
			}
		}
		if m.PluginName == "vuka" {
			text = strings.TrimPrefix(text, "[plugin vuka] ")
		}
		loc := m.Location
		if loc == nil {
			lines = append(lines, fmt.Sprintf("%s: %s", c.goName(), text))
			continue
		}
		file, line := loc.File, loc.Line
		if strings.HasPrefix(file, "vuka-inline:") {
			i, _ := strconv.Atoi(strings.TrimPrefix(file, "vuka-inline:"))
			s := c.scripts[i]
			f, l, _ := strings.Cut(s.label, ":")
			n, _ := strconv.Atoi(l)
			file, line = f, n+line-1
		} else if filepath.IsAbs(file) {
			file, _ = filepath.Rel(root, file)
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d: %s", filepath.ToSlash(file), line, loc.Column+1, text))
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

var (
	exportDefaultObj = regexp.MustCompile(`(?m)^export default \{`)
	exportDefaultVar = regexp.MustCompile(`(?m)^export default ([A-Za-z_$][\w$]*);$|^\s*([A-Za-z_$][\w$]*) as default,?$`)
	objectKey        = regexp.MustCompile(`^\s*(?:async\s+)?(?:\*\s*)?(?:(?:get|set)\s+)?([A-Za-z_$][\w$]*)\s*[(:]`)
)

// scriptMethods lists the methods c's scripts' hook objects export, but
// the hooks: what ui.JS.Call reaches. Two scripts exporting one method is
// an error naming both.
func scriptMethods(root string, c *assetComp) ([]string, error) {
	var methods []string
	from := map[string]string{}
	for _, s := range c.scripts {
		src, label := s.inline, s.label
		if s.path != "" {
			data, err := load.ReadSource(s.path)
			if err != nil {
				return nil, err
			}
			src = string(data)
		}
		loader := api.LoaderTS
		if ext := filepath.Ext(s.path); ext == ".js" || ext == ".mjs" {
			loader = api.LoaderJS
		}
		out := api.Transform(src, api.TransformOptions{Loader: loader, Format: api.FormatESModule, Target: api.ES2020, Sourcefile: label})
		if len(out.Errors) > 0 {
			var lines []string
			for _, m := range out.Errors {
				line := 0
				if m.Location != nil {
					line = m.Location.Line + s.line
				}
				lines = append(lines, fmt.Sprintf("%s:%d: %s", strings.Split(label, ":")[0], line, m.Text))
			}
			return nil, fmt.Errorf("%s", strings.Join(lines, "\n"))
		}
		for _, name := range hookKeys(string(out.Code)) {
			if slices.Contains(hookNames, name) {
				continue
			}
			if other, ok := from[name]; ok {
				return nil, fmt.Errorf("%s: %s and %s both export the method %s; ui.JS.Call(%q) would reach only one", c.goName(), other, label, name, name)
			}
			from[name] = label
			methods = append(methods, name)
		}
	}
	return methods, nil
}

// hookKeys are the keys of a module's default export, as esbuild prints
// it: export default { … }, or an object literal a variable holds (through
// variables holding it) exported as default.
func hookKeys(code string) []string {
	if m := exportDefaultObj.FindStringIndex(code); m != nil {
		return objectKeys(code, m[1]-1)
	}
	m := exportDefaultVar.FindStringSubmatch(code)
	if m == nil {
		return nil
	}
	name := m[1] + m[2]
	for range 5 {
		d := regexp.MustCompile(`(?m)^(?:const|let|var) ` + regexp.QuoteMeta(name) + ` = (\{|[A-Za-z_$][\w$]*;)`).FindStringSubmatchIndex(code)
		if d == nil {
			return nil
		}
		if code[d[2]] == '{' {
			return objectKeys(code, d[2])
		}
		name = strings.TrimSuffix(code[d[2]:d[3]], ";")
	}
	return nil
}

// objectKeys are the keys of the object literal whose { is at open.
func objectKeys(code string, open int) []string {
	var keys []string
	depth := 0
	for i := open; i < len(code); i++ {
		switch c := code[i]; c {
		case '"', '\'', '`':
			for i++; i < len(code) && code[i] != c; i++ {
				if code[i] == '\\' {
					i++
				}
			}
		case '/':
			if i+1 < len(code) && code[i+1] == '/' {
				i += strings.IndexByte(code[i:]+"\n", '\n')
			} else if i+1 < len(code) && code[i+1] == '*' {
				i += strings.Index(code[i:]+"*/", "*/") + 1
			}
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			if depth--; depth == 0 {
				return keys
			}
		}
		if depth == 1 && (code[i] == '{' || code[i] == ',') {
			if m := objectKey.FindStringSubmatch(code[i+1:]); m != nil {
				keys = append(keys, m[1])
			}
		}
	}
	return keys
}

// liveCall is a script's call of a live method, live.reorder(…), or set of
// a field, live.q.set(…).
var liveCall = regexp.MustCompile(`\blive\.([A-Za-z_$][\w$]*)\s*(\.\s*set\s*)?\(`)

// ctxPush is a script's ctx.push( and what follows it.
var ctxPush = regexp.MustCompile(`\bctx\.push\(\s*`)

// jsString is a string literal at the start of the text: "x", 'x' or `x`
// (without substitutions).
var jsString = regexp.MustCompile("^(?:\"([A-Za-z_]\\w*)\"|'([A-Za-z_]\\w*)'|`([A-Za-z_]\\w*)`)")

// scriptPushes lists the Go methods c's scripts push by name, each
// ctx.push("Name") they make: the only methods the browser may call by name
// (live.AllowPush). A name that isn't a string literal, or that names no
// method of the component, is an error at its line.
func scriptPushes(c *assetComp) ([]string, error) {
	var names []string
	var errs []string
	var methods []string
	known := false
	for _, s := range c.scripts {
		src, label, base := s.inline, s.label, s.line
		if s.path != "" {
			data, err := load.ReadSource(s.path)
			if err != nil {
				return nil, err
			}
			src, base = string(data), 0
		} else {
			label, _, _ = strings.Cut(label, ":")
		}
		code := blankComments(src)
		for _, m := range ctxPush.FindAllStringIndex(code, -1) {
			line := base + strings.Count(src[:m[0]], "\n") + 1
			lit := jsString.FindStringSubmatch(code[m[1]:])
			if lit == nil {
				errs = append(errs, fmt.Sprintf("%s:%d: ctx.push needs the method's name as a string literal (the vuka command allows the methods scripts name)", label, line))
				continue
			}
			name := lit[1] + lit[2] + lit[3]
			if !known {
				methods, known = componentMethods(c), true
			}
			switch {
			case !c.live:
				errs = append(errs, fmt.Sprintf("%s:%d: ctx.push(%q): %s is no live component; only a live component's methods can be pushed", label, line, name, c.goName()))
			case !slices.Contains(methods, name):
				msg := fmt.Sprintf("%s:%d: ctx.push(%q): %s has no method %s", label, line, name, c.name, name)
				if s := closest(name, methods); s != "" {
					msg += "; did you mean " + s + "?"
				}
				errs = append(errs, msg)
			case !slices.Contains(names, name):
				names = append(names, name)
			}
		}
		// live.reorder(ids) pushes Reorder, live.q.set(v) _set.
		for _, m := range liveCall.FindAllStringSubmatch(code, -1) {
			name := m[1]
			if m[2] != "" {
				name = "_set"
			} else if name == "navigate" || name == "patch" {
				continue
			} else {
				name = strings.ToUpper(name[:1]) + name[1:]
				if !known {
					methods, known = componentMethods(c), true
				}
				if !slices.Contains(methods, name) {
					continue
				}
			}
			if c.live && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return names, nil
}

// blankComments replaces a script's comments with spaces, keeping lines.
func blankComments(src string) string {
	b := []byte(src)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '"' || b[i] == '\'' || b[i] == '`':
			q := b[i]
			for i++; i < len(b) && b[i] != q && !(q != '`' && b[i] == '\n'); i++ {
				if b[i] == '\\' {
					i++
				}
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			for ; i < len(b) && !(b[i] == '*' && i+1 < len(b) && b[i+1] == '/'); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
			if i+1 < len(b) {
				b[i], b[i+1] = ' ', ' '
			}
		}
	}
	return string(b)
}

// componentMethods are the exported methods a live component's type
// declares in its package's .vuka and .go files that a script may push:
// not Render, View, Mount or an Update. Nil when it declares none.
func componentMethods(c *assetComp) []string {
	re := regexp.MustCompile(`(?m)^func\s*\(\s*\w*\s*\*?\s*` + regexp.QuoteMeta(c.name) + `(?:\[[^\]]*\])?\s*\)\s*([A-Z]\w*)\s*(?:\[[^\]]*\])?\(([^)]*)\)`)
	var out []string
	entries, _ := load.ReadDir(c.dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_test.vuka") {
			continue
		}
		src, ok := goSource(filepath.Join(c.dir, n))
		if !ok {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if name := m[1]; name != "Render" && name != "View" && name != "Update" && !isUpdateMethod(name, m[2]) && !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}
