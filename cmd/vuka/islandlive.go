package main

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Pages backed by a component: an @ui.Island struct routed with @web.Live
// (live) or @web.Get (stateless).
// vuka-live.d.ts is the types of the module "vuka:live" its component
// imports from — the frontend's tsconfig maps the name to it ("paths":
// {"vuka:live": ["./vuka-live.d.ts"]}), which also lets Vue's compiler
// resolve defineProps<T>(): an interface per @ui.Island struct — its exported fields as the
// props (json names), its callable methods and its fields' handles (live.q,
// settable for a bind: field, pending for a defer: one) as phantom __methods and __fields members —
// and useLive<T>() / live<T>() / useVuka() typed by them.

// liveMethod is a method of an island's struct its component may call.
type liveMethod struct {
	name   string      // the wire name: the Go name, first letter lower-cased
	params [][2]string // each parameter's name and Go type, context.Context left out
	scope  *tsScope    // where its parameters' types are read
}

var lifecycleMethods = map[string]bool{"Render": true, "Mount": true, "Update": true, "Info": true, "Changed": true,
	"ID": true, "Subscribe": true, "PushJS": true, "PushPatch": true, "PushNavigate": true, "PushEvent": true}

// methodRe finds a method declaration's receiver type, name and parameters.
var methodRe = regexp.MustCompile(`(?m)^func\s*\(\s*\w*\s*\*?\s*(\w+)\s*\)\s*([A-Z]\w*)\s*\(([^)]*)\)\s*([^{]*)\{`)

// islandLive reads, for the declared islands of a package dir, their
// callable methods, and maps the types they and the props name.
type islandLive struct {
	methods map[string][]liveMethod // island → methods
	types   *tsTypes
	dirs    []string
}

func readIslandLive(root, modPath string, dirs []string) *islandLive {
	il := &islandLive{methods: map[string][]liveMethod{}, types: newTSTypes(root, modPath), dirs: dirs}
	for _, dir := range dirs {
		pkg := il.types.pkg(dir)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !(strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".go")) || strings.Contains(n, "_test.") || strings.HasSuffix(n, "_vuka.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				continue
			}
			full, _ := scanFile(n, src)
			scope := &tsScope{pkg: pkg, imports: fileImports(dropNewlineSemis(full))}
			hooks := hookMethods(string(src))
			for _, m := range methodRe.FindAllStringSubmatch(string(src), -1) {
				if lifecycleMethods[m[2]] || m[2] == "LiveSchema" || hooks[m[1]+"."+m[2]] || isUpdateName(m[2], goParams(m[3])) {
					continue
				}
				res := strings.TrimSpace(m[4])
				if res != "" && res != "error" {
					continue
				}
				lm := liveMethod{name: strings.ToLower(m[2][:1]) + m[2][1:], scope: scope}
				for _, p := range goParams(m[3]) {
					if p[1] != "context.Context" {
						lm.params = append(lm.params, p)
					}
				}
				il.methods[m[1]] = append(il.methods[m[1]], lm)
			}
		}
	}
	return il
}

var (
	// schemaRe finds a LiveSchema method's receiver, its type and its body,
	// up to the method's closing brace at the start of a line.
	schemaRe = regexp.MustCompile(`(?ms)^func\s*\(\s*(\w+)\s+\*?\s*(\w+)\s*\)\s*LiveSchema\s*\(.*?^}`)
	// hookDecoRe finds a method a .vuka file marks @ui.Mount, @ui.OnChange
	// or @ui.Loads(…): its receiver type and name.
	hookDecoRe = regexp.MustCompile(`(?m)^@\w+\.(?:Mount|OnChange|Loads)\b[^\n]*\n(?:@[^\n]*\n)*func\s*\(\s*\w*\s*\*?\s*(\w+)\s*\)\s*(\w+)`)
)

// hookMethods are the methods a file makes hooks — given to l.Mount,
// l.OnChange or l.Loads in a LiveSchema, or marked @ui.Mount, @ui.OnChange
// or @ui.Loads — as "Type.Method": never called from the browser.
func hookMethods(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range schemaRe.FindAllStringSubmatch(src, -1) {
		recv := regexp.QuoteMeta(m[1])
		re := regexp.MustCompile(`\b(?:Mount|OnChange)\(\s*` + recv + `\.(\w+)\s*\)|\bLoads\(\s*"[^"]*"\s*,\s*` + recv + `\.(\w+)\s*\)`)
		for _, h := range re.FindAllStringSubmatch(m[0], -1) {
			out[m[2]+"."+h[1]+h[2]] = true
		}
	}
	for _, m := range hookDecoRe.FindAllStringSubmatch(src, -1) {
		out[m[1]+"."+m[2]] = true
	}
	return out
}

