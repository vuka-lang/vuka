# RFC: 30,000 live connections on one process

Status: draft. No code changes accompany this document; each phase lands on
its own, judged by the load harness of §2.

## 1. Goal and budget

One process serves 30,000 or more concurrent live pages. Out of scope: the
application's own work (handlers, queries) and scaling past one process.
Several replicas already work: `web.LiveKey` shares the join keys and
`web.WithRelay` carries broadcasts between replicas (see
[Running Several Replicas](/web/deployment#running-several-replicas)).

Where the runtime stands today:

- `web/livews.go` keeps every connection in one `hub`. A single mutex `mu`
  guards `conns`, `topics` (topic to the set of connections), `perIP`,
  `perToken` and the `open` count. `admit` takes it for every join,
  `setTopics` for every change of subscription, and `deliver` for every
  broadcast.
- A joined page costs two goroutines: the loop in `serveLive` that runs the
  session, and the reader that feeds it. Its writes happen on the loop's
  goroutine (`conn.write`, bounded by `writeTimeout`).
- Broadcasts reach a connection through `conn.post`, which appends to an
  unbounded `inbox`. `conn.drain` takes the whole batch but handles each
  message on its own: `Info`, a render, a write. N broadcasts waiting for a
  page are N renders and N frames.
- `ui/live/session.go` holds a `Session` per page: an `instance` tree
  (`byID`), and per instance a `shadow`, what the browser holds, kept to
  diff the next render against. `Session.Trim` (`treesession.go`) releases
  the shadows of an idle page; `LiveTrimAfter` turns it on and it is off by
  default, because it saves about a tenth of a page's memory.
- Limits exist: `LiveMaxConns` (default 10,000), `LiveMaxConnsPerIP`,
  `LiveMaxConnsPerPage`, `LiveJoinTimeout`, `LiveIdleTimeout`,
  `LiveCheckEvery`, `LiveTrimAfter`. Parked sessions (`livepark.go`) carry
  a page across a reconnect for `LiveAdopt`'s window.

The budget for 30,000 pages:

| Cost | Estimate | Verdict |
|---|---|---|
| Goroutine stacks, 2 per page | ~0.5 GB | fine for the Go runtime |
| Session memory | tens of KB per page; 30,000 × ~60 KB ≈ 2 GB before GC headroom | the main ceiling; the harness pins the real number |
| Broadcast to one hot topic | 30,000 renders and 30,000 writes at once | a ceiling: a spike of CPU and heap, and tail latency for everyone |
| The hub lock | one mutex across join, leave, subscribe and every broadcast | a ceiling: it serializes at this size |

## 2. Phase 0: a load harness as the acceptance gate

A protocol-speaking Go client lives in the repository (a command beside the
conformance tests, not part of the shipped modules). It speaks the live
protocol ([Live Protocol](/reference/live-protocol)): it fetches a page over
HTTP, takes the join token from the render, opens the socket, joins with
protocol v2, and then sends events at a configurable rate per page.

It ramps connections to 30,000 and beyond, and records:

- the server's RSS, goroutine count and GC pauses (read from the process, or
  from `runtime/metrics` through a debug endpoint the harness app mounts);
- event round-trip time, p50 and p99;
- broadcast-to-last-subscriber latency: the time from `Broadcast` to the
  last of N subscribers receiving its frame;
- connections dropped, by close status.

Every later phase is judged by it. A change that does not move a number the
harness measures does not land, and a number that matters but the harness
cannot see gets a probe added first.

## 3. Phase 1: fan-out and locking

(a) **Shard the hub.** `conns`, `topics` and `perIP` split by hash into a
fixed number of shards (a power of two, 64 by default), each with its own
lock. A topic's subscribers live in the shard of the topic's hash, a
connection's counters in the shard of its address. `admit` still enforces
`maxConns` with an atomic counter. A broadcast locks one shard, not the hub.

(b) **Coalesce broadcasts per connection.** A broadcast marks the
subscribed instances dirty and schedules one render for the connection;
broadcasts that arrive before it runs merge into it. Each message still
reaches its `Update` method in arrival order, so state is exact, but the
page renders once and writes one patch for the batch. Today `drain` renders
and writes per message.

(c) **A bounded, latest-wins outbound queue per connection.** A connection
gets a queue of about 32 frames, drained by one writer goroutine with a
write deadline, in place of writes inline on the session's goroutine and an
unbounded `inbox`. When the queue overflows, the intermediate patches are
dropped and one catch-up diff against the browser's last acknowledged state
takes their place; the instance `shadow` makes that cheap, since it is
exactly what the browser holds. A client that still cannot drain before the
deadline is closed with the existing reload status (`closeReload`, 4001), so
its tab loads the page again. A slow client never back-pressures the rest:
its queue is its own, and a broadcast never waits on a write.

(d) **Bound concurrent broadcast renders.** A process-wide semaphore of
about 2 × `NumCPU` permits limits the renders a broadcast runs at once. The
rest wait their turn, so a storm on one topic costs latency to its own
subscribers and does not exhaust memory or starve the interactive events of
other pages.

(e) **Shared render for identical fragments.** For a topic whose output is
the same for every subscriber, the server renders once and fans one frame
out. It is opt-in, behind an explicit marker on the topic (a
`web.SharedTopic(topic)` option, or the equivalent for components that
subscribe to it), because it asserts that the fragment reads nothing of the
viewer: no session, no identity, no per-page state. Everything else keeps
the per-page render of (b).

## 4. Phase 2: memory per session

(a) **Intern statics process-wide.** A tree's statics are already keyed by
fingerprint on the wire (the browser keeps each once per connection). On the
server, each session holds its own copies. Hold them once per process,
reference counted, and let sessions keep pointers. Pages of the same route
share nearly all of their statics.

(b) **A deeper hibernation tier past `Trim`.** After a long idle, serialize
the page's instance state and drop its session tree, keeping the token and
the state blob. The next event rehydrates the session, the way a parked
session (`livepark.go`) does for a reconnect, applied to an idle page that
stays connected. A deep-idle page falls from tens of KB to about 1 to 2 KB.
`LiveTrimAfter` stays as the shallow tier; the new option, `LiveHibernateAfter`,
sets the deep one.

(c) **Pool buffers; document `GOMEMLIMIT`.** Render and encode buffers
(`conn.write` builds each frame in a fresh `bytes.Buffer`) come from a pool.
The deployment guide gains advice on setting `GOMEMLIMIT` below the
container's limit, so the collector works harder before the kernel does.

## 5. Phase 3: transport and OS

- **Compression.** The default is already `NoContextTakeover`, which holds
  no compressor per open socket. At this scale `LiveCompression(Off)` is the
  recommended setting for pages whose frames are small (messages under 256
  bytes are never compressed); `ContextTakeover` costs about 800 KB per
  socket, 24 GB at 30,000, and is not an option here. The docs say so.
- **Kernel buffers.** Set `SO_SNDBUF` and `SO_RCVBUF` on live sockets to
  16 to 32 KB. Frames are small, and the system defaults can cost gigabytes
  of kernel memory at 30,000 sockets.
- **A "running at scale" guide.** A page for operators: `ulimit -n` above
  the connection count, `net.core.somaxconn` and the listen backlog,
  `GOMAXPROCS` against the container's CPU quota, and `GOMEMLIMIT`.
- **Deferred: a single-poller read path.** Replacing the reader goroutines
  with an epoll loop is a large change with its own risks. It is done only if
  the harness shows stacks dominating the budget, and not otherwise.

## 6. Phase 4: admission and degradation

`admit` already refuses a join over `LiveMaxConns` with 503 and a
`Retry-After` header, and one over the per-address or per-page cap with 429.
This phase keeps those and adds two things:

- The default `LiveMaxConns` is raised from 10,000, and the refusal's
  `Retry-After` is spread (jittered) so refused pages do not return
  together.
- Under memory pressure (the heap near `GOMEMLIMIT`), the process
  force-hibernates the idlest sessions, by last activity, before it refuses a
  join. Refusing is the last step, not the first.

## 7. Acceptance criteria

On the harness, on the reference machine recorded with the results:

1. 30,000 held connections, each with its page rendered and joined, settle
   at a steady RSS within the budget of §1, with GC pauses that do not grow
   with the connection count.
2. A broadcast to all 30,000 reaches the last subscriber with p99 under
   about 250 ms.
3. While one topic storms, interactive event round-trip p99 on other pages
   stays in single-digit milliseconds.
4. No interactive connection is dropped. Only a client that cannot drain is
   closed, and the harness's deliberately slow clients are the only ones.

## 8. Compatibility

The wire does not change, but for one additive marker: the shared-topic
declaration of §3(e), which a client that predates it never sees. Coalescing
(§3b) and the overflow catch-up (§3c) send the same patch messages as today,
fewer and merged. A slow-client close reuses the reload status (4001), so
the tab recovers the way it does after a deploy; see
[Live Protocol](/reference/live-protocol) and
[Rolling Deploys](/web/deployment#rolling-deploys). Applications need no
change to run at a lower scale: every new option defaults to behavior the
current limits already allow, except where this document says a default
rises.
