package tsprops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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
		clean := filepath.Clean(rel)
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, string(filepath.Separator)) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes the module root")
		}
		if !filepath.IsLocal(rel) {
			return "", fmt.Errorf("invalid component path for this platform")
		}
		return filepath.Join(moduleRoot, rel), nil
	}
	if shapeDir == "" {
		return "", fmt.Errorf("the package's directory is unknown")
	}
	return filepath.Join(shapeDir, filepath.FromSlash(importPath)), nil
}

// ReadIslandComponent resolves and reads an island, reporting its written and
// resolved paths once. Both relative and module-root imports use this check.
func ReadIslandComponent(moduleRoot, shapeDir, importPath string, read func(string) ([]byte, error)) (file string, src []byte, err error) {
	file, err = ResolveIslandPath(moduleRoot, shapeDir, importPath)
	if err != nil {
		return file, nil, fmt.Errorf("can't read island component %s: %w", importPath, err)
	}
	src, err = read(file)
	if err == nil {
		return file, src, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil, fmt.Errorf("can't read island component %s: %s doesn't exist", importPath, file)
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return file, nil, fmt.Errorf("can't read island component %s (resolved to %s): %w", importPath, file, err)
}
