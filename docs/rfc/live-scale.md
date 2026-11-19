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

(e) **Shared render for identical fragments.** For a topic whose output is
the same for every subscriber, the server renders once and fans one frame
out. It is opt-in and explicit on both sides, because it asserts that the
fragment reads nothing of the viewer and answers no event. Everything else
keeps the per-page render of (b). The design below is checked against
`ui/live` (`Session`, `instance`, `shadow`, `conn`'s statics ids, the v2
tree encoding) and `web/livews.go`, `liveout.go`.

*API.* `web.SharedTopic(names...)` is an `App` option naming the fragments the
app allows. A component renders as one by declaring `func (*Feed)
SharedTopic() string` (the fragment's name); a tag of it anywhere in a page
becomes a placeholder for the fragment. A name the app did not list is a render
error, as is a method on a component of a topic the option did not name. The
name is the fragment's identity, not a broadcast topic: the fragment's
instances `Subscribe` to whatever topics they like and the hub delivers those
to the fragment, not to the pages.

*Ownership.* A `live.SharedSet` per app holds one `live.Shared` per name for
the life of the app. A `Shared` owns a `live.Session` of its own (ids prefixed
`s<n>.`, so they never meet a page's `c<n>`), mounted from the first tag's
props with a context that holds the app but no request. A later tag must carry
equal props (`sameProps`: value fields only; pointers, slices and children
never match), else the render fails: props are the one way a page could put
something of itself into the fragment, and equal props cannot. The page's
instance tree gets a `TreeRef` to the fragment's root instance, exactly as
it does for a child component, and records the `*Shared` on the instance that
rendered the tag (kept across skipped renders). The page's diff treats the
reference as it treats any reference: equal or not, never inside it. A page
that first renders over HTTP (protocol v1 HTML, no socket yet) gets the
fragment's current HTML in place of the reference.

*One render, one diff, one encode.* Broadcasts to the fragment's topics go to
the `Shared`, not to its subscribers: `web` posts them to an inbox that a pump
goroutine drains in batches (the coalescing of (b)), takes one permit of the
render gate (d), and calls `Shared.Publish`: `InfoBatch` on the fragment's
session, which renders each changed instance once and diffs it against the
topic's single `shadow`, then encodes the entries once into the finished
message `{"type":"patch","trees":[…]}`. The entries are addressed by the
fragment's own instance ids, which every page that shows it holds the same
(`s1.c3`), so the bytes are the same for all of them. Fan-out is one append
of that same slice into each subscriber's outbound queue (c). A queue that is
full closes that browser with `closeReload` from a goroutine of its own; the
publisher never waits on a write.

*Statics.* A page's statics ids are numbered per connection (`conn.ids`), the
blocker for sharing bytes. A fragment's frames use a process-wide table: a
statics key is interned to `1<<30 + n`, an id no connection numbers, and the
encoder writes the id and never the statics inline. Each connection tracks
the shared ids it holds (a bitset); a frame lists the ids it uses. A
connection that holds all of them gets the frame. One that lacks any gets the
topic's snapshot instead, which is encoded with its statics inline on first
use per frame key, cached per version, and marks every id of the tree as
held. Steady state needs no statics (rows repeat a few keys), so a fan-out is
a bitset check per subscriber. Long-markup references (`{"r":…}`) are per
connection too, so a shared encoding never uses them: it quotes. Token
patches and keyed list plans depend on the shadow only, so they are shared.
The table is append-only and bounded by the fragments' template variety.

*Versions.* The fragment has a version, incremented by each publish that changed
something. A frame carries `Base` and `Ver`. A connection holds, per
fragment, the version it has: a frame whose `Ver` it has already is skipped;
one whose `Base` is its version (and whose statics it holds) is sent; any
other case, a connection that joined between two frames or missed one, gets the
snapshot of the current version and is then at it. So correctness never rests
on ordering of publishes and joins, only on the rule "a frame applies to
exactly its base". To join, the session asks its host to attach (the
`SharedLinks` the connection implements): under the fragment's lock the
connection is entered in its subscribers and handed the snapshot's entries
(`full` trees, statics inline), which the session appends to the very
message that carries the page's reference (a client that prunes the trees its
`c0` does not reach would otherwise drop them), the join render or the
patch of an event or navigation. Frames that arrive before that message is
queued are not sent; once it is, the connection compares versions and sends a
snapshot if it missed any. When a page stops showing the fragment the session
detaches before its patch is queued, so no frame can reach a browser after the
trees it still holds are gone. Coalescing (b) is untouched for the page's own
parts; the fragment coalesces on its own inbox. The FIFO queue (c) keeps a
connection's frames in the order versions advanced, since each is queued
under the connection's lock when its version is updated.

