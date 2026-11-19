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
| Session memory | measured ~122 KB per page of 21 instances (§2.1); 30,000 ≈ 3.7 GB | a ceiling; it grows with a page's instance count |
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

### 2.1 Baseline

The harness lives in the web repository, `examples/loadtest`: a scenario with
no database (`/counter`, `/feed`, 20 rows of live instances each, so 21
instances a page) besides the existing Postgres one. Before any phase, on an
Apple M1 Pro, 10 cores, 16 GB, Go 1.27, server `GOMAXPROCS` 4, client and
server on the same machine:

| Measure | 1,000 pages | 10,000 pages | §7 target at 30,000 |
|---|---|---|---|
| Server RSS per page (settled, slope of the ramp) | ~139 KB | ~122 KB (heap and stack ~112 KB) | within §1 |
| Goroutines per page | 2 | 2 | — |
| Broadcast to the last subscriber, p99 | 67 ms | 682 ms (p50 610, max 760) | under ~250 ms |
| Broadcast to the first subscriber, p50 | 0.6 ms | 0.8 ms | — |
| Interactive round trip, quiet, p99 | 5 ms | 9.8 ms | single-digit ms |
| Interactive round trip during a storm, p99 | 270 ms | 4.9 s (p50 746 ms) | single-digit ms |
| GC pause, max | ~0.2 ms | 0.5 ms; 5 ms during the storm | not growing with pages |
| Interactive and normal pages dropped | 0 | 0 | 0 |

What the baseline shows:

- **Holding pages is not the problem.** Memory per page is flat from 1,000
  to 10,000, GC pauses don't grow with the count, and the client's own lag
  stays near 2 ms p99, so the client is not the bottleneck. At 30,000 the
  same page would hold about 3.7 GB.
- **A broadcast is serial**: about 70 µs a subscriber, so the last of 30,000
  would wait about 2 s.
- **The storm (2 broadcasts a second offered to 8,800 subscribers) proves the
  lock and the inbox.** `Broadcast` itself blocked, so only 1.4 a second went
  out. Every subscriber got one frame per broadcast: nothing merges. Summed
  mutex wait in the process went from 0.4 s to about 4,100 s over the 40 s
  storm, scheduler latency p99 to 940 ms, and RSS from 1.2 GB to 2.7 GB as
  inboxes grew. Interactive pages on other topics waited behind it.
- **Slow clients** (200, reading with a delay) were all closed, but by the
  transport — connection reset or EOF — with no close frame, so the reload
  status §3(c) relies on never reached them.

Probes the harness still needs: a per-connection queue depth or pending-bytes
gauge (the inbox is invisible from outside today), and a check that merged
broadcasts leave each page's state exact (it only identifies the last
message in a frame).

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
its tab loads the page again. The baseline (§2.1) shows that today such a
client is dropped by the transport with no close frame at all: the close
has to be written before the socket is given up, under its own short
deadline. A slow client never back-pressures the rest:
its queue is its own, and a broadcast never waits on a write.

(d) **Bound concurrent broadcast renders.** A process-wide semaphore of
about 2 × `NumCPU` permits limits the renders a broadcast runs at once. The
rest wait their turn, so a storm on one topic costs latency to its own
subscribers and does not exhaust memory or starve the interactive events of
other pages.

(e) **Shared render for identical fragments.** *Experimental: kept pending a
comparison with automatic shared HTML (identical rendered subtrees stored once
across sessions, render memoization for pure components); `ui.SharedLive` and
`ui.Broadcast` may be removed. The plumbing below does not depend on that API:
the process-wide statics ids and the versioned frame fanned out as one encoded
slice to many outbound queues serve either.* For a component whose output
is the same for every viewer, the server renders once and fans one frame out.
The component says so on its type, and everything else keeps the per-page
render of (b). The design below is checked against `ui/live` (`Session`,
`instance`, `shadow`, `conn`'s statics ids, the v2 tree encoding) and
`web/livews.go`, `liveout.go`.

*API.* A component embeds `ui.SharedLive` in place of `ui.Live`; that is the
whole declaration, in a Go struct and in the `<script lang="go">` of a
`.vuka.html` file alike (the compiler treats the embed as it treats
`ui.Live`). Its changes are sent with a typed broadcast to its type:

```go
type PriceBoard struct {
	ui.SharedLive
	prices []Price
}

func (b *PriceBoard) Update(ctx context.Context, p Price) { b.prices = upsert(b.prices, p) }

// anywhere in the app, from a request or a live event:
ui.Broadcast[*PriceBoard](ctx, Price{"TZS", 2510})
```

```html
<PriceBoard />
```

The name is `SharedLive`, not `Shared`, because `ui.Shared` is already the
state attribute that syncs a `ui.State` across tabs (`@ui.Shared`). There are
no topic strings: a shared component is addressed by its type, and
`ui.Broadcast[T]` is constrained to `T` embedding `ui.SharedLive`, so any
other type is a compile error. `Subscribe` on a shared component fails the
mount. `Update` follows the rules of `ui.Live`'s (by message type, optional
`context.Context`, optional error). The broadcast mounts the instance first
when no page has, once per process, and then applies the message, so its state
is never behind because no page had appeared. `ui.RegisterShared(&T{})`, from
an `init`, makes a replica know the type by name before any page or local
broadcast does (the compiler is to emit it for every struct embedding
`ui.SharedLive`; until then it is one line in the package).

