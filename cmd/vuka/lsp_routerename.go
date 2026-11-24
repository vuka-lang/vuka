package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// Renaming a route name, in route("…") and the other links by name, moves
// its folder: the rename is vuka mv's plan, as a workspace edit of the
// links and the folder's (or page file's) rename. It needs an editor that
// renames files in a workspace edit; others are told to run vuka mv.

// editorRenamesFiles: the editor applies a workspace edit's file renames.
var editorRenamesFiles atomic.Bool

// renamedByRoute are the folders a route rename's edit renames, by their
// path: the editor's willRenameFiles for them finds the links rewritten
// already.
var renamedByRoute sync.Map

// routeRenameRequest answers rename and prepareRename on a route name; it
// reports whether it did.
func (p *proxy) routeRenameRequest(m *rpcMsg, path string) bool {
	var params struct {
		Position lspPosition `json:"position"`
		NewName  string      `json:"newName"`
	}
	if !m.isRequest() || json.Unmarshal(m.Params, &params) != nil {
		return false
	}
	src := p.sourceText(path)
	if !bytes.Contains(src, []byte("oute")) {
		return false
	}
	off := offsetOf(src, params.Position)
	rc, ok := routeCallAt(src, off, strings.HasSuffix(path, ".go"))
	if !ok || !rc.inName(off) || !rc.nameClosed || p.namedRoutes()[rc.name] == nil {
		return false
	}
	p.untrack(m.ID)
	go func() {
		reply := map[string]any{"jsonrpc": "2.0", "id": m.ID}
		if res, err := p.routeRename(m.Method, src, rc, params.NewName); err != nil {
			reply["error"] = map[string]any{"code": -32803, "message": err.Error()}
		} else {
			reply["result"] = res
		}
		_ = p.editor.send(reply)
	}()
	return true
}

// routeRename is prepareRename's range or rename's workspace edit for the
// name of rc, a link in src.
func (p *proxy) routeRename(method string, src []byte, rc routeCall, newName string) (any, error) {
	if !editorRenamesFiles.Load() {
		return nil, fmt.Errorf("renaming the route %s moves its folder, and this editor doesn't rename files in an edit: run vuka mv", rc.name)
	}
	if method == "textDocument/prepareRename" {
		return map[string]any{"range": lspRange{positionOf(src, rc.nameStart+1), positionOf(src, rc.nameEnd-1)}, "placeholder": rc.name}, nil
	}
	routes, modRoot := p.routes()
	var r *folderRoute
	for _, x := range routes {
		if x.Name == rc.name && (r == nil || rank(x) < rank(r)) {
			r = x
		}
	}
	if r == nil || modRoot == "" {
		return nil, fmt.Errorf("the route %s isn't built yet: rename it once the build is done", rc.name)
	}
	from, to, err := renameTarget(r, newName)
	if err != nil {
		return nil, err
	}
	root, modPath, err := load.ModuleRoot(modRoot)
	if err != nil {
		return nil, err
	}
	plan, err := planMove(root, modPath, from, to)
	if err != nil {
		var refused *moveRefusal
		if errors.As(err, &refused) && len(refused.diags) > 0 {
			d := refused.diags[0]
			return nil, fmt.Errorf("%s: %s: %s", refused.msg, d.Code, d.Message)
		}
		return nil, err
	}
	for _, e := range plan.edits {
		if !bytes.Equal(p.sourceText(e.path), e.src) {
			return nil, fmt.Errorf("%s has changes not saved: save it, then rename", relPath(root, e.path))
		}
	}
	renamedByRoute.Store(plan.from, time.Now())
	return plan.workspaceEdit(func(path string) string { return pathToURI(p.display(path)) }), nil
}

