package main

import (
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/vuka-lang/vuka/diag"
	"github.com/vuka-lang/vuka/internal/load"
)

const buildIslandsUsage = `usage: vuka build-islands [-watch]

Builds the module's islands — the types ui.RegisterIsland, a route's
ui.Island option or @ui.Island registers — from the source alone, into the
.vuka directory of the frontend's build (web.Frontend's root), where
//go:embed picks them up and the app serves them from:

	//go:generate go tool vuka build-islands

Run it after the frontend's own build (vite build empties its dist). It
writes islands.d.ts and vuka-live.d.ts, the components' types, beside
them in the frontend's folder.

-watch keeps them built in .vuka/assets, for web.Dev under go run, until
its input closes.
`

func buildIslands(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build-islands", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, buildIslandsUsage) }
	watch := fs.Bool("watch", false, "keep .vuka/assets built until stdin closes")
	if err := fs.Parse(args); err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	all, err := findAssets(root, modPath)
	if err != nil {
		return err
	}
	pa := &pageAssets{root: root, modPath: modPath, global: &assetComp{}}
	if all != nil {
		pa.islands, pa.frontendRoot = all.islands, all.frontendRoot
	}
	if *watch {
		return watchIslands(root, pa, stderr)
	}
	if pa.empty() {
		fmt.Fprintln(stdout, "vuka: no islands to build")
		return nil
	}
	dist, err := frontendDist(root)
	if err != nil {
		return err
	}
	out, err := compileAssets(pa, false, stderr)
	if err != nil {
		return err
	}
	dir := filepath.Join(dist, ".vuka")
	if err := os.MkdirAll(filepath.Join(dir, "i"), 0o755); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, ib := range out.islands {
		keep[ib.name] = true
		if err := writeIfChanged(filepath.Join(dir, "i", ib.name), ib.js); err != nil {
			return err
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "i"))
	for _, e := range entries {
		if !keep[e.Name()] {
			os.Remove(filepath.Join(dir, "i", e.Name()))
		}
	}
	if err := writeIfChanged(filepath.Join(dir, assetsManifest), out.manifest); err != nil {
		return err
	}
	if len(out.dts) > 0 {
		writeIfChanged(filepath.Join(pa.frontendRoot, "islands.d.ts"), out.dts)
	}
	fmt.Fprintf(stdout, "vuka: %d island(s) built into %s\n", len(pa.islands), rel(root, dir))
	return nil
}

// watchIslands keeps the islands built in .vuka/assets, as vuka run does,
// until stdin closes (the app running it is gone) or a signal comes.
func watchIslands(root string, pa *pageAssets, stderr io.Writer) error {
	d := newDevAssets(root, stderr)
	defer d.close()
	d.set(pa)
	done := make(chan struct{}, 2)
	go func() { io.Copy(io.Discard, os.Stdin); done <- struct{}{} }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-done:
	case <-sig:
	}
	return nil
}

// frontendDist is the directory of the frontend's build: web.Frontend's
// root, beside the source calling it.
func frontendDist(root string) (string, error) {
	dist := ""
	walkSources(root, func(p, name string) error {
		ext := filepath.Ext(name)
		if dist != "" || ext != ".go" && ext != ".vuka" || strings.HasSuffix(name, "_test"+ext) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(src), `"`+webPath+`"`) {
			return nil
		}
		if d, ok := frontendDistIn(src); ok {
			dist = filepath.Join(filepath.Dir(p), filepath.FromSlash(d))
		}
		return nil
	})
	if dist == "" {
		return "", diag.New("E0609", "the islands have no frontend build to go into: the app calls no web.Frontend").
			WithHelp("embed the build the islands go into:\n\n\t//go:embed all:web/dist\n\tvar dist embed.FS\n\n\tapp := web.New(web.Frontend(dist, \"web/dist\"))")
	}
	return dist, nil
}

// frontendDistIn is the root argument of a source file's web.Frontend call,
// and whether there is one.
func frontendDistIn(src []byte) (string, bool) {
	full, _ := scanFile("", src)
	toks := dropNewlineSemis(full)
	webName := ""
	for n, p := range fileImports(toks) {
		if p == webPath {
			webName = n
		}
	}
	for i := 0; webName != "" && i+3 < len(toks); i++ {
		if toks[i].lit != webName || toks[i+1].tok != token.PERIOD || toks[i+2].lit != "Frontend" || toks[i+3].tok != token.LPAREN {
			continue
		}
		args := callArgs(toks, i+3)
		if len(args) < 2 || args[1][1]-args[1][0] != 1 {
			continue
		}
		if v, err := strconv.Unquote(toks[args[1][0]].lit); err == nil {
			return strings.Trim(path.Clean("/"+filepath.ToSlash(v)), "/"), true
		}
	}
	return "", false
}