*Ownership.* A `live.SharedSet` per app holds one `live.Shared` per component
type for the life of the app. A `Shared` owns a `live.Session` of its own (ids
prefixed `s<n>.`, so they never meet a page's `c<n>`), mounted with a context
that holds the app's injector and nothing of a viewer. A page's tag of the
component, which takes no props, is a placeholder: the page's instance tree
gets a `TreeRef` to the fragment's root instance exactly as it does for a child
component, and the instance that rendered the tag records the `*Shared` (kept
across skipped renders). The page's diff treats the reference as it treats any
reference: equal or not, never inside it. A page that first renders over HTTP
(v1 HTML, no socket yet) gets the fragment's current markup in place of the
reference.

*One render, one diff, one encode.* `Broadcast` delivers to the app's hub
under the topic `"\x00shared:" + ui.SharedName(T)` (the package path and
name), which no page subscribes to; the hub hands it to the fragment's inbox,
which a pump goroutine drains in batches (the coalescing of (b)), takes one
permit of the render gate (d) and calls `Shared.Publish`: `InfoBatch` on the
fragment's session, which delivers to the root component only, renders each
changed instance once, diffs it against the fragment's single `shadow`, and
encodes the entries once into the finished message
`{"type":"patch","trees":[…]}`. The entries are addressed by the fragment's own
instance ids, which every page showing it holds the same (`s1.c3`), so the
bytes are the same for all of them. Fan-out is the append of that one slice
into each subscriber's outbound queue (c). A full queue closes that browser
with `closeReload` from a goroutine of its own; the publisher never waits on a
write. Across replicas the broadcast goes through the app's `Relay` as the
string broadcasts do, under that same topic, so the stable name is what
travels; each replica renders its own fragment once.

*Statics.* A page's statics ids are numbered per connection (`conn.ids`),
the blocker for sharing bytes. A fragment's frames use a process-wide table: a
statics key is interned to `1<<30 + n`, an id no connection numbers, and the
encoder writes the id and never the statics inline. Each connection tracks the
shared ids it holds (a bitset); a frame lists the ids it uses. A connection
that holds all of them gets the frame. One that lacks any gets the fragment's
snapshot instead: every instance's tree with its statics inline on first use
in the message, cached per version, marking every id of the tree as held. Most
frames reference none (a changed dynamic of a known frame uses no statics), so
a fan-out is a bitset check per subscriber. Long-markup references (`{"r":…}`)
are per connection too, so a shared encoding never uses them and quotes. Token
patches and keyed list plans depend on the shadow only, so they are shared. The
table is append-only and bounded by the fragments' template variety.

*Versions.* The fragment has a version, incremented by each publish that
changed something. A frame carries `Base` and `Ver`. A connection holds, per
fragment, the version it has: a frame whose `Ver` it already has is skipped;
one whose `Base` is its version (and whose statics it holds) is sent; any other
case, a connection that joined between two frames or missed one, gets the
snapshot of the current version and is then at it. Correctness rests on one
rule, "a frame applies to exactly its base", never on the order of publishes
and joins. To attach, the session asks its connection (`SharedLinks`): under
the fragment's lock the connection is entered among the subscribers and handed
the snapshot's entries, which the session appends to the very message that
carries the page's reference (a client prunes the trees its `c0` does not
reach, so they cannot come before it), the join render or the patch of an
event or navigation. Frames that arrive before that message is queued are not
sent; once it is, the connection compares versions and sends a snapshot if it
missed any. When a page stops showing the fragment the session detaches before
its patch is queued, so no frame can reach a browser after the trees it holds
are gone. Coalescing (b) is untouched for the page's own parts; the fragment
coalesces on its own inbox. The FIFO queue (c) keeps a connection's frames in
the order versions advanced, since each is queued under the connection's lock
as its version is updated.

