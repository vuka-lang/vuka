package tsprops

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveIslandPath resolves a component import against its shape directory.
// Imports starting with @/ are relative to the main module root. The caller
// supplies that root; an empty root means the shape is outside a main module.
func ResolveIslandPath(moduleRoot, shapeDir, importPath string) (string, error) {
	if strings.HasPrefix(importPath, "@/") {
		if moduleRoot == "" {
			return "", fmt.Errorf("the main module root is unknown (no go.mod)")
		}
		rel := filepath.FromSlash(strings.TrimPrefix(importPath, "@/"))
		if !filepath.IsLocal(rel) {
			return "", fmt.Errorf("path escapes the module root")
		}
		return filepath.Join(moduleRoot, rel), nil
	}
	if shapeDir == "" {
		return "", fmt.Errorf("the package's directory is unknown")
	}
	return filepath.Join(shapeDir, filepath.FromSlash(importPath)), nil
}
