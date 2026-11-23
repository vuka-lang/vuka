//go:build unix

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// manyPackages is a module of n packages with Vuka source, whose first
// build (cold: nothing cached) takes seconds; its program prints "up".
func manyPackages(t *testing.T, n int) string {
	t.Helper()
	files := map[string]string{}
	imports := ""
	for i := range n {
		files[fmt.Sprintf("p%d/p.vuka", i)] = fmt.Sprintf(`package p%[1]d

import "fmt"

type T struct {
	Name string

	static count int
}

func T.New(n string) *T { T.count++; return &T{Name: n} }

func F() Result[string] { return Ok(fmt.Sprint(T.New("x").Name, %[1]d)) }
`, i)
		imports += fmt.Sprintf("\t_ \"m/p%d\"\n", i)
	}
	files["main.go"] = "package main\n\nimport (\n\t\"fmt\"\n\t\"time\"\n\n" + imports + ")\n\nfunc main() { fmt.Println(\"up\"); time.Sleep(time.Hour) }\n"
	return module(t, files)
}

// TestRunWatchSignalWhileTranspiling: a signal while vuka run -watch's
// first build transpiles ends vuka promptly, before the program starts —
// it used to wait for the build and start the program first — and a
// second signal ends it at once.
func TestRunWatchSignalWhileTranspiling(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := manyPackages(t, 200)
	vukavet, err := checkoutVukavet()
	if err != nil {
		t.Fatal(err)
	}
	for _, signals := range []int{1, 2} {
		t.Run(fmt.Sprint(signals), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var out syncBuffer
			c := exec.CommandContext(ctx, mustExecutable(t), "run", "-watch", ".")
			c.Dir = dir
			c.Env = append(os.Environ(), childEnv+"="+vukavet, "VUKA_CACHE="+t.TempDir(), traceEnv+"=1")
			c.Stdout, c.Stderr = &out, &out
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			// Signalled as the transpile starts.
			for !strings.Contains(out.String(), "prepare routes") {
				if ctx.Err() != nil {
					t.Fatalf("no transpile:\n%s", out.String())
				}
				time.Sleep(5 * time.Millisecond)
			}
			sent := time.Now()
			for range signals {
				c.Process.Signal(syscall.SIGINT)
				time.Sleep(50 * time.Millisecond)
			}
			err := c.Wait()
			took := time.Since(sent)
			if ctx.Err() != nil {
				t.Fatalf("vuka run -watch didn't stop:\n%s", out.String())
			}
			if strings.Contains(out.String(), "up\n") || regexp.MustCompile(`(?m)^trace \S+ start$`).MatchString(out.String()) {
				t.Errorf("vuka run -watch started the program after the signal (exited %v after %v):\n%s", err, took, out.String())
			}
			if took > 15*time.Second {
				t.Errorf("vuka run -watch stopped %v after the signal", took)
			}
			if exitCode(err) != 130 {
				t.Errorf("vuka run -watch: %v, want exit status 130\n%s", err, out.String())
			}
		})
	}
}