*The guarantee.* The promise is on the type, so what breaks it fails there. A
fragment renders with a context that holds nothing of a viewer: no request,
session, identity, URL, page variables or deferred loads. A read of one
(`web.Request`, `web.ResponseWriter`, `RequestContext`, `CSRFContext`,
`auth.Current`, …, each calling `ui.ViewerRead`) fails the render in
development with the component's name and what it read, and answers "none"
outside it, which cannot leak what is not there. Its tag takes no props: a prop
is the way a page would put something of itself into what everyone sees, so
`<PriceBoard user={u} />` fails the render, in development and in production.
Per-viewer variation goes in a wrapper (below). A shared component may not use
streams, Temporary regions, forms, uploads, `PushJS`, `Reload`, `Subscribe`,
handlers, or nest another shared component: each is a render error. Doing the
props and viewer-read checks at compile time and a vet diagnostic for a shared
component reading per-viewer values are follow-up work in the compiler.

*Events.* A shared instance is one per process, so state an event changed
would change for every viewer and leak one viewer's action to all. A shared
component is display-only for what its broadcasts carry. An element with an
event handler, a `ui.JS.Push`, or a form inside one is a render error. Over the
wire, an event whose target names a shared instance (the `s<n>.` prefix) is
refused before the page's session looks for a handler, with an error reply,
whatever the build; no browser message reaches the shared instance. A change
meant for everyone is a broadcast (an admin action calls `ui.Broadcast`),
delivered to `Update`, never an event on the shared instance. Per-viewer
interaction belongs in one of two places:

```html
<!-- browser-side state: expand, tab, copy: the page's own, in the browser -->
<script lang="go">
type PriceBoard struct{ ui.SharedLive; prices []Price }
</script>
<section>
  <button onClick={ui.JS.Toggle("#detail")}>Details</button>
  <div id="detail" hidden>…</div>
</section>
```

```html
<!-- a per-page component wraps the shared one -->
<script lang="go">
type Page struct {
	ui.Live
	sort string // per page: this page's own event changes it
}
func (p *Page) SortByName() { p.sort = "name" }
</script>
<button onClick={SortByName}>Name</button>
<p>sorted by {sort}</p>
<PriceBoard />
```

The wrapper cannot pass its state down to the shared component, because the
tag takes no props; what a viewer chooses (a sort, a filter) is applied by the
wrapper around the shared markup, in the browser, or the part that depends on
it is a per-page component.

*Client.* No change to `live.js`. A fragment is a set of instances the client
holds as it holds any, by id, referenced from the page's tree, applied by the
same `trees.apply`; `1<<30 + n` statics ids are ordinary map keys. The wire
additions are the `s<n>.` instance ids and those statics ids, documented in
`docs/reference/live-protocol.md`; a client never sees them from a page that
shows no shared component. Protocol v1 clients get the fragment as of the
render and no updates.

*Mixed pages.* A page with a shared fragment and per-page parts: the page's own
instances are diffed per page as before and their messages are per page; the
fragment's frames arrive as their own messages. The page's render never
descends into the fragment, and the fragment never reads the page.

*Cost.* Per subscriber per broadcast: a lock, a bitset check and a queue
append, in place of render, diff and encode. The render and encode happen once
per fragment per batch.

## 4. Phase 2: memory per session

### 4.1 Where a page's memory goes

Measured with the harness (web `examples/loadtest`, branch `dedup-probe`):
a heap profile at steady state with 10,000 joined pages of the admin mix
(`admin-popular`: list pages with a per-user header and role-filtered
sidebar, most users on popular lists; detail pages; a profile form), and a
census of every session's render tree and shadow.

| Per page | KB |
|---|---|
| goroutine stacks (2 per page) | 26 |
| render tree | 22.9 |
| instance state (instance structs, handler maps, component structs) | 22.7 |
| the join's HTTP request, contexts and headers, kept for the session's life | ~14 |
| shadow | 13.6 |
| bufio buffers (HTTP and WebSocket) | 8.2 |
| WebSocket connection, session | ~4 |
| **total (ramp slope)** | **~134** |

Across scenarios a page costs 95 to 137 KB; the render tree and shadow are
15 to 25% of it. How much of the tree and shadow is identical across
sessions, stored once:

| Scenario | identical | per page now → shared | 30,000 pages |
|---|---|---|---|
| everyone on the same page (`mem`) | 100% | 108.8 → 92.5 KB | 3.34 → 2.84 GB |
| popular lists (`admin-popular`) | 95% | 133.7 → 102.5 KB | 4.11 → 3.15 GB |
| mostly unique detail pages (`admin-unique`) | 73% | 95.4 → 84.1 KB | 2.93 → 2.58 GB |

Hashing a node costs about 135 ns warm, so hashing a whole 290-node page is
about 40 µs; hashing only what changed, which the renderer already tracks,
costs less.

### 4.2 The work, in order of measured payoff

