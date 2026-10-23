package main

import (
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A .vuka.html file in the Go-header form (vukavet's W0107) gets a code
// action writing its Go in a <script lang="go">: transpile.ToScriptForm, the
// rewrite vuka fix shape-script and vuka fmt -rewrite make, formatted.

const shapeScriptTitle = `Write the Go in <script lang="go">`

// withShapeScriptAction adds the quick fix to a code action answer for a
// shape in the Go-header form.
func (p *proxy) withShapeScriptAction(vf *vfile, v any) any {
	if !isShape(vf) {
		return v
	}
	src := vf.text()
	out, ok := transpile.ToScriptForm(src)
	if !ok {
		return v
	}
	if f, err := formatTemplateFile(vf.source, out, false); err == nil {
		out = f
	}
	whole := lspRange{Start: positionOf(src, 0), End: positionOf(src, len(src))}
	uri := pathToURI(p.display(vf.source))
	action := map[string]any{"title": shapeScriptTitle, "kind": "refactor.rewrite",
		"edit": map[string]any{"changes": map[string]any{uri: []any{map[string]any{"range": whole, "newText": string(out)}}}}}
	list, _ := v.([]any)
	return append(list, action)
}