// isUpdateName reports whether a method is an Update overload, as
// ui.IsUpdate tells one: Update, or Update and its last parameter's type's
// name (UpdateNewMessage(m NewMessage)).
func isUpdateName(name string, params [][2]string) bool {
	switch {
	case name == "Update" || strings.HasPrefix(name, "Update__"):
		return true
	case !strings.HasPrefix(name, "Update") || len(params) == 0:
		return false
	}
	t := strings.TrimLeft(params[len(params)-1][1], "*")
	plural := strings.HasPrefix(t, "[]")
	t = strings.TrimLeft(strings.TrimPrefix(t, "[]"), "*")
	t = t[strings.LastIndex(t, ".")+1:]
	if t == "" {
		return false
	}
	t = strings.ToUpper(t[:1]) + t[1:]
	return name == "Update"+t || plural && strings.HasPrefix(name, "Update"+t)
}

// islandDecl is the declaration of the island named name.
func (il *islandLive) islandDecl(name string) *goDecl {
	for _, dir := range il.dirs {
		if d := il.types.pkg(dir).decls[name]; d != nil {
			return d
		}
	}
	return nil
}

// goParams splits a Go parameter list into (name, type) pairs, names
// sharing a type each given it.
func goParams(list string) [][2]string {
	var out [][2]string
	var pending []string
	for i, part := range strings.Split(list, ",") {
		f := strings.Fields(strings.TrimSpace(part))
		switch len(f) {
		case 0:
		case 1:
			pending = append(pending, f[0])
		default:
			typ := strings.Join(f[1:], " ")
			for _, n := range append(pending, f[0]) {
				out = append(out, [2]string{n, typ})
			}
			pending = nil
		}
		_ = i
	}
	for i, n := range pending { // types alone: unnamed parameters
		out = append(out, [2]string{fmt.Sprintf("arg%d", i), n})
	}
	return out
}

// liveDTS is vuka-live.d.ts: the "vuka:live" module, its interface per
// declared island (props and methods) and its hooks.
func liveDTS(islands []*islandComp, il *islandLive) []byte {
	var decl []*islandComp
	for _, ic := range islands {
		if !ic.imported {
			decl = append(decl, ic)
		}
	}
	if len(decl) == 0 {
		return nil
	}
	slices.SortFunc(decl, func(a, b *islandComp) int { return strings.Compare(a.name, b.name) })
	var b strings.Builder
	b.WriteString("// Code generated by vuka from the @ui.Island structs. DO NOT EDIT.\n")
	b.WriteString("// A page backed by a component: import type { T } from \"vuka:live\";\n")
	b.WriteString("// useLive<T>() (Svelte: live<T>()) — live.<field>.get(), .value, .set(v) for a\n")
	b.WriteString("// bind: field, live.<method>(…) — or defineProps<T>(). tsconfig.json maps\n")
	b.WriteString("// \"vuka:live\" here: \"paths\": { \"vuka:live\": [\"./vuka-live.d.ts\"] }.\n\n")
	r := il.types
	scopes := map[*islandComp]*tsScope{}
	for _, ic := range decl {
		if d := il.islandDecl(ic.name); d != nil {
			r.claim(d, ic.name)
			scopes[ic] = d.scope(nil)
		} else {
			scopes[ic] = &tsScope{}
		}
	}
	tsOf := func(ic *islandComp, p islandProp) string {
		t := p.typ
		if len(t) > 0 && t[0].tok == token.MUL {
			t = t[1:]
		}
		return r.ts(t, scopes[ic])
	}
	for _, ic := range decl {
		fmt.Fprintf(&b, "export interface %s {\n", ic.name)
		loaders := map[string]bool{}
		for _, p := range ic.props {
			fmt.Fprintf(&b, "\t%s\n", fieldTS(tsField{p.name, tsOf(ic, p), p.optional || p.deferred != ""}))
			if p.deferred != "" {
				loaders["load"+p.deferred] = true
			}
		}
		var ms []liveMethod
		for _, m := range il.methods[ic.name] {
			if !loaders[m.name] {
				ms = append(ms, m)
			}
		}
		if len(ms) > 0 {
			b.WriteString("\treadonly __methods?: {\n")
			for _, m := range ms {
				args := make([]string, len(m.params))
				for i, p := range m.params {
					full, _ := scanFile("", []byte(p[1]))
					args[i] = p[0] + ": " + r.ts(dropNewlineSemis(full), m.scope)
				}
				fmt.Fprintf(&b, "\t\t%s(%s): Promise<void>;\n", m.name, strings.Join(args, ", "))
			}
			b.WriteString("\t};\n")
		}
		if hs := handles(ic.props, ms); len(hs) > 0 {
			b.WriteString("\treadonly __fields?: {\n")
			for _, p := range hs {
				kind := "LiveField"
				switch {
				case p.bind:
					kind = "LiveBound"
				case p.deferred != "":
					kind = "LiveDeferred"
				}
				ts := tsOf(ic, p)
				if p.optional {
					ts += " | undefined"
				}
				fmt.Fprintf(&b, "\t\t%s: %s<%s>;\n", tsKey(strings.ToLower(p.name[:1])+p.name[1:]), kind, ts)
			}
			b.WriteString("\t};\n")
		}
		b.WriteString("}\n\n")
	}
	for i := 0; i < len(r.want); i++ {
		d := r.want[i]
		tp := ""
		if len(d.tparams) > 0 {
			tp = "<" + strings.Join(d.tparams, ", ") + ">"
		}
		fmt.Fprintf(&b, "export interface %s%s {\n", r.names[d], tp)
		for _, f := range r.declFields(d) {
			fmt.Fprintf(&b, "\t%s\n", fieldTS(f))
		}
		b.WriteString("}\n\n")
	}
	b.WriteString(`export interface LiveError { status?: number; message: string; fields?: Record<string, string>; }
/** A prop the component reads: get() or value. */
export interface LiveField<V> { get(): V; readonly value: V; }
/** A defer: prop, not in the first render: undefined and pending until its loader (Load<Group>) sends it; error is its loader's. */
export interface LiveDeferred<V> { get(): V | undefined; readonly value: V | undefined; readonly pending: boolean; readonly error: LiveError | null; }
/** A bind: prop the component also sets: set(v) (or value = v) is sent after its debounce, {now: true} at once; error is the server's message for it (its validate: tags), what was typed kept. */
export interface LiveBound<V> { get(): V; value: V; set(v: V, opts?: { now?: boolean }): Promise<void>; readonly error: string | null; }
export type LiveMethods<M> = { [K in keyof M]: M[K] & { readonly pending: boolean; readonly error: LiveError | null } };
export type Live<T> = (T extends { readonly __methods?: infer M } ? LiveMethods<NonNullable<M>> : {}) &
	(T extends { readonly __fields?: infer F } ? NonNullable<F> : {}) & Navigation & {
	readonly pending: boolean;
	readonly error: LiveError | null;
};
export interface Navigated { url: string; patch: boolean; }
/** navigate moves to another page, patch stays on this one (Update, else Mount again): over the live connection, as a data-vk-link would, else a page load; replace takes the current history entry's place. The promise resolves once the move is applied; it rejects for a URL that isn't http or https, or when another move takes its place. */
export interface Navigation {
	navigate(href: string, opts?: { replace?: boolean }): Promise<Navigated>;
	patch(href: string, opts?: { replace?: boolean }): Promise<Navigated>;
}
export interface Vuka extends Navigation { user: { id: string } | null; roles: string[]; perms: string[]; can(perm: string): boolean; }
/** A link moving the page over the live connection (data-vk-link); activeClass (default "active") while the address is href's path or below it (exact: the same path), aria-current="page" on the same path. */
export interface VukaLinkProps { href: string; patch?: boolean; replace?: boolean; activeClass?: string; exact?: boolean; }
export declare function useLive<T>(): Live<T>;
export declare function live<T>(): Live<T> & { subscribe(run: (value: Live<T>) => void): () => void };
export declare function useVuka(): Vuka;
`)
	b.WriteString(vukaLinkDTS(islands))
	return []byte(b.String())
}