*The guarantee.* A fragment renders with a context holding the app's
injector and nothing of a viewer: no request, no session, no identity, no URL, no
page variables, no deferred loads. A read of one (`web.Request`,
`web.ResponseWriter`, `RequestContext`, `CSRFContext`, `auth.Current`, …)
panics in development with the fragment's name and what it read, and answers
"none" outside it, which cannot leak what is not there. Nothing reaches the
frame from the page but the props the tag set, and those must match across
every tag. A fragment may not use streams, Temporary regions, forms, uploads,
`PushJS`, `Reload`, handlers, or nest another fragment: each is a render
error. A vet diagnostic for a shared component reading per-viewer values
statically is follow-up work.

*Events.* A shared instance is one per process, so state an event changed
would change for every viewer and leak one viewer's action to all. A shared
fragment is display-only for the broadcast's data. Rendering an element with
an event handler, a `ui.JS.Push`, a form, or any other registration of a handler
inside a fragment is a render error in development ("a shared fragment answers
no events") and no handler is registered in production. Over the wire, an event
whose target names a shared instance (the `s<n>.` prefix) is refused before
the page's session looks for a handler, with an error reply, whatever the
build; the shared instance is never reached by a browser message.
Per-viewer interaction belongs in one of two places:

```html
<!-- browser-side state: sort, expand, filter on the client -->
<script lang="go">
type Board struct{ ui.Live }
func (*Board) SharedTopic() string { return "board" }
</script>
<section>
  <ui.State name="open" initial="false"/>
  <button onClick={ui.JS.Toggle("#detail")}>Details</button>
  <div id="detail" hidden>…</div>
</section>
```

```html
<!-- a per-page component wraps the shared one and passes props -->
<script lang="go">
type Page struct {
	ui.Live
	Sort string // per page: this page's own event changes it
}
func (p *Page) SortByName() { p.Sort = "name" }
</script>
<button onClick={SortByName}>Name</button>
<Board sort={Sort}/>
```

The second pattern changes the tag's props, so the props rule of the first
paragraph applies: a fragment whose props differ between pages is not shared.
A fragment that must differ per viewer is a per-page component; use sharing for
the part that is the same. A change meant for everyone is a broadcast (an admin
action calls the app's `Broadcast`), delivered to the fragment's `Update`,
never an event on the shared instance.

*Client.* No change to `live.js`. A fragment is a set of instances the client
holds as it holds any, by id, referenced from the page's tree, applied by the
same `trees.apply`; `1<<30+n` statics ids are ordinary map keys. The only wire
addition is that those ids and the `s<n>.` instance ids exist, documented in
`docs/reference/live-protocol.md`; a client that predates this never sees
them, since a page without a shared tag sends none. Protocol v1 clients get the
fragment as of the render and no updates.

*Mixed pages.* A page with a shared fragment and per-page parts: the page's own
instances are diffed per page as before and their messages are per page; the
fragment's frames arrive as their own messages. The page's render never
descends into the fragment (it holds a reference), and the fragment never
reads the page.

*Cost.* Per subscriber per broadcast: a lock, a bitset check and a queue
append, in place of render, diff and encode. The render and encode happen once
per fragment per batch.

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
