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
	return dir, liveDTS([]*islandComp{ic}, readIslandLive(dir, "", []string{dir}))
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
	for _, c := range []struct {
		dts func(*testing.T) (string, []byte)
		use string
	}{{levelsDTS, levelsUse}, {chasingDTS, chasingUse}} {
		dir, dts := c.dts(t)
		os.WriteFile(filepath.Join(dir, "vuka-live.d.ts"), dts, 0o644)
		os.WriteFile(filepath.Join(dir, "use.ts"), []byte(c.use), 0o644)
		cmd := exec.Command(tsc, "--noEmit", "--strict", "--target", "es2020", "--module", "esnext", "--moduleResolution", "bundler", "use.ts")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc: %v\n%s\n%s", err, out, dts)
		}
	}
}

const chasingSrc = `package settings

import (
	"context"

	"example.com/app/modules/shared"
)

@ui.Island
type AcademicLevels struct {
	ui.Live
	Current shared.Level ` + "`json:\"current\" bind:\"current\"`" + `
	Rows    Page[Tag]    ` + "`json:\"rows\"`" + `
}

type Data struct {
	Audit
	Level  shared.Level       ` + "`json:\"level\"`" + `
	Tags   []Tag              ` + "`json:\"tags\"`" + `
	Meta   map[string]Meta    ` + "`json:\"meta\"`" + `
	Parent *Data              ` + "`json:\"parent\"`" + `
	Note   string             ` + "`json:\"note,omitempty\"`" + `
	Secret string             ` + "`json:\"-\"`" + `
}

type Audit struct {
	By   string ` + "`json:\"by\"`" + `
	Tags int    ` + "`json:\"tags\"`" + `
}

type Tag struct{ Name string ` + "`json:\"name\"`" + ` }

type Meta struct {
	Count int ` + "`json:\"count\"`" + `
}

type Page[T any] struct {
	Items []T ` + "`json:\"items\"`" + `
	Total int ` + "`json:\"total\"`" + `
}

func (p *AcademicLevels) Chasing(ctx context.Context, name string, data Data) error { return nil }
`

const sharedSrc = `package shared

type Level struct {
	Code  string ` + "`json:\"code\"`" + `
	Group Group  ` + "`json:\"group\"`" + `
	Kind  Kind   ` + "`json:\"kind\"`" + `
}

type Group struct {
	Name  string ` + "`json:\"name\"`" + `
	Years int    ` + "`json:\"years\"`" + `
}

type Kind string
`

func chasingDTS(t *testing.T) (dir string, dts []byte) {
	t.Helper()
	root := t.TempDir()
	for name, src := range map[string]string{"go.mod": "module example.com/app\n", "modules/settings/levels.vuka": chasingSrc, "modules/shared/shared.go": sharedSrc} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755)
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir = filepath.Join(root, "modules/settings")
	full, _ := scanFile("levels.vuka", []byte(chasingSrc))
	ic := &islandComp{name: "AcademicLevels", props: structFields(full, "AcademicLevels")}
	return dir, liveDTS([]*islandComp{ic}, readIslandLive(root, "example.com/app", []string{dir}))
}

// TestLiveDTSParamTypes: the types a method's parameters and the fields name
// are declared at every depth — another package's, slices, maps, pointers,
// recursion, embedded fields promoted as JSON does, generics.
func TestLiveDTSParamTypes(t *testing.T) {
	_, dts := chasingDTS(t)
	for _, want := range []string{
		"\t\tchasing(name: string, data: Data): Promise<void>;",
		"\tcurrent: Level;",
		"\trows: Page<Tag>;",
		"\t\tcurrent: LiveBound<Level>;",
		"export interface Level {\n\tcode: string;\n\tgroup: Group;\n\tkind: string;\n}",
		"export interface Group {\n\tname: string;\n\tyears: number;\n}",
		"export interface Data {\n\tby: string;\n\tlevel: Level;\n\ttags: Tag[];\n\tmeta: Record<string, Meta>;\n\tparent?: Data | null;\n\tnote?: string;\n}",
		"export interface Page<T> {\n\titems: T[];\n\ttotal: number;\n}",
		"export interface Tag {\n\tname: string;\n}",
		"export interface Meta {\n\tcount: number;\n}",
	} {
		if !strings.Contains(string(dts), want) {
			t.Errorf("lacks %q:\n%s", want, dts)
		}
	}
	if strings.Count(string(dts), "export interface Data ") != 1 || strings.Contains(string(dts), "Secret") || strings.Contains(string(dts), "tags: number") {
		t.Errorf("Data once, without Secret or the shadowed tags:\n%s", dts)
	}
	if _, again := chasingDTS(t); string(again) != string(dts) {
		t.Errorf("not stable:\n%s\n%s", dts, again)
	}
}

const chasingUse = `import type { Live, AcademicLevels, Data } from './vuka-live'
declare const live: Live<AcademicLevels>
live.chasing('x', { by: 'me', level: { code: 'A', group: { name: 'g', years: 1 }, kind: 'k' }, tags: [{ name: 't' }], meta: { a: { count: 1 } }, parent: null }).then(() => {})
const d: Data = { by: '', level: { code: '', group: { name: '', years: 0 }, kind: '' }, tags: [], meta: {}, parent: { by: '', level: { code: '', group: { name: '', years: 0 }, kind: '' }, tags: [], meta: {} } }
const years: number = live.current.value.group.years
const total: number = live.rows.get().items.length + live.rows.value.total
// @ts-expect-error a wrong nested field
live.chasing('x', { by: 'me', level: { code: 'A', group: { name: 'g', years: '1' }, kind: 'k' }, tags: [], meta: {}, parent: null })
// @ts-expect-error an unknown field
live.chasing('x', { by: 'me', level: { code: 'A', group: { name: 'g', years: 1 }, kind: 'k' }, tags: [{ name: 't', extra: 1 }], meta: {}, parent: null })
// @ts-expect-error a missing field
live.chasing('x', { level: { code: 'A', group: { name: 'g', years: 1 }, kind: 'k' }, tags: [], meta: {}, parent: null })
export { d, years, total }
`
