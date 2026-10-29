package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// jailUID is the user builds and programs run as, outside and in.
const jailUID = 65534

// nsjail confines builds and programs with nsjail: new pid, mount, net,
// ipc, uts and cgroup namespaces; a root of read-only binds and tmpfs; no
// capabilities; rlimits, cgroup v2 limits and a seccomp policy. The net
// namespace has only lo: a program listens on 127.0.0.1:8080 inside it, and
// the shim, the runner's own binary started beside it in the jail, bridges
// a unix socket in a bound directory to that port. Nothing else reaches it
// and it reaches nothing.
type nsjail struct {
	bin        string // nsjail
	shim       string // the runner's static binary
	vuka       string
	goroot     string
	gomodcache string
	gocache    string // shared build cache, writable by builds only
	vukaSrc    string // the vuka/v2 replace target
	cgroup     string // cgroup v2 mount

	buildTime, runTime time.Duration
	buildMem, runMem   int64
	buildCPU, runCPU   int // ms of CPU per second
	pids               int
	dataBytes          int64
}

func newNsjail(c config) (Sandbox, error) {
	j := &nsjail{
		bin: c.nsjail, shim: c.shim, vuka: c.vuka, goroot: c.goroot,
		gomodcache: c.gomodcache, gocache: c.gocache, vukaSrc: c.vukaSrc, cgroup: c.cgroup,
		buildTime: c.buildTimeout, runTime: c.maxLife,
		buildMem: c.buildMemory, runMem: c.runMemory,
		buildCPU: 2000, runCPU: 1000, pids: 64, dataBytes: 50 << 20,
	}
	for _, p := range []string{j.bin, j.shim, j.vuka, j.goroot, j.gomodcache, j.gocache} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("nsjail sandbox: %w", err)
		}
	}
	if err := delegateCgroups(j.cgroup); err != nil {
		return nil, fmt.Errorf("nsjail sandbox: cgroup v2 at %s: %w (run the container with --privileged --cgroupns=private)", j.cgroup, err)
	}
	return j, nil
}

// delegateCgroups moves the processes of the cgroup at root into a leaf so
// root may enable the controllers the jails' cgroups, its children, use.
func delegateCgroups(root string) error {
	ctl, err := os.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil {
		return err
	}
	if has := string(ctl); strings.Contains(has, "memory") && strings.Contains(has, "pids") && strings.Contains(has, "cpu") {
		return nil
	}
	leaf := filepath.Join(root, "runner")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		return err
	}
	procs, err := os.ReadFile(filepath.Join(root, "cgroup.procs"))
	if err != nil {
		return err
	}
	for _, pid := range strings.Fields(string(procs)) {
		os.WriteFile(filepath.Join(leaf, "cgroup.procs"), []byte(pid), 0)
	}
	return os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0)
}

// seccompPolicy refuses what neither go build nor a web program needs.
const seccompPolicy = `POLICY play {
	ERRNO(1) {
		ptrace, process_vm_readv, process_vm_writev, mount, umount, pivot_root,
		swapon, swapoff, reboot, kexec_load, init_module, finit_module, delete_module,
		bpf, perf_event_open, keyctl, add_key, request_key, unshare, setns, acct,
		settimeofday, clock_settime, adjtimex, syslog, iopl, ioperm, open_by_handle_at,
		userfaultfd
	}
}
USE play DEFAULT ALLOW`

func (j *nsjail) common(limit time.Duration, mem int64, cpu int, fsizeMB int) []string {
	return []string{
		"--mode", "o", "--quiet", "--hostname", "play",
		"--disable_clone_newuser", "--user", strconv.Itoa(jailUID), "--group", strconv.Itoa(jailUID),
		"--time_limit", strconv.Itoa(int(limit / time.Second)),
		"--rlimit_as", "max", "--rlimit_core", "0", "--rlimit_cpu", strconv.Itoa(int(limit/time.Second) + 5),
		"--rlimit_fsize", strconv.Itoa(fsizeMB), "--rlimit_nofile", "1024", "--rlimit_nproc", "max",
		"--use_cgroupv2", "--cgroupv2_mount", j.cgroup,
		"--cgroup_mem_max", strconv.FormatInt(mem, 10), "--cgroup_mem_swap_max", "0",
		"--cgroup_pids_max", strconv.Itoa(j.pids), "--cgroup_cpu_ms_per_sec", strconv.Itoa(cpu),
		"--seccomp_string", seccompPolicy,
		"--bindmount", "/dev/null", "--bindmount_ro", "/dev/urandom",
	}
}

