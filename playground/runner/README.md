# Playground runner

The service behind the playground's **Run**: it builds a project with
`vuka build`, starts the program, and serves it from an origin of its own,
each step confined by nsjail.

## API

| | |
|---|---|
| `POST /run` | `{"files": {"path": "source", …}}` → `{session, url, diagnostics, buildLog, output, error}`. With `Accept: text/event-stream` it streams `log` events (the build's lines), then `result`, then the program's `output` lines and an `exit` event. Diagnostics have the shape the WASM compiler's have. |
| `DELETE /s/<session>` | stops a program (the page does on a run again and on leaving). |
| `GET /healthz` | `{sessions, cap}`. |
| `<session>.<run host>/…` or `<run origin>/s/<session>/…` | the program: HTTP and websockets, proxied to it. |

A project is at most 50 files and 256 KB of `.vuka`, `.vuka.html`, `.go` and
`.css`; it is module `example.com/play`, built with the go.mod and go.sum of
`docs/testdata/gomode` (the versions the docs pin), and may import std,
`ui`, `web`, `orm` and `vuka` only — any other import is a diagnostic before
anything builds. A program sees `PORT`, `DATABASE_URL=sqlite:///data/app.db`
(a fresh file each run) and `HOME=/data`.

A session ends after `RUNNER_IDLE` without a request (an open websocket
counts as one) or `RUNNER_MAX_LIFE` in all, or when the program exits.

## Sandbox

Two sandboxes implement `Sandbox` (`Build`, `Start`):

- **nsjail** (Linux, the default there). Builds and programs run as uid
  65534 with no capabilities in new pid, mount, network, ipc, uts and
  cgroup namespaces, under a seccomp policy refusing ptrace, mount, bpf,
  perf, keyring, module and namespace calls.
  - A build sees read-only `/usr`, the Go root, the module cache, the vuka
    source its go.mod replaces, the shared build cache (writable: only the
    go command writes it, and a build runs no user code) and the project at
    `/work`; `/tmp` is a 512 MB tmpfs. 1 GB of memory, 2 CPUs, 64
    processes, 60 s, `GOPROXY=off GOFLAGS=-mod=mod CGO_ENABLED=0`,
    `-trimpath`.
  - A program sees only its binary (`/app`, read-only), `/data` (a 50 MB
    tmpfs), a 16 MB `/tmp`, `/proc` of its own pid namespace, `/dev/null`,
    `/dev/urandom` and `/sock`. 256 MB of memory and no swap, 1 CPU, 64
    processes, 64 MB files, killed at `RUNNER_MAX_LIFE`.
  - **Network**: the jail's network namespace has only `lo`, so nothing
    leaves it. The runner's own static binary runs in the jail as `shim`:
    it starts the program with `PORT=8080` and, once that port answers,
    serves `/sock/http.sock`, a unix socket in a directory bound from the
    host, bridging each connection to `127.0.0.1:8080`. The proxy dials
    that socket. No veth, no bridge, no routes.
  - Limits are cgroup v2 controllers on a cgroup per jail; at start the
    runner moves its container's processes into a leaf cgroup and enables
    `cpu memory pids` for the jails.
- **local** (development only): plain subprocesses, refused unless
  `RUNNER_UNSAFE_LOCAL=1`. It runs anything a project does, as you.

gVisor (`runsc`) would fit the same interface; it isn't implemented.

## Container

```sh
docker build -f playground/runner/Dockerfile -t vuka-runner .   # from the repository's root
docker run --privileged --cgroupns=private -p 8090:8090 vuka-runner
```

nsjail needs, inside the container, the power to make namespaces, mount,
and write the cgroup v2 hierarchy; `--privileged --cgroupns=private` is
what CI runs and what is tested. `--privileged` gives the runner (root in
the container) every capability, no seccomp or AppArmor profile, and a
writable `/sys/fs/cgroup`; `--cgroupns=private` makes that hierarchy the
container's own. Narrower settings (`--cap-add SYS_ADMIN`, unconfined
seccomp and AppArmor, a writable cgroup mount) may suffice but are
untested. The host needs cgroup v2 with the `cpu`, `memory` and `pids`
controllers delegated to containers.

