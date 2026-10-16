package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const levelsSrc = `package app

@web.Get("/levels")
@ui.Island("islands/Levels.vue")
type Levels struct {
	ui.Live
	Q      string  ` + "`url:\"q\" bind:\"q\"`" + `
	Page   int     ` + "`json:\"page\" bind:\"page,debounce=0\"`" + `
	Levels []Level
	Error  string
	OnPick func(id int64)
	db     *DB
}

type Level struct {
	ID   int64  ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

func (p *Levels) Mount(ctx context.Context) error   { return nil }
func (p *Levels) Changed(ctx context.Context) error { return nil }
func (p *Levels) Delete(ctx context.Context, id int64) error { return nil }
`

func levelsDTS(t *testing.T) (dir string, dts []byte) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "levels.vuka"), []byte(levelsSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	full, _ := scanFile("levels.vuka", []byte(levelsSrc))
	ic := &islandComp{name: "Levels", props: structFields(full, "Levels")}
	return dir, liveDTS([]*islandComp{ic}, readIslandLive([]string{dir}))
}

// TestLiveDTSHandles: a page's interface carries its fields' handles —
// settable for a bind: field, read-only otherwise, none for functions or
// live's own names — and its methods, not Changed or Mount.
func TestLiveDTSHandles(t *testing.T) {
	_, dts := levelsDTS(t)
	for _, want := range []string{
		"\tQ: string;",
		"\tLevels: Level[];",
		"\t\tdelete(id: number): Promise<void>;",
		"\treadonly __fields?: {\n\t\tq: LiveBound<string>;\n\t\tpage: LiveBound<number>;\n\t\tlevels: LiveField<Level[]>;\n\t};",
		"export interface Level {\n\tid: number;\n\tname: string;\n}",
		"export interface LiveBound<V>",
	} {
		if !strings.Contains(string(dts), want) {
			t.Errorf("lacks %q:\n%s", want, dts)
		}
	}
	for _, no := range []string{"changed(", "mount(", "error: LiveField", "onPick: Live", "db"} {
		if strings.Contains(string(dts), no) {
			t.Errorf("has %q:\n%s", no, dts)
		}
	}
}

const levelsUse = `import type { Live, Levels, Level } from './vuka-live'
declare const live: Live<Levels>
const q: string = live.q.get()
live.q.value = 'x'
live.q.set('y', { now: true }).then(() => {})
live.page.set(2)
const rows: Level[] = live.levels.value
const first: Level[] = live.levels.get()
live.delete(1).then(() => {})
const busy: boolean = live.delete.pending || live.pending
const why: string | undefined = live.delete.error?.message
// @ts-expect-error a read-only field has no set
live.levels.set([])
// @ts-expect-error a read-only field's value
live.levels.value = []
// @ts-expect-error Changed isn't callable
live.changed()
// @ts-expect-error q is a string
live.q.set(1)
export { q, rows, first, busy, why }
`

// TestLiveDTSTypeChecks: the types check a component's use of them, and
// refuse a set of a read-only field (tsc, installed with npm).
func TestLiveDTSTypeChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("installs typescript")
	}
	tsc := os.Getenv("VUKA_TSC")
	if tsc == "" {
		npm, err := exec.LookPath("npm")
		if err != nil {
			t.Skip("no npm")
		}
		nm := t.TempDir()
		os.WriteFile(filepath.Join(nm, "package.json"), []byte(`{"name":"x","private":true}`), 0o644)
		cmd := exec.Command(npm, "install", "--prefer-offline", "--no-audit", "--no-fund", "-D", "typescript@~6.0")
		cmd.Dir = nm
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("npm install typescript failed (offline?): %v\n%s", err, out)
		}
		tsc = filepath.Join(nm, "node_modules", ".bin", "tsc")
	}
	dir, dts := levelsDTS(t)
	os.WriteFile(filepath.Join(dir, "vuka-live.d.ts"), dts, 0o644)
	os.WriteFile(filepath.Join(dir, "use.ts"), []byte(levelsUse), 0o644)
	cmd := exec.Command(tsc, "--noEmit", "--strict", "--target", "es2020", "--module", "esnext", "--moduleResolution", "bundler", "use.ts")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s\n%s", err, out, dts)
	}
}
