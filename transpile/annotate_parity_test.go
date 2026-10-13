package transpile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// queryShapedSrc mirrors vuka-orm's Django-style manager: a Query[T] whose only
// Annotate takes ...Annotation[T] (NOT the string form QuerySet/Manager have),
// called here with the string form — exactly what the portal does on a model
// embedding orm.Base (whose static Objects is a Query[T]).
const queryShapedSrc = `package p

type Expr interface{ expr() }

type Annotation[T any] interface{ annotation(x *T) (string, Expr) }

type QuerySet[T any] struct{}

func (qs QuerySet[T]) Annotate(name string, e Expr) QuerySet[T] { return qs }

type Query[T any] struct {
	qs  QuerySet[T]
	set bool
}

func (q Query[T]) Annotate(as ...Annotation[T]) Query[T] { return q }

func (q Query[T]) QuerySet() QuerySet[T] { return q.qs }

type exprImpl struct{}

func (exprImpl) expr() {}

func F(name string) Expr { return exprImpl{} }

type Task struct{ ID int }

func run() {
	var q Query[Task]
	_ = q.Annotate("employer", F("x")).Annotate("category", F("y"))
}
`

var lineDirectiveRe = regexp.MustCompile(`/\*line [^*]*\*/|//line [^\n]*\n`)

// normalizeGen strips the generated header and the //line / /*line*/ directives
// that the build (non-bare) path adds, leaving only the type-checkable Go.
func normalizeGen(s string) string {
	if i := strings.Index(s, "package "); i >= 0 {
		s = s[i:]
	}
	return lineDirectiveRe.ReplaceAllString(s, "")
}

// TestBuildLSPParityAnnotate guards against a vuka build vs vuka lsp divergence:
// the build path (Bare:false, //line directives) and the lsp path (Bare:true,
// settled buffer) must transpile a chain to the SAME type-checkable Go, so gopls
// (via vuka lsp) sees exactly what the Go compiler (via vuka build) sees. The
// transpiler applies no ergonomic rewrite to string-form Annotate on a Query[T],
// so both emit it verbatim and both are rejected identically by go/types.
func TestBuildLSPParityAnnotate(t *testing.T) {
	file := transpile.File{Name: "p.vuka", Src: []byte(queryShapedSrc)}
	build, err := transpile.Package([]transpile.File{file}, transpile.Options{Importer: load.NewImporter(".", "")})
	if err != nil {
		t.Fatalf("build-path transpile: %v", err)
	}
	lsp, err := transpile.Package([]transpile.File{file}, transpile.Options{Importer: load.NewImporter(".", ""), Bare: true})
	if err != nil {
		t.Fatalf("lsp-path transpile: %v", err)
	}
	bg, lg := normalizeGen(string(build.Files[0].Src)), normalizeGen(string(lsp.Files[0].Src))
	if bg != lg {
		t.Fatalf("build and lsp paths diverge after stripping line directives:\n--- build ---\n%s\n--- lsp ---\n%s", bg, lg)
	}
	// The generated Go is the source verbatim (no Annotate rewrite): confirm the
	// call site is carried through unchanged.
	if !strings.Contains(bg, `q.Annotate("employer", F("x")).Annotate("category", F("y"))`) {
		t.Fatalf("expected the Annotate call carried through verbatim; got:\n%s", bg)
	}
	// And that verbatim Go is a genuine go/types error — the same one the editor
	// reports — so vuka lsp is faithful to vuka build, not inventing a diagnostic.
	if msg := goBuildError(t, string(build.Files[0].Src)); !strings.Contains(msg, "does not implement") || !strings.Contains(msg, "Annotation") {
		t.Fatalf("expected go build to reject the generated Go with an Annotation error (parity with the editor); got:\n%s", msg)
	}
}

// goBuildError compiles gen as a standalone main package and returns the
// compiler's output (empty when it builds).
func goBuildError(t *testing.T, gen string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module paritytest\n\ngo 1.27.1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(gen), 0o644)
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, _ := cmd.CombinedOutput()
	return string(out)
}
