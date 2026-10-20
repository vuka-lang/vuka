package main

import (
	"bytes"
	"fmt"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/internal/format"
	"github.com/vuka-lang/vuka/internal/load"
)

// The orm's earlier forms the fixers rewrite: struct tags of models
// (orm:"elements:…") as attributes (attributeTags), orm.Schema values as
// orm.DBSchema (db-schema), and queries written against orm.Query, which
// Post.Objects was before it was the model's Manager (queries).

const ormFnPath = ormModule + "/fn"

// importedAs is the name src imports path as, "" when it doesn't.
func importedAs(src []byte, path string) string {
	re := regexp.MustCompile(`(?m)^\s*(?:import\s+)?(\w+\s+)?"` + regexp.QuoteMeta(path) + `"`)
	g := re.FindSubmatch(src)
	if g == nil {
		return ""
	}
	if alias := strings.TrimSpace(string(g[1])); alias != "" {
		if alias == "_" || alias == "." {
			return ""
		}
		return alias
	}
	return path[strings.LastIndexByte(path, '/')+1:]
}

// ormTagAttrs are the attributes a model field's orm: tag says, and the
// rest of the tag: what has no attribute stays in it.
func ormTagAttrs(tag, orm string) (attrs []string, rest string) {
	var keep []string
	for _, kv := range tagPairs(tag) {
		if kv[0] != "orm" {
			keep = append(keep, kv[0]+":"+strconv.Quote(kv[1]))
			continue
		}
		a, r := ormTagValue(kv[1], orm)
		attrs = append(attrs, a...)
		if r != "" {
			keep = append(keep, "orm:"+strconv.Quote(r))
		}
	}
	return attrs, strings.Join(keep, " ")
}

func ormTagValue(v, orm string) (attrs []string, rest string) {
	if v == "-" {
		return []string{"@" + orm + ".Skip"}, ""
	}
	var keep, rel []string
	for part := range strings.SplitSeq(v, ";") {
		part = strings.TrimSpace(part)
		k, val, _ := strings.Cut(part, ":")
		attr := ""
		switch strings.ToLower(k) {
		case "":
			continue
		case "column":
			attr = "Column(" + strconv.Quote(val) + ")"
		case "pk":
			attr = "PK"
		case "auto_now_add":
			attr = "AutoNowAdd"
		case "auto_now":
			attr = "AutoNow"
		case "unique":
			attr = "Unique"
		case "index":
			attr = "Index"
		case "type":
			attr = "DBType(" + strconv.Quote(val) + ")"
		case "vector":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				attr = "Dims(" + val + ")"
			}
		case "elements":
			col, ref, _ := strings.Cut(val, ",")
			attr = "Elements{Column: " + strconv.Quote(col)
			if ref != "" {
				attr += ", Ref: " + strconv.Quote(ref)
			}
			attr += "}"
		case "on_delete":
			od := map[string]string{"cascade": "Cascade", "set_null": "SetNull", "restrict": "Restrict"}[strings.ToLower(val)]
			if od == "" {
				rel = append(rel, "OnDelete: "+strconv.Quote(val))
			} else {
				rel = append(rel, "OnDelete: "+orm+"."+od)
			}
			continue
		case "related":
			rel = append(rel, "Related: "+strconv.Quote(val))
			continue
		case "crossdb":
			rel = append(rel, "CrossDB: true")
			continue
		}
		if attr == "" {
			keep = append(keep, part)
			continue
		}
		attrs = append(attrs, "@"+orm+"."+attr)
	}
	if len(rel) > 0 {
		attrs = append(attrs, "@"+orm+".Rel{"+strings.Join(rel, ", ")+"}")
	}
	return attrs, strings.Join(keep, ";")
}

// ormHasDBSchema reports whether the orm the module in root requires
// names its schema value DBSchema (orm.Schema being a model's Schema
// method's since).
func ormHasDBSchema(root string) bool {
	c := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", ormModule)
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		return false
	}
	src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "manager.go"))
	return err == nil && bytes.Contains(src, []byte("\ntype DBSchema "))
}

