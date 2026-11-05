package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// How gopls first loads the module with its generated files.
//
// gopls loads the workspace when it is given its folders (initialized),
// before any file is open, and a file opened afterwards that its load
// didn't have is loaded on its own (lsp_open.go): a go list of a few
// hundred milliseconds per generated file, for minutes in a module of a
// few hundred. gopls keeps an open file it has no folder for, though, and
// loads nothing for it; a folder added later is loaded with every file
// open in it.
//
// So gopls starts in a staging folder of the proxy's own, a module of one
// empty package. The first build sends gopls all its generated files at
// once, outside every folder gopls has, and only then are the workspace's
// folders put in the staging folder's place: gopls's workspace load is a
// single go list, with every generated file in it. Meanwhile gopls warns
// that no folder of its has the generated files (outsideFolders), and
// reports on the staging folder's files: neither reaches the editor.

// staging is the folder gopls starts in, and the workspace's folders it is
// given once it has the first build's files.
type staging struct {
	folder  any
	file    string // its Go file
	folders []any
}

// stage puts a staging folder in the place of the workspace's folders in the
// initialize params raw, and records them for unstage. Without folders, or
// without a staging folder, gopls gets raw as it is.
func (p *proxy) stage(raw json.RawMessage) json.RawMessage {
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return raw
	}
	folders, _ := params["workspaceFolders"].([]any)
	if u, _ := params["rootUri"].(string); len(folders) == 0 && u != "" {
		folders = []any{map[string]any{"uri": u, "name": filepath.Base(uriToPath(u))}}
	}
	if len(folders) == 0 {
		return raw
	}
	dir, err := stagingModule(p.tmp)
	if err != nil {
		p.logf("staging folder: %v", err)
		return raw
	}
	folder := map[string]any{"uri": pathToURI(dir), "name": "vuka-staging"}
	params["workspaceFolders"], params["rootUri"], params["rootPath"] = []any{folder}, folder["uri"], dir
	out, err := json.Marshal(params)
	if err != nil {
		return raw
	}
	p.mu.Lock()
	p.stagingDir = dir
	p.staged = &staging{folder: folder, file: filepath.Join(dir, "staging.go"), folders: folders}
	p.mu.Unlock()
	return out
}

// stagingModule writes the staging folder under tmp: a module with one
// package, which gopls loads without a complaint.
func stagingModule(tmp string) (string, error) {
	dir := filepath.Join(tmp, "staging")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module vuka.lsp/staging\n\ngo 1.21\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "staging.go"), []byte(stagingSource), 0o644); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(dir) // gopls's paths are real ones
}

const stagingSource = "package staging\n"

// unstage gives gopls the workspace's folders in the staging folder's
// place, once the first build has sent it its files.
func (p *proxy) unstage() {
	p.view.Lock() // after the first build's files, before anything else
	defer p.view.Unlock()
	p.mu.Lock()
	s := p.staged
	p.staged = nil
	p.mu.Unlock()
	if s == nil {
		return
	}
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "workspace/didChangeWorkspaceFolders", "params": map[string]any{
		"event": map[string]any{"added": s.folders, "removed": []any{s.folder}}}})
	// gopls remembers, for each open file, the folder it found it in, none
	// for the generated files, until a file is opened or closed: the staging
	// folder's own file is, so that it looks again.
	doc := map[string]any{"uri": pathToURI(s.file)}
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": doc["uri"], "languageId": "go", "version": 1, "text": stagingSource}}})
	_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{"textDocument": doc}})
}

// outsideFolders reports whether a gopls diagnostic of a generated file is
// its warning that no folder of its contains the file: true of each while
// gopls is staged, and about a file the editor never shows.
func outsideFolders(d any) bool {
	m, _ := d.(map[string]any)
	msg, _ := m["message"].(string)
	return strings.HasPrefix(msg, "No active builds contain ")
}
