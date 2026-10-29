package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// warm builds each example project the way a sandboxed build does, filling
// the build cache the image ships.
func warm(cfg config, dirs []string) error {
	tmpl, err := loadTemplate(cfg.gomod, cfg.vukaSrc)
	if err != nil {
		return err
	}
	vuka, err := exec.LookPath(cfg.vuka)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		files := map[string]string{}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			if badPath(filepath.ToSlash(rel)) != "" {
				return nil
			}
			b, err := os.ReadFile(p)
			files[filepath.ToSlash(rel)] = string(b)
			return err
		})
		if err != nil {
			return err
		}
		tmp, err := os.MkdirTemp("", "warm-")
		if err != nil {
			return err
		}
		if err := tmpl.write(tmp, files); err != nil {
			return err
		}
		c := exec.Command(vuka, buildArgs(true)...)
		c.Dir = tmp
		c.Env = append(os.Environ(), buildEnv()...)
		out, err := c.CombinedOutput()
		os.RemoveAll(tmp)
		if err != nil {
			return fmt.Errorf("%s: %v\n%s", dir, err, strings.TrimSpace(string(out)))
		}
		fmt.Printf("warm: %s\n", dir)
	}
	return nil
}
