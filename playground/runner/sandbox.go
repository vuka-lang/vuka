package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// A Sandbox builds a project and runs what it builds, each confined.
type Sandbox interface {
	// Build runs vuka build in dir, the project, writing dir/app; the
	// command's output goes to log.
	Build(ctx context.Context, dir string, log io.Writer) error
	// Start runs bin, its output going to out. data is the program's own
	// directory: its working directory, where its SQLite file is.
	Start(ctx context.Context, bin, data string, env []string, out io.Writer) (*Process, error)
}

// A Process is a started program.
type Process struct {
	Network, Addr string        // where it serves HTTP
	Done          chan struct{} // closed when it has exited
	Stop          func()        // ends it and waits for that
	Exit          func() string // after Done: how it ended
}

// buildEnv is the environment of every build: no network, the image's
// module cache only.
func buildEnv(extra ...string) []string {
	return append([]string{
		"GOPROXY=off",
		"GOFLAGS=-mod=mod",
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
		"CGO_ENABLED=0",
		"VUKA_CACHE=off",
	}, extra...)
}

// buildArgs is vuka build's command line.
func buildArgs(trimpath bool) []string {
	args := []string{"build", "-diag=json"}
	if trimpath {
		args = append(args, "-trimpath")
	}
	return append(args, "-o", "app", ".")
}

// runEnv is what a program is started with besides the sandbox's own.
func runEnv(data string) []string {
	return []string{"DATABASE_URL=sqlite://" + filepath.Join(data, "app.db")}
}

// local runs builds and programs as plain subprocesses: for developing the
// page, never for untrusted code.
type local struct {
	vuka string
}

func newLocal(vuka string) (*local, error) {
	if os.Getenv("RUNNER_UNSAFE_LOCAL") != "1" {
		return nil, errors.New("the local sandbox runs code unconfined: set RUNNER_UNSAFE_LOCAL=1 to use it")
	}
	return &local{vuka: vuka}, nil
}

func (l *local) Build(ctx context.Context, dir string, log io.Writer) error {
	c := exec.CommandContext(ctx, l.vuka, buildArgs(false)...)
	c.Dir = dir
	c.Env = append(os.Environ(), buildEnv()...)
	if os.Getenv("VUKA_TAILWIND") == "" {
		if _, err := exec.LookPath("tailwindcss"); err != nil {
			c.Env = append(c.Env, "VUKA_TAILWIND=off")
		}
	}
	c.Stdout, c.Stderr = log, log
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	return c.Run()
}

func (l *local) Start(ctx context.Context, bin, data string, env []string, out io.Writer) (*Process, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	c := exec.Command(bin)
	c.Dir = data
	c.Env = append(append(append(os.Environ(), env...), runEnv(data)...), "PORT="+strconv.Itoa(port))
	c.Stdout, c.Stderr = out, out
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		return nil, err
	}
	p := watch(c, func() { syscall.Kill(-c.Process.Pid, syscall.SIGKILL) })
	p.Network, p.Addr = "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	return p, nil
}

// watch makes a Process of a started command; kill ends it.
func watch(c *exec.Cmd, kill func()) *Process {
	p := &Process{Done: make(chan struct{})}
	var exit string
	go func() {
		err := c.Wait()
		exit = "exited"
		if err != nil {
			exit = err.Error()
		}
		close(p.Done)
	}()
	var once sync.Once
	p.Stop = func() {
		once.Do(func() {
			select {
			case <-p.Done:
				return
			default:
			}
			c.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.Done:
			case <-time.After(2 * time.Second):
				kill()
				<-p.Done
			}
		})
	}
	p.Exit = func() string { <-p.Done; return exit }
	return p
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// ready waits until the process serves at its address, or exits.
func ready(ctx context.Context, p *Process) error {
	for {
		c, err := net.DialTimeout(p.Network, p.Addr, time.Second)
		if err == nil {
			c.Close()
			return nil
		}
		select {
		case <-p.Done:
			return fmt.Errorf("the program %s before it served HTTP", p.Exit())
		case <-ctx.Done():
			return fmt.Errorf("the program didn't serve HTTP: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