func ro(src, dst string) []string { return []string{"--bindmount_ro", src + ":" + dst} }

func tmpfs(dst string, size int64) []string {
	return []string{"--mount", "none:" + dst + ":tmpfs:size=" + strconv.FormatInt(size, 10) + ",mode=1777"}
}

func env(kv ...string) []string {
	var out []string
	for _, e := range kv {
		out = append(out, "--env", e)
	}
	return out
}

func chownTree(dir string) error {
	return filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, jailUID, jailUID)
	})
}

func (j *nsjail) Build(ctx context.Context, dir string, log io.Writer) error {
	if err := chownTree(dir); err != nil {
		return err
	}
	args := j.common(j.buildTime, j.buildMem, j.buildCPU, 512)
	for _, p := range []string{"/usr", "/lib", "/lib64", "/bin"} {
		if _, err := os.Stat(p); err == nil {
			args = append(args, ro(p, p)...)
		}
	}
	args = append(args, ro(j.goroot, j.goroot)...)
	args = append(args, ro(j.vuka, j.vuka)...)
	args = append(args, ro(j.gomodcache, j.gomodcache)...)
	if j.vukaSrc != "" {
		args = append(args, ro(j.vukaSrc, j.vukaSrc)...)
	}
	args = append(args, "--bindmount", j.gocache+":"+j.gocache, "--bindmount", dir+":/work", "--cwd", "/work")
	args = append(args, tmpfs("/tmp", 512<<20)...)
	args = append(args, env(buildEnv(
		"PATH="+filepath.Join(j.goroot, "bin")+":/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp", "TMPDIR=/tmp", "GOTMPDIR=/tmp", "XDG_CACHE_HOME=/tmp/.cache",
		"GOCACHE="+j.gocache, "GOMODCACHE="+j.gomodcache,
	)...)...)
	args = append(args, "--", j.vuka)
	args = append(args, buildArgs(true)...)
	c := exec.CommandContext(ctx, j.bin, args...)
	c.Stdout, c.Stderr = log, log
	c.Cancel = func() error { return c.Process.Signal(syscall.SIGTERM) }
	c.WaitDelay = 3 * time.Second
	return c.Run()
}

func (j *nsjail) Start(ctx context.Context, bin, data string, extra []string, out io.Writer) (*Process, error) {
	sock := filepath.Join(data, "sock")
	if err := os.MkdirAll(sock, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chown(sock, jailUID, jailUID); err != nil {
		return nil, err
	}
	args := j.common(j.runTime, j.runMem, j.runCPU, 64)
	args = append(args, ro(bin, "/app")...)
	args = append(args, ro(j.shim, "/shim")...)
	args = append(args, "--bindmount", sock+":/sock", "--cwd", "/data")
	args = append(args, tmpfs("/data", j.dataBytes)...)
	args = append(args, tmpfs("/tmp", 16<<20)...)
	args = append(args, env(append([]string{"HOME=/data", "TMPDIR=/tmp"}, append(extra, runEnv("/data")...)...)...)...)
	args = append(args, "--", "/shim", "shim", "/sock/http.sock", "8080", "/app")
	c := exec.Command(j.bin, args...)
	c.Stdout, c.Stderr = out, out
	if err := c.Start(); err != nil {
		return nil, err
	}
	p := watch(c, func() { c.Process.Kill() })
	p.Network, p.Addr = "unix", filepath.Join(sock, "http.sock")
	return p, nil
}