// vukaLinkDTS declares VukaLink for the frameworks the islands use: each
// one's component type, joined when there are several.
func vukaLinkDTS(islands []*islandComp) string {
	var types []string
	for _, fw := range []string{"vue", "react", "svelte"} {
		if !slices.ContainsFunc(islands, func(ic *islandComp) bool { return ic.framework == fw }) {
			continue
		}
		switch fw {
		case "vue": // a class to React's JSX: beside React, Vue's functional form
			if slices.ContainsFunc(islands, func(ic *islandComp) bool { return ic.framework == "react" }) {
				types = append(types, "import('vue').FunctionalComponent<VukaLinkProps>")
			} else {
				types = append(types, "import('vue').DefineComponent<VukaLinkProps>")
			}
		case "react":
			types = append(types, "((props: VukaLinkProps & Omit<import('react').AnchorHTMLAttributes<HTMLAnchorElement>, 'href'>) => import('react').JSX.Element)")
		case "svelte":
			types = append(types, "import('svelte').Component<VukaLinkProps & Omit<import('svelte/elements').HTMLAnchorAttributes, 'href'> & { children?: import('svelte').Snippet }>")
		}
	}
	if len(types) == 0 {
		return ""
	}
	return "export declare const VukaLink: " + strings.Join(types, " & ") + ";\n"
}

// handles are the props a page's component gets a handle for: the plain
// values, but live's own names (pending, error, subscribe, navigate, patch)
// and a method's.
func handles(props []islandProp, ms []liveMethod) []islandProp {
	taken := map[string]bool{"pending": true, "error": true, "subscribe": true, "navigate": true, "patch": true}
	for _, m := range ms {
		taken[m.name] = true
	}
	var out []islandProp
	for _, p := range props {
		if h := strings.ToLower(p.name[:1]) + p.name[1:]; p.value && !taken[h] {
			taken[h] = true
			out = append(out, p)
		}
	}
	return out
}

// liveTSConfig is the tsconfig.json a frontend without one gets, so editors
// resolve "vuka:live" to vuka-live.d.ts.
func liveTSConfig() []byte {
	b, _ := scaffoldFS.ReadFile("scaffold/app/web@islands/tsconfig.json")
	return b
}