(a) **Release the join's request.** A live session keeps the HTTP request it
joined with, its contexts and headers: about 14 KB a page. Keep only what the
session reads afterwards (identity, the page URL, the headers a page asked
for) and let the rest go after the join.

What changed, for anyone writing middleware:

- A WebSocket's upgrade handler returns once the socket is accepted; the
  session runs on a goroutine of its own. Middleware wrapping the live route
  sees the request complete at the upgrade, not when the socket closes (an
  in-flight request count no longer counts live sockets; `App.LiveConns`
  does). The event-stream transport still holds its handler open.
- The session's context no longer derives from the request's, so a
  `BaseContext` cancelling does not end a socket by itself. Sockets end when
  the browser closes, on `App.Stop` or the server's `Shutdown`, or on `CloseLive`. Values
  that middleware outside the app put on the upgrade request's context are
  still visible to the session; an object there that holds the request or
  response keeps it alive.
- The session keeps a trimmed copy of the upgrade request: headers, address,
  peer, mount; no body, forms or trailers. `web.Request(ctx)` in an event is
  the join's run of the route: method GET, the page's URL, the upgrade's
  headers plus `X-Vuka-Live`, `Body` `http.NoBody`.
- `web.ResponseWriter(ctx)` in an event is a writer that discards what is
  written to it, as the join's recorder did.

(b) **Shared HTML storage.** Every rendered subtree and its shadow is stored
by content hash in a process-wide, reference-counted store; sessions hold
pointers. Rendering and diffing stay per page, so nothing one viewer sees
reaches another unless it was already identical: it needs no marking and has
no rule to break. It saves 12 to 25% of a page (table above). Trees become
immutable once rendered; a render that changes a subtree builds a new node.
Statics interning, proposed here before, is the special case of a subtree
with no dynamic values. The shadow's boxed strings (24 bytes where an 8-byte
hash would do) shrink along the way.

(c) **Fewer, smaller stacks.** Two goroutines a page hold 26 KB. The reader
spends its life blocked on the socket; a shared reader (a poller) or a
reader that parks between frames, and letting an idle page's stacks shrink,
target about half of it. This is §5's deferred poller, now justified by the
numbers.

(d) **Pool buffers; document `GOMEMLIMIT`.** The HTTP and WebSocket bufio
buffers (8 KB a page) and the render and encode buffers (`conn.write` builds
each frame in a fresh `bytes.Buffer`) come from pools, held only while a
frame is read or written. The deployment guide gains advice on setting
`GOMEMLIMIT` below the container's limit, so the collector works harder
before the kernel does.

(e) **A deeper hibernation tier past `Trim`.** After a long idle, serialize
the page's instance state and drop its session tree, keeping the token and
the state blob. The next event rehydrates the session, the way a parked
session (`livepark.go`) does for a reconnect, applied to an idle page that
stays connected. A deep-idle page falls to about 1 to 2 KB. `LiveTrimAfter`
stays as the shallow tier; the new option, `LiveHibernateAfter`, sets the
deep one.

Together (a) to (d) target roughly half of a page's memory, against the 12 to
25% shared storage gives alone.

### 4.3 Render reuse is a CPU question, not a memory one

Reusing a render across sessions (keyed by component type and field values)
saves no memory beyond (b), which already stores its output once. Its case is
render CPU, the cost that bounds broadcasts (§2.1), and it is weighed against
shared components (§3(e)). Two findings bound it: rendered output carries
each instance's id (handler references, child references), so a key of type
and fields alone matched about 1,300 groups whose output differed; reuse
needs the ids filled in per page rather than baked into the output. And its
hit rate falls from 86% on popular lists to 54% on unique pages.

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

1. 30,000 held connections, each with the harness's 21-instance page
   rendered and joined, settle at a steady RSS within the budget of §1, with
   GC pauses that do not grow with the connection count. Memory per page is
   reported with the page's instance count, since it scales with it.
2. A broadcast to all 30,000 reaches the last subscriber with p99 under
   about 250 ms.
3. While one topic storms, interactive event round-trip p99 on other pages
   stays in single-digit milliseconds.
4. No interactive connection is dropped. Only a client that cannot drain is
   closed, and the harness's deliberately slow clients are the only ones.

## 8. Compatibility

The wire does not change, but for the shared fragments of §3(e): instances
with `s<n>.` ids and statics ids from `1<<30`, which a client that predates
it never sees, a page having none unless the app opts in. Coalescing
(§3b) and the overflow catch-up (§3c) send the same patch messages as today,
fewer and merged. A slow-client close reuses the reload status (4001), so
the tab recovers the way it does after a deploy; see
[Live Protocol](/reference/live-protocol) and
[Rolling Deploys](/web/deployment#rolling-deploys). Applications need no
change to run at a lower scale: every new option defaults to behavior the
current limits already allow, except where this document says a default
rises.