The image holds Go (the docs' pin), nsjail, Tailwind's CLI, the module
cache of what `docs/testdata/gomode` requires, and a build cache warmed by
building every example as a sandboxed build does, so a run compiles only
the project.

## Configuration

| Variable | Default | |
|---|---|---|
| `RUNNER_ADDR` | `:8090` | listen address |
| `RUNNER_ALLOW_ORIGIN` | `http://localhost:5173` | the docs' origins, comma-separated: CORS on the API, `frame-ancestors` on programs |
| `RUNNER_RUN_ORIGIN` | `http://*.run.localhost:<port>` | where programs are served: `https://*.run.example.com` serves each at its own host (deploy this, with a wildcard DNS record and certificate); `https://run.example.com` serves them at `/s/<session>/` |
| `RUNNER_API_HOST` | any | the API's host; others get 404 |
| `RUNNER_SESSIONS` | `20` | programs running and builds at once; more get 503 "busy" |
| `RUNNER_RATE` | `10` | runs a minute per client address (429 beyond) |
| `RUNNER_TRUST_PROXY` | off | `1`: the client address is `X-Forwarded-For`'s first |
| `RUNNER_IDLE` / `RUNNER_MAX_LIFE` | `2m` / `10m` | session limits |
| `RUNNER_BUILD_TIMEOUT` | `60s` | |
| `RUNNER_RUN_MEMORY` / `RUNNER_BUILD_MEMORY` | 256 MiB / 1 GiB | bytes |
| `RUNNER_SANDBOX` | `nsjail` on Linux | or `local` |
| `RUNNER_DIR` | `/var/lib/runner` in the image | session directories |
| `RUNNER_GOMOD`, `RUNNER_VUKA_SRC`, `RUNNER_VUKA`, `RUNNER_SHIM`, `RUNNER_NSJAIL`, `RUNNER_GOROOT`, `RUNNER_GOMODCACHE`, `RUNNER_GOCACHE`, `RUNNER_CGROUP` | set by the image | |

Programs must be served from an origin that is neither the docs' nor the
API's: the iframe's `sandbox="allow-scripts allow-forms allow-same-origin"`
keeps a program's scripts in that origin. The runner strips a program's
`X-Frame-Options` and replaces its `frame-ancestors` with the docs' origins.
Per-session hosts (`*.run…`) keep one program's cookies and storage from
another's; with `/s/<session>/` paths they share one origin, and a page's
absolute links reach its session through the Referer or a cookie, which
third-party-cookie blocking can defeat — use hosts in production.

Sizing: a run holds up to 256 MB and a CPU while building at 1 GB and 2
CPUs, so `RUNNER_SESSIONS=20` wants about 8 GB of memory and 4–8 CPUs; the
image is about 2.5 GB (2.48 GB in CI).

The docs enable Run when built with `VITE_VUKA_RUNNER=https://<api host>`.

## Developing

```sh
cd playground/runner
RUNNER_SANDBOX=local RUNNER_UNSAFE_LOCAL=1 RUNNER_ADDR=127.0.0.1:8090 go run .   # vuka on PATH
cd ../../docs && VITE_VUKA_RUNNER=http://localhost:8090 npx vitepress dev
node playground/run-check.mjs http://localhost:5173/playground                  # Chrome check
go test ./...                                                                    # API, with a fake sandbox
RUNNER_LOCAL_TEST=1 go test -run Local ./...                                     # a real build, local sandbox
```

The nsjail tests run in the image (CI: `.github/workflows/runner.yml`):

```sh
docker run --rm --privileged --cgroupns=private -v "$PWD/playground/runner:/src" -w /src \
  -e RUNNER_NSJAIL_TEST=1 vuka-runner go test -run Nsjail -v ./...
```