// renameTarget is the move renaming the route r to name: the folder (or
// page file) of the one part of the name that changes.
func renameTarget(r *folderRoute, name string) (from, to string, err error) {
	if r.dir == nil {
		return "", "", fmt.Errorf("the route %s isn't a folder's", r.Name)
	}
	old, parts := strings.Split(r.Name, "."), strings.Split(name, ".")
	if len(parts) != len(old) {
		return "", "", fmt.Errorf("%s names %d folders and %s %d: a rename changes one, run vuka mv to move a route elsewhere", r.Name, len(old), name, len(parts))
	}
	at := -1
	for i := range parts {
		if parts[i] == old[i] {
			continue
		}
		if at >= 0 {
			return "", "", fmt.Errorf("%s and %s differ in more than one folder: rename one at a time, or run vuka mv", r.Name, name)
		}
		at = i
	}
	if at < 0 {
		return "", "", fmt.Errorf("%s is the route's name already", name)
	}
	seg := parts[at]
	if seg == "" || strings.ContainsAny(seg, `/\[]()`) || strings.HasPrefix(seg, "_") || strings.HasPrefix(seg, ".") {
		return "", "", fmt.Errorf("%q can't be a folder's name", seg)
	}
	// The parts of the name, each the folder (or file) it comes from.
	type part struct {
		dir   int // its folder's index in rel; -1 for the file
		param string
	}
	pages := strings.TrimSuffix(strings.TrimSuffix(r.dir.dir, filepath.FromSlash(r.dir.rel)), string(filepath.Separator))
	var rel []string
	if r.dir.rel != "" {
		rel = strings.Split(r.dir.rel, "/")
	}
	full := r.Name == routeFullName(r.Path) && r.Name != routeBaseName(r.Path)
	var named []part
	dirSegs := 0
	for i, f := range rel {
		switch {
		case load.Grouped(f):
			continue
		case load.Bracketed(f):
			dirSegs++
			if full {
				named = append(named, part{i, f})
			}
			continue
		}
		dirSegs++
		named = append(named, part{dir: i})
	}
	if len(splitPath(r.Path)) > dirSegs {
		base := filepath.Base(r.File)
		fs := fileSeg(base)
		switch {
		case load.Bracketed(fs):
			if full {
				named = append(named, part{-1, fs})
			}
		case fs != "" && (strings.HasSuffix(base, ".vuka.html") || r.typ == "" || r.fn != ""):
			named = append(named, part{dir: -1})
		default:
			named = append(named, part{dir: -2})
		}
	}
	if len(named) != len(old) {
		return "", "", fmt.Errorf("the route %s isn't named by its folders alone: run vuka mv", r.Name)
	}
	pt := named[at]
	newSeg := seg
	if pt.param != "" {
		newSeg = "[" + seg + "]"
		if isRest(pt.param) {
			newSeg = "[" + seg + "...]"
		}
	}
	switch pt.dir {
	case -2:
		return "", "", fmt.Errorf("%s is named by its type %s: rename the type", old[at], r.typ)
	case -1:
		return r.File, filepath.Join(filepath.Dir(r.File), newSeg+sourceExt(r.File)), nil
	}
	return filepath.Join(pages, filepath.Join(rel[:pt.dir+1]...)), filepath.Join(pages, filepath.Join(append(slices.Clone(rel[:pt.dir]), newSeg)...)), nil
}

// renamedForRoute reports whether a route rename's edit renames path, a
// minute ago at most.
func renamedForRoute(path string) bool {
	at, ok := renamedByRoute.LoadAndDelete(path)
	return ok && time.Since(at.(time.Time)) < time.Minute
}

// moveEdits is willRenameFiles's edit for a file or folder under pages/
// the editor moves from to to: vuka mv's edits of the links, nil when vuka
// mv would refuse the move or a file to edit has changes not saved.
func (p *proxy) moveEdits(modRoot, from, to string) any {
	if modRoot == "" || renamedForRoute(realPathOf(from)) {
		return nil
	}
	root, modPath, err := load.ModuleRoot(modRoot)
	if err != nil {
		return nil
	}
	plan, err := planMove(root, modPath, from, to)
	if err != nil || len(plan.edits) == 0 {
		return nil
	}
	changes := map[string]any{}
	for _, e := range plan.edits {
		if !bytes.Equal(p.sourceText(e.path), e.src) {
			return nil
		}
		changes[pathToURI(p.display(e.path))] = e.lspEdits()
	}
	return map[string]any{"changes": changes}
}

// lspEdits are the edit's changes as an editor's text edits on its source.
func (e fileEdit) lspEdits() []any {
	if !bytes.Equal(spliceEdits(e.src, slices.Clone(e.splices)), e.out) {
		return []any{map[string]any{"range": lspRange{lspPosition{}, positionOf(e.src, len(e.src))}, "newText": string(e.out)}}
	}
	var edits []any
	for _, s := range e.splices {
		edits = append(edits, map[string]any{"range": lspRange{positionOf(e.src, s.from), positionOf(e.src, s.to)}, "newText": s.text})
	}
	return edits
}

// workspaceEdit is the plan as an editor applies it: the edits of the
// sources, then the rename.
func (p *movePlan) workspaceEdit(uri func(string) string) map[string]any {
	var changes []any
	for _, e := range p.edits {
		changes = append(changes, map[string]any{"textDocument": map[string]any{"uri": uri(e.path), "version": nil}, "edits": e.lspEdits()})
	}
	changes = append(changes, map[string]any{"kind": "rename", "oldUri": uri(p.from), "newUri": uri(p.to)})
	return map[string]any{"documentChanges": changes}
}