// fixDBSchema rewrites orm.Schema values — orm.Of[T](orm.Schema{Names: …}),
// a variable of the type — as orm.DBSchema, the type's name since
// orm.Schema is what a model's Schema method takes (*orm.Schema stays).
func fixDBSchema(m *fixModule) ([]fixChange, error) {
	if !ormHasDBSchema(m.root) {
		return nil, nil
	}
	var changes []fixChange
	err := walkModule(m.root, func(path string) error {
		if !strings.HasSuffix(path, ".vuka") && !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, lines := dbSchemaRefs(src)
		if len(lines) == 0 {
			return nil
		}
		rel, _ := filepath.Rel(m.root, path)
		changes = append(changes, fixChange{
			what:  fmt.Sprintf("in %s, orm.Schema → orm.DBSchema on line %s", rel, strings.Join(lines, ", ")),
			apply: func() error { return os.WriteFile(path, out, 0o644) },
		})
		return nil
	})
	return changes, err
}

// dbSchemaRefs is src with its orm.Schema values as orm.DBSchema, and the
// lines changed.
func dbSchemaRefs(src []byte) ([]byte, []string) {
	orm := importedAs(src, ormModule)
	if orm == "" || !bytes.Contains(src, []byte(orm+".Schema")) {
		return src, nil
	}
	toks, line := scanFile("", src)
	var b bytes.Buffer
	var lines []string
	last := 0
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].tok != token.IDENT || toks[i].lit != orm || toks[i+1].tok != token.PERIOD || toks[i+2].lit != "Schema" ||
			i > 0 && (toks[i-1].tok == token.MUL || toks[i-1].tok == token.PERIOD) {
			continue
		}
		at := toks[i+2].off
		b.Write(src[last:at])
		b.WriteString("DB")
		last = at
		lines = append(lines, strconv.Itoa(line(at)))
	}
	b.Write(src[last:])
	return b.Bytes(), lines
}

// fixQueries rewrites the queries of .vuka files written against
// orm.Query, before Post.Objects was the model's Manager and Post.Title
// its typed field: vuka.Or of a model's fields is orm.Or, the deprecated
// package fn's functions the orm's (fn.In a typed field's InQuery), a
// First whose result is used as an Option orm.Optional of it, and
// Post.Objects.Manager() Post.Objects.
func fixQueries(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	for _, p := range m.pkgs {
		paths, srcs := vukaSources(p)
		models := packageModels(p)
		for i, path := range paths {
			out, n := queryFixes(srcs[i], models)
			if n == 0 {
				continue
			}
			changes = append(changes, fixChange{fmt.Sprintf("%s: %d quer(ies) as the orm's", relPath(m.root, path), n), func() error {
				return os.WriteFile(path, out, 0o644)
			}})
		}
	}
	return changes, nil
}

// packageModels are the names of the orm models p declares, in its .vuka
// and .go files.
func packageModels(p *load.Package) map[string]bool {
	models := map[string]bool{}
	for _, f := range p.Files {
		src, err := os.ReadFile(filepath.Join(p.Dir, f.Name))
		if err != nil {
			src = f.Src
		}
		orm := importedAs(src, ormModule)
		for _, t := range structTypes(src) {
			if orm != "" && t.orm == orm {
				models[t.name] = true
			}
		}
	}
	return models
}

// optionMethods are the methods that read a value as an Option.
var optionMethods = map[string]bool{"Get": true, "IsSome": true, "IsNone": true, "Unwrap": true, "UnwrapOr": true}

// fnFuncs are package fn's functions the orm has under the same name,
// taking typed fields.
var fnFuncs = map[string]bool{"Lower": true, "Upper": true, "Trim": true, "Length": true, "Abs": true,
	"Year": true, "Month": true, "Day": true, "Date": true, "Coalesce": true}

