//go:build ignore

// gen_icon_theme writes icon_themes/vuka.json, the "Vuka Icons" icon theme.
//
//	go run gen_icon_theme.go [icon_theme.rs path or URL]
//
// Zed matches a file's stems and suffixes against the active icon theme only:
// a theme's tables replace those of the bundled "Zed (Default)" theme instead
// of being merged with them. A theme that listed only Vuka's suffixes would
// show every other file with the generic icon, so this one carries the bundled
// theme's tables, read from FILE_STEMS_BY_ICON_KEY and FILE_SUFFIXES_BY_ICON_KEY
// in Zed's crates/theme/src/icon_theme.rs, plus Vuka's. Only the mappings are
// vendored: an icon key the theme doesn't define (rust, go, html, …) falls back
// to the bundled theme's icon, so no SVG of Zed's is copied. Rerun after
// bumping zedRev to pick up Zed's new mappings.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const zedRev = "31a3267183b8f1cdc35fc01532ebd9ca052990dc"

var source = "https://raw.githubusercontent.com/zed-industries/zed/" + zedRev + "/crates/theme/src/icon_theme.rs"

var vukaSuffixes = map[string]string{"vuka": "vuka", "vuka.html": "vuka"}

func main() {
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	src, err := read(source)
	if err != nil {
		fail(err)
	}
	stems, err := table(src, "FILE_STEMS_BY_ICON_KEY")
	if err != nil {
		fail(err)
	}
	suffixes, err := table(src, "FILE_SUFFIXES_BY_ICON_KEY")
	if err != nil {
		fail(err)
	}
	for k, v := range vukaSuffixes {
		suffixes[k] = v
	}

	theme := map[string]any{
		"$schema": "https://zed.dev/schema/icon_themes/v0.3.0.json",
		"name":    "Vuka Icons",
		"author":  "Vuka",
		"themes": []any{map[string]any{
			"name":          "Vuka Icons",
			"appearance":    "dark",
			"file_stems":    stems,
			"file_suffixes": suffixes,
			"file_icons":    map[string]any{"vuka": map[string]string{"path": "./icons/vuka.svg"}},
		}},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(theme); err != nil {
		fail(err)
	}
	if err := os.MkdirAll("icon_themes", 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile("icon_themes/vuka.json", buf.Bytes(), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("icon_themes/vuka.json: %d stems, %d suffixes\n", len(stems), len(suffixes))
}

func read(src string) (string, error) {
	if !strings.HasPrefix(src, "https://") {
		b, err := os.ReadFile(src)
		return string(b), err
	}
	resp, err := http.Get(src)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", src, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

var (
	entry  = regexp.MustCompile(`(?s)\(\s*"([^"]+)"\s*,\s*&\[(.*?)\]\s*,?\s*\)`)
	strLit = regexp.MustCompile(`"([^"]*)"`)
)

// table turns `const NAME: &[(&str, &[&str])] = &[("key", &["a", "b"]), …];`
// into {"a": "key", "b": "key", …}.
func table(src, name string) (map[string]string, error) {
	start := strings.Index(src, "const "+name+":")
	if start < 0 {
		return nil, fmt.Errorf("%s not found", name)
	}
	body := src[start:]
	open := strings.Index(body, "= &[")
	end := strings.Index(body, "\n];")
	if open < 0 || end < open {
		return nil, fmt.Errorf("%s: unexpected shape", name)
	}
	body = body[open+len("= &[") : end]
	out := map[string]string{}
	for _, m := range entry.FindAllStringSubmatch(body, -1) {
		for _, s := range strLit.FindAllStringSubmatch(m[2], -1) {
			out[s[1]] = m[1]
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no entries", name)
	}
	return out, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen_icon_theme:", err)
	os.Exit(1)
}
