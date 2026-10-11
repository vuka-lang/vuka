package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// A component's scripts (web.Script, web.JS) are TypeScript or JavaScript,
// bundled by esbuild into one ES module whose default export is the list of
// their default exports, in order: each a hook object, { mounted(el, ctx),
// updated(el, ctx), destroyed(el, ctx), …methods }. Relative imports are
// bundled; packages aren't resolved (there is no node_modules here).

// hookNames are the hooks the loader runs, not methods ui.JS.Call reaches.
var hookNames = []string{"mounted", "updated", "destroyed"}

// bundle is a component's module.
type bundle struct {
	name    string // its file name under /_vuka/c/: <scope>.<hash>.js
	js      []byte
	methods []string
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
	return &bundle{name: id + "." + hex.EncodeToString(sum[:4]) + ".js", js: js, methods: methods}, nil
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
		b.OnResolve(api.OnResolveOptions{Filter: `^[^./]`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			if a.Kind == api.ResolveEntryPoint {
				return api.OnResolveResult{}, nil
			}
			return api.OnResolveResult{}, fmt.Errorf("%s is a package; a component's scripts bundle relative imports only (./x.ts)", a.Path)
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
			data, err := os.ReadFile(s.path)
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
