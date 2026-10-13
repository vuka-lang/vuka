package islandui

import (
	"encoding/json"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vuka-lang/vuka"
)

// Islands are client components hydrated in the browser — a framework
// component (Vue/React/Svelte) or a vanilla module — placed in a server-
// rendered page and brought to life on a client:* strategy. A struct type
// annotated with @ui.Island names the component file and declares the props;
// the transpiler lowers a tag of that type to MountIsland. See
// docs/design/islands.md.

// islandInfo is a registered island: the component file, the framework
// inferred from its extension, and the default hydration strategy (empty when
// the decorator set none).
type islandInfo struct {
	Name      string
	Path      string
	Framework string
	Strategy  string
}

var islandReg sync.Map // reflect.Type (the props struct) -> *islandInfo

// Island is the @ui.Island type decorator. @ui.Island("islands/Chart.vue")
// above a struct type registers that type as an island whose props are its
// exported fields; an optional strategy sets the default used when a tag gives
// no client:* directive. The framework is inferred from the file extension
// (.vue, .tsx/.jsx, .svelte, else vanilla .ts/.js).
func Island(componentPath string, strategy ...string) func(*vuka.Type) {
	return func(t *vuka.Type) {
		info := &islandInfo{
			Name:      shortName(t.Name),
			Path:      componentPath,
			Framework: frameworkOf(componentPath),
		}
		if len(strategy) > 0 {
			info.Strategy = strategy[0]
		}
		islandReg.Store(t.Reflect, info)
	}
}

func shortName(qualified string) string {
	if i := strings.LastIndex(qualified, "."); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

func frameworkOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".vue":
		return "vue"
	case ".tsx", ".jsx":
		return "react"
	case ".svelte":
		return "svelte"
	default:
		return "vanilla"
	}
}

func lookupIsland(t reflect.Type) *islandInfo {
	if v, ok := islandReg.Load(t); ok {
		return v.(*islandInfo)
	}
	return nil
}

// IslandOpt configures a MountIsland call: a hydration strategy, a slot, or a
// fallback. The transpiler emits these from a tag's client:* directive, its
// children, and a <ui.Template fallback>.
type IslandOpt func(*islandConfig)

type islandConfig struct {
	strategy string
	arg      string
	slot     Node
	fallback Node
	ssr      bool
}

// Load hydrates the island immediately on page load (client:load).
func Load() IslandOpt { return func(c *islandConfig) { c.strategy = "load" } }

// Idle hydrates on requestIdleCallback (client:idle).
func Idle() IslandOpt { return func(c *islandConfig) { c.strategy = "idle" } }

// Visible hydrates when the island scrolls into view (client:visible); an
// optional rootMargin widens the trigger. This is the default strategy.
func Visible(rootMargin ...string) IslandOpt {
	return func(c *islandConfig) {
		c.strategy = "visible"
		if len(rootMargin) > 0 {
			c.arg = rootMargin[0]
		}
	}
}

// Media hydrates when the media query matches (client:media).
func Media(query string) IslandOpt {
	return func(c *islandConfig) { c.strategy, c.arg = "media", query }
}

// ClientOnly never server-renders the island (client:only): the fallback shows
// until the client mounts it fresh.
func ClientOnly() IslandOpt { return func(c *islandConfig) { c.strategy = "only" } }

// SSR opts the island into a server-rendered initial HTML via the build's SSR
// sidecar (server:defer); the client then hydrates it. Phase 4.
func SSR() IslandOpt { return func(c *islandConfig) { c.ssr = true } }

// Slot sets the island's slot — the component's own children, handed to the
// framework on mount. Distinct from a Fallback.
func Slot(n Node) IslandOpt { return func(c *islandConfig) { c.slot = n } }

// Fallback sets what shows until the island hydrates (and the SSR target under
// SSR). Distinct from the component's Slot.
func Fallback(n Node) IslandOpt { return func(c *islandConfig) { c.fallback = n } }

// Template marks a named region slot inside a <ui.Island> wrapper — e.g.
// <ui.Template fallback>…</ui.Template> gives the island's pre-hydration
// fallback. The transpiler consumes these tags (they render no element of
// their own); this declaration only lets the tag resolve. Calling it directly
// renders nothing.
func Template(...any) Node { return Fragment() }

var islandSeq atomic.Uint64

// MountIsland renders the <vk-island> host the client loader hydrates. The
// transpiler emits it for a tag whose type carries @ui.Island; props is that
// type's value (its exported fields are serialized as JSON for the client).
func MountIsland(props any, opts ...IslandOpt) Node {
	var cfg islandConfig
	for _, o := range opts {
		o(&cfg)
	}

	info := lookupIsland(reflect.TypeOf(props))
	if info == nil {
		return El("vk-island", []Attr{{"data-vk-error", "island not registered (missing @ui.Island?)"}})
	}

	strategy := cfg.strategy
	if strategy == "" {
		strategy = info.Strategy
	}
	if strategy == "" {
		strategy = "visible"
	}

	attrs := []Attr{
		{"data-vk-s", "i" + strconv.FormatUint(islandSeq.Add(1), 36)},
		{"data-vk-island", info.Name},
		{"data-vk-client", strategy},
	}
	switch strategy {
	case "media":
		if cfg.arg != "" {
			attrs = append(attrs, Attr{"data-vk-media", cfg.arg})
		}
	case "visible":
		if cfg.arg != "" {
			attrs = append(attrs, Attr{"data-vk-rootmargin", cfg.arg})
		}
	}
	if j, err := json.Marshal(props); err == nil {
		attrs = append(attrs, Attr{"data-vk-props", string(j)})
	}
	if cfg.ssr {
		attrs = append(attrs, Attr{"data-vk-ssr", true})
	}

	var kids []Node
	if cfg.fallback != nil {
		kids = append(kids, cfg.fallback)
	}
	if cfg.slot != nil {
		kids = append(kids, El("template", []Attr{{"data-vk-slot", true}}, cfg.slot))
	}
	return El("vk-island", attrs, kids...)
}