// queryFixes is src with its orm.Query forms rewritten, models being the
// package's models, and how many it rewrote.
func queryFixes(src []byte, models map[string]bool) ([]byte, int) {
	if len(models) == 0 {
		return src, 0
	}
	n := 0
	for range 8 { // an edit inside another's text goes on the next round
		out, k := queryRound(src, models)
		if k == 0 {
			break
		}
		src, n = out, n+k
	}
	if n == 0 {
		return src, 0
	}
	if fn := importedAs(src, ormFnPath); fn != "" && !bytes.Contains(src, []byte(fn+".")) {
		src = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:\w+\s+)?"`+regexp.QuoteMeta(ormFnPath)+`"\n`).ReplaceAll(src, nil)
	}
	if f, err := format.Source(src); err == nil {
		src = f
	}
	return src, n
}

func queryRound(src []byte, models map[string]bool) ([]byte, int) {
	toks, _ := scanFile("", src)
	orm := importedAs(src, ormModule)
	needOrm := false
	ormName := func() string {
		needOrm = true
		if orm == "" {
			return "orm"
		}
		return orm
	}
	rt := importedAs(src, "github.com/vuka-lang/vuka")
	if rt == "" {
		rt = "vuka"
	}
	fn := importedAs(src, ormFnPath)
	text := func(a, b int) string { return string(src[toks[a].off : toks[b].off+tokLen(toks[b])]) }
	var edits []srcEdit
	for i := 0; i+3 < len(toks); i++ {
		t := toks[i]
		if i > 0 && t.tok == token.PERIOD && toks[i+1].lit == "First" && toks[i+2].tok == token.LPAREN {
			if e, ok := optionalFirst(src, toks, i-1, models, ormName); ok {
				edits = append(edits, e)
			}
			continue
		}
		if t.tok != token.IDENT || toks[i+1].tok != token.PERIOD || toks[i+2].tok != token.IDENT {
			continue
		}
		sel := toks[i+2].lit
		switch {
		case models[t.lit] && sel == "Objects" && i+6 < len(toks) && toks[i+3].tok == token.PERIOD && toks[i+4].lit == "Manager" &&
			toks[i+5].tok == token.LPAREN && toks[i+6].tok == token.RPAREN:
			edits = append(edits, srcEdit{toks[i+3].off, toks[i+6].off + 1, ""})
		case t.lit == rt && (sel == "Or" || sel == "And" || sel == "Not") && toks[i+3].tok == token.LPAREN:
			if end := closing(toks, i+3); end > 0 && refersToModel(toks[i+4:end], models) {
				edits = append(edits, srcEdit{t.off, t.off + len(t.lit), ormName()})
			}
		case fn != "" && t.lit == fn && toks[i+3].tok == token.LPAREN && fnFuncs[sel]:
			edits = append(edits, srcEdit{t.off, t.off + len(t.lit), ormName()})
		case fn != "" && t.lit == fn && toks[i+3].tok == token.LPAREN && sel == "In":
			end := closing(toks, i+3)
			args := splitArgs(toks, i+4, end)
			if end < 0 || len(args) != 3 {
				continue
			}
			a, q, c := text(args[0][0], args[0][1]-1), text(args[1][0], args[1][1]-1), text(args[2][0], args[2][1]-1)
			edits = append(edits, srcEdit{t.off, toks[end].off + 1, a + ".InQuery(" + q + ".Values[any](" + c + ".Name()))"})
		}
	}
	if len(edits) == 0 {
		return src, 0
	}
	slices.SortStableFunc(edits, func(a, b srcEdit) int { return a.start - b.start })
	var b bytes.Buffer
	last, n := 0, 0
	for _, e := range edits {
		if e.start < last {
			continue // inside an earlier edit's text: next round
		}
		b.Write(src[last:e.start])
		b.WriteString(e.text)
		last, n = e.end, n+1
	}
	b.Write(src[last:])
	out := b.Bytes()
	if needOrm && orm == "" {
		out = ensureImport(out, ormModule)
	}
	return out, n
}

// optionalFirst is the edits making x := q.First(ctx)? — toks[i] the
// token before .First — orm.Optional(q.First(ctx))? when x is read as an
// Option later in the block.
func optionalFirst(src []byte, toks []wtok, i int, models map[string]bool, ormName func() string) (srcEdit, bool) {
	end := closing(toks, i+3)
	if end < 0 {
		return srcEdit{}, false
	}
	after := end + 1
	if after < len(toks) && toks[after].tok == token.ILLEGAL && toks[after].lit == "?" {
		after++
	}
	if after < len(toks) && toks[after].tok != token.SEMICOLON && toks[after].tok != token.RBRACE {
		return srcEdit{}, false
	}
	// The statement's start: back to its := at depth 0.
	depth, assign := 0, -1
	for k := i; k >= 0; k-- {
		switch toks[k].tok {
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth++
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth--
		}
		if depth < 0 || depth == 0 && toks[k].tok == token.SEMICOLON {
			break
		}
		if depth == 0 && (toks[k].tok == token.DEFINE || toks[k].tok == token.ASSIGN) {
			assign = k
			break
		}
	}
	if assign < 1 || toks[assign-1].tok != token.IDENT {
		return srcEdit{}, false
	}
	v := toks[assign-1].lit
	if assign >= 3 && toks[assign-2].tok == token.COMMA && toks[assign-3].tok == token.IDENT {
		v = toks[assign-3].lit // x, err := q.First(ctx)
	}
	if !refersToModel(toks[assign+1:i+1], models) || !readAsOption(toks, after, v) {
		return srcEdit{}, false
	}
	start, stop := toks[assign+1].off, toks[end].off+1
	return srcEdit{start, stop, ormName() + ".Optional(" + string(src[start:stop]) + ")"}, true
}

// readAsOption reports whether v is read as an Option (v.Get(), …) in the
// block from toks[from].
func readAsOption(toks []wtok, from int, v string) bool {
	depth := 0
	for k := from; k+2 < len(toks); k++ {
		switch toks[k].tok {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			if depth--; depth < 0 {
				return false
			}
		}
		if toks[k].tok == token.IDENT && toks[k].lit == v && toks[k+1].tok == token.PERIOD && optionMethods[toks[k+2].lit] {
			return true
		}
	}
	return false
}

// refersToModel reports whether toks name a model's field or manager:
// Post.Title, Post.Objects.
func refersToModel(toks []wtok, models map[string]bool) bool {
	for k := 0; k+2 < len(toks); k++ {
		if toks[k].tok == token.IDENT && models[toks[k].lit] && toks[k+1].tok == token.PERIOD && toks[k+2].tok == token.IDENT &&
			(k == 0 || toks[k-1].tok != token.PERIOD) {
			return true
		}
	}
	return false
}

// closing is the index of the token closing the bracket at toks[open], -1
// when there is none.
func closing(toks []wtok, open int) int {
	depth := 0
	for k := open; k < len(toks); k++ {
		switch toks[k].tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if depth--; depth == 0 {
				return k
			}
		}
	}
	return -1
}

// splitArgs are the arguments between toks[from] and toks[end], as token
// ranges [start, end).
func splitArgs(toks []wtok, from, end int) [][2]int {
	if end < 0 || from >= end {
		return nil
	}
	var out [][2]int
	depth, start := 0, from
	for k := from; k < end; k++ {
		switch toks[k].tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		case token.COMMA:
			if depth == 0 {
				out = append(out, [2]int{start, k})
				start = k + 1
			}
		}
	}
	if start < end && !(toks[end-1].tok == token.COMMA) {
		out = append(out, [2]int{start, end})
	}
	return out
}

func tokLen(t wtok) int {
	if t.lit != "" && t.tok != token.SEMICOLON {
		return len(t.lit)
	}
	return len(t.tok.String())
}

// ejectFixes rewrites the earlier forms of the .vuka files eject -rm
// takes — tags as attributes, hooks found by name, orm.Query's queries,
// orm.Schema values — so the Go they become is written the way it is today.
func ejectFixes(patterns, extra []string, files map[string]bool) error {
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	dbSchema := ormHasDBSchema(root)
	for _, p := range append(slices.Clip(patterns), extra...) {
		dir, recursive := strings.CutSuffix(p, "/...")
		if dir == "" {
			dir = "."
		}
		if dir, err = filepath.Abs(dir); err != nil {
			return err
		}
		pkgs, err := load.Discover(root, modPath, dir, recursive, nil)
		if err != nil {
			return err
		}
		for _, pkg := range pkgs {
			paths, srcs := vukaSources(pkg)
			models := packageModels(pkg)
			fixed := make([][]byte, len(srcs))
			for i, path := range paths {
				src, _, err := attributeTags(srcs[i])
				if err != nil {
					return fmt.Errorf("%s: %w", relPath(root, path), err)
				}
				src, _ = queryFixes(src, models)
				if dbSchema {
					src, _ = dbSchemaRefs(src)
				}
				fixed[i] = src
			}
			live, deferred := deferredGroups(fixed)
			for i, path := range paths {
				if !files[realPathOf(path)] && !slices.ContainsFunc(patterns, func(p string) bool { return inPattern(pkg.Dir, p) }) {
					continue
				}
				src, _ := markHooks(fixed[i], live, deferred)
				if !bytes.Equal(src, srcs[i]) {
					if err := os.WriteFile(path, src, 0o644); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
