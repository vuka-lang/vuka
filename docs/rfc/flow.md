# RFC: flow, approval workflows over your records

Status: approved design (2026-11-20). No code accompanies this document; each
phase of §13 lands on its own.

## 1. Goal

An app has records that move through approvals: an order that is reviewed,
charged and shipped; a candidate who passes screening, interviews and an offer;
a leave request signed by three people in parallel. flow lets the app declare
that process once, next to the record, and gets:

- **human tasks** with a permission, an optional assignee, a form, a due time
  and a history of who did what and how long it waited;
- **guards** that disable an action and show the reason;
- **undo** of a finished step while nothing after it has started;
- **inboxes and dashboard tabs**: my tasks, tasks I may take, tasks I did, as
  one indexed query each, ready to render;
- **code steps** that run in the same transaction as the move that reached
  them;
- **parallel branches**, **timers** and **subprocesses**, run by the engine;
- the record's status column kept correct, and every transition atomic across
  tables and across flows;
- mistakes in the graph reported when the app boots, naming the method.

The module is `github.com/vuka-lang/flow/v2`, released with the set (vuka, ui,
web, orm, flow under one version).

Out of scope: a visual editor, BPMN import and export, and running a process
across several apps.

## 2. What it builds on

| Piece | Used for |
|---|---|
| [Attributes](/features/attributes) and the attribute registry (`vuka.TypeAttrs`, `vuka.MethodAttrs`, `vuka.FuncOf`) | Declaring nodes and edges on methods; reading them back at boot |
| orm | The process and task tables, their migrations, conditions, transactions (`orm.Atomic`, `orm.OnCommit`) |
| web (`auth`, `cron`, live broadcasts) | Permissions, the timer scheduler, pushing task changes to open pages |
| `.vuka.html` templates | A task's UI, written by the project with its own components; the flow module itself never imports the ui kit |

An admin app built a prototype of this engine inside its own code before this
RFC. What it taught is in the design: a process's state must be guarded against
two users acting at once (§8.4); "what lands in my list" and "what I may do"
are different rules (§9); a status column the rest of the app reads must stay
correct (§8.3); and every flow declared the same plumbing (loading, ids,
saving, searching), which belongs in the engine.

## 3. Declaring a flow

A flow is one struct per process, over one record type R. Its nodes are its
methods, marked by attributes. Edges are fields of those attributes, so they
sit on the node they leave. There is no wiring method and no code generation.

```vuka
package orders

@flow.Meta{Name: "order", Title: "Orders", Version: 1}
type OrderProcess struct {
	flow.Workflow[Order]

	Billing *BillingService // plain pointer fields are injected services
	Mail    *MailService
}

@flow.Start{To: (*OrderProcess).Review}
func (p *OrderProcess) Submitted() {}

@flow.Start{To: (*OrderProcess).Review} // a second entry, started from code
func (p *OrderProcess) Imported() {}

@flow.View{Can: "orders.review", Sets: StatusReview, NoUndo: true}
@flow.On{Name: "approve", To: (*OrderProcess).Charge, Can: "orders.approve",
	Title: "Approve order", Done: "Order approved", Guard: inStock}
@flow.On{Name: "reject", To: (*OrderProcess).Rejected, Can: "orders.reject",
	Title: "Reject order", Done: "Order rejected", Undo: flow.CanUndo}
func (p *OrderProcess) Review(ctx context.Context, in ReviewInput) error {
	return p.Billing.SaveReview(ctx, p.Record(), in)
}

@flow.Action{To: (*OrderProcess).Done}
func (p *OrderProcess) Charge(ctx context.Context) error {
	return p.Billing.Charge(ctx, p.Record().ID)
}

@flow.End{Sets: StatusDone}
func (p *OrderProcess) Done() {}

@flow.End{Sets: StatusRejected}
func (p *OrderProcess) Rejected() {}

var F = flow.Register(&OrderProcess{})
```

- **A node's name is its method name.** Tasks store `order.Review`; renaming a
  method is a data migration (§8.5).
- **Edges are method expressions** (`(*OrderProcess).Review`), so the compiler
  checks them, the editor jumps to them and renames them. A cycle is an edge
  like any other.
- **A node with several outcomes repeats `@flow.On`**, one attribute per
  outcome, each carrying its own target, permission, title and guard. The
  outcomes read top to bottom beside the code that handles them.
- **A View with one outcome** uses the sugar fields on `@flow.View` itself
  (`To`, `Label`, `Title`, `Confirm`, `Done`, `Guard`, `Undo`).
- **Functions in attributes** (`inStock`) are ordinary Go functions; their
  signatures are checked at boot (§11).
- A flow over a record with no table of its own uses a plain struct for R: the
  record is kept as JSON on the process.

## 4. Attributes

All attributes go on methods, except `@flow.Meta`, which goes on the type.

| Attribute | Fields | Meaning |
|---|---|---|
| `@flow.Meta` (type) | Name, Title, Version | the flow's identity; Version is recorded on each process |
| `@flow.Start` | To, Can | an entry node; startable from the UI when `Can` is set, always from code |
| `@flow.View` | Can, Sets, Assign, Due, Page, Template, NoUndo; single-outcome sugar: To, Label, Title, Confirm, Done, Guard, Undo | a human task |
| `@flow.On` (repeated, on a View) | Name, To, Can, Label, Title, Confirm, Done, Guard, Page, Template, Undo | one outcome of the View |
| `@flow.Action` | To, Sets | a code node; its body runs in the transition's transaction, and an error rolls the move back |
| `@flow.Decide` | To []any (the possible targets) | a code branch; the body returns `flow.Next` through `p.Go(target)`, which must be one of `To` |
| `@flow.Split` | To []any | parallel fan-out, one token per target |
| `@flow.Join` | To | continues once every token that left the split has arrived |
| `@flow.Timer` | After, To | continues after a delay |
| `@flow.Sub` | Flow, To | starts a child flow; continues when the child ends |
| `@flow.End` | Sets | finishes the process; any task still open is cancelled |

Field types:

- **Undo** is `flow.CanUndo | flow.NoUndo | zero`; zero means the node's
  default.
- **Guard** is `func(ctx, *flow.Activation[R]) error`. Returning
  `flow.Refuse(...)` disables the button and shows the reason.
- **Page** is an existing app URL such as `"/orders/view"` (§11).
- **Template** is a `.vuka.html` file in the flow's package, such as
  `"review.vuka.html"` (§11).
- **Assign** is `func(ctx, *flow.Activation[R]) (int64, error)` and routes the
  task to a user; an unassigned task stays claimable by anyone holding the
  node's permission.
- **Sets** is the declarative status write (§8.3).

## 5. Method shapes

| Node | Signature |
|---|---|
| View | `func (p *T) M(ctx context.Context[, in FormStruct]) error` |
| Action | `func (p *T) M(ctx context.Context) error` |
| Decide | `func (p *T) M(ctx context.Context) (flow.Next, error)` |
| Start, End, Split, Join, Timer, Sub | empty body |

- **The View's trailing struct is the task's form.** It is bound and validated
  with web's binder, from the struct's own field attributes. There is
  no `Form:` field on the View.
- **An outcome can have its own form.** `@flow.On{…, Form: RejectForm{}}` (the
  zero value of a struct; `Form` is also a field of `@flow.View` for its single
  outcome) makes completing with that outcome bind and validate that struct
  instead. A View whose outcomes all have a `Form` omits the struct parameter;
  with a parameter, an outcome's `Form` must be the same type. Register refuses
  a `Form` that is not a struct, naming the method and the outcome.
- **A View's body runs on completion**, after `Can` and `Guard` and before the
  token moves. `p.Outcome()` says which `@flow.On` fired. Returning an error,
  or `flow.Refuse`, vetoes the move.
- **An Action's body runs in the same transaction** as the move that reached
  it.
- **Later nodes read the submitted form** with `p.Form() any` (a pointer to the
  outcome's struct) and `flow.FormAs[F](p) (F, bool)`. In the View's body and in
  an Action in the same transition it is the bound form in memory; in a node
  reached in a later transition it is decoded from the stored task data into
  the outcome's struct. It is the form of the last completed View, nil when that
  had none.

## 6. flow.Workflow[R]: the embedded base

- `flow.Register(&T{})` keeps the value as a **template**. Injected fields are
  filled once from the app container.
- Attributes are read through vuka's registry, which records method and type
  attributes at program start. A package-level `var F = flow.Register(...)` is
  therefore safe: no init-order problem and no separate registration file.
- On every activation the template is **cloned**, and the clone's
  `Workflow[R]` holds the live activation. A clone belongs to one transition;
  state written on it does not leak into another process.
- Methods on the clone:

| Method | Returns |
|---|---|
| `p.Record() *R` | the record the process is about |
| `p.Outcome() string` | the outcome that fired, in a View body or `Moved` |
| `p.Say(msg)` | a message shown to the acting user after the move |
| `p.Go(method) flow.Next` | the chosen target, from a Decide |
| `p.Actor() int64` | the acting user |
| `p.ProcessID()` | the process id |

## 7. Convention methods

Looked up by name at `Register`; each is optional.

```vuka
func (p *T) Status(r *R) int64                      // record → status value: adoption, display, Sets consistency
func (p *T) Scope(ctx) (flow.Cond, error)           // row security, enforced on every list, task and transition
func (p *T) Moved(ctx, from, to flow.NodeRef) error // after every move, same transaction; an error rolls back
```

- `Scope` is security: where it does not match, a record offers nothing and a
  step is refused.
- `Moved` is for audit, notification and anything else that is not a status
  write. `to.Is((*T).Done)` compares by method.
- `Sets:` stays the declarative status write.

## 8. Runtime

### 8.1 Tables

The module ships its tables as orm migrations (`flow/migrations`), applied with
the app's own.

- `flow_process`: id, flow, flow version, status (`new`, `done`, `cancelled`),
  record id (text, so integer, UUID and composite keys fit), data (JSON),
  parent task, version (§8.4), created, finished, started by.
- `flow_task`: id, process, flow, node, status (`new`, `assigned`, `started`,
  `scheduled`, `done`, `cancelled`, `revived`, `error`), owner, outcome, token
  (§8.2), data (JSON), error text, created, assigned, started, finished,
  scheduled (due or timer time).
- `flow_task_previous`: task, previous task, the graph of what led to what.

Indexes serve the lists: `(status, owner)`, `(flow, node, status)`,
`(process)`, `(flow, record)`, `(status, scheduled)`.

### 8.2 Activation and tokens

Every node runs through one lifecycle: a task is created, optionally assigned,
started, completed, and its successors are activated, all in the transaction of
the step that reached it. Action, Decide, Split and Start complete at once and
the engine continues; a View waits for a person, a Timer for the scheduler
(`web/cron`), a Join for its tokens, a Sub for its child's end.

`Split` gives each branch a token derived from its own; `Join` continues when
every token that left the split has arrived, or has been cancelled.

An Action that fails puts its task in `error` with the error text, and the
transaction that reached it rolls back to before the step; `Revive` retries
it. A panic is recovered the same way.

### 8.3 The status mirror

Apps keep reading a record's status column. `Sets:` on a node writes it in the
same transaction as the step, so the column is always the state the process is
in; the `Status` convention method (§7) tells the engine how to read it back.
Nothing else should write it.

### 8.4 Two users at once

Each step updates `flow_process` with `version = version + 1 WHERE version = ?`
before anything else, and checks the row was matched, not changed, which some
drivers report instead (`flow.ErrConflict`: "This record was changed by someone
else. Reload it and try again."). A task's own status change is guarded the
same way, so two people completing one task, or a timer firing as a person
acts, cannot both win.

### 8.5 Undo, revive, cancel, change

- **Undo** a done task: allowed when the node's `Undo` is not `NoUndo` and no
  task it led to has started; cancels the successors and reopens a new task at
  that node. Undo of a `Sub` undoes its children first, or refuses with the
  reason.
- **Revive** a cancelled or failed task: a new task at the same node, with the
  same token and data.
- **Cancel** a process: every open task, if each can be cancelled.
- **Changing a flow**: tasks store method names only, so adding nodes and edges
  needs no migration, and Version records which declaration a process started
  under. A removed node keeps its old tasks readable in history; a renamed one
  needs `flow.Rename(migration, "order", "Review", "Approval")` in a migration.

## 9. Transactions

Every transition is one database transaction, and the `ctx` handed to user code
carries it. The order is:

`Can` + `Guard` → View or Action body → `Sets` → `Moved` → token moves.

- An error at any step rolls everything back.
- Writes to other tables made with that `ctx` join the transaction, so a body
  can update the order, insert an invoice and move the process atomically.
- `OtherFlow.Start` or `Advance(ctx, …)` called from a body joins it too, so a
  cross-flow move is atomic: either both processes moved or neither did.
- External side effects (HTTP calls, SMS, mail) cannot roll back. Put them last
  in the body, or in their own Action node, and make them safe to repeat.
  `orm.OnCommit` runs work only after the transaction commits.

## 10. The handle

`flow.Register` returns the handle `F`:

```vuka
F.Start(ctx, rec)                       // the first Start node
F.StartAt(ctx, rec, (*T).Imported)      // a named Start node
F.Advance(ctx, rec, "approve")          // move a record from code
F.Complete(ctx, task, "approve", form)  // complete a task
F.Claim(ctx, task) / Assign(ctx, task, user) / Unassign(ctx, task)
F.Process(ctx, recID) / Active(ctx, recID) / History(ctx, recID)
F.Undo(ctx, task) / Cancel(ctx, process)
F.Inbox(ctx) / Queue(ctx) / Archive(ctx)
```

`flow.Inbox(ctx)` and its siblings list across flows. The lists:

- **Inbox**: tasks assigned to me, `assigned` or `started`.
- **Queue**: unassigned `new` tasks of nodes whose permission I hold.
- **Archive**: tasks I completed.

Each is one query on `flow_task`, joined to the record table only when a
filter, search or sort needs it. Errors are `flow.Refuse` and
`flow.ErrForbidden`, and `flow.ErrConflict` from §8.4.

The engine asks the app for four things through one value registered once
(`flow.Use(hooks)`), each with a default:

```vuka
type Hooks struct {
	User  func(ctx context.Context) (id string, ok bool)                     // default: web/auth's identity
	Can   func(ctx context.Context, perm string) bool                        // default: web/auth.Can
	Audit func(ctx context.Context, e flow.Event) error                      // default: none
	Names func(ctx context.Context, ids []string) (map[string]string, error) // display names for owners
}
```

## 11. Task UI and lists: the project's own

The flow module ships no task UI and never imports the ui kit. A View, or one
`@flow.On`, says where its task UI lives in one of two ways:

- **`Page: "/orders/view"`** reuses a page the app already has. The link to the
  task carries it (`?task=<id>`). On that page, `flow.TaskFrom(ctx)` returns
  the open task, or nil when the page was not opened from a workflow link, so
  the page can show the outcome buttons and hide what the task does not allow.
- **`Template: "review.vuka.html"`** is a `.vuka.html` file in the flow's
  package, rendered with a typed context `*flow.Task[R]`: the record, the form
  (the View's struct parameter, and each outcome's own form), the allowed outcomes with their labels,
  confirmations and guard refusals, the actor, the process id, and helpers to
  complete an outcome. The template uses any of the project's own components.
  The file is checked at compile time where vuka allows a file-typed attribute
  field, and at `Register` otherwise (§12).

With neither set, the task completes from code (`F.Complete`) or through a
minimal plain form, with no ui kit.

Inboxes, dashboard tabs and record lists are built by the project on the
handle's list and task APIs (§10): `F.Inbox`, `F.Queue` and `F.Archive` return
paged, searchable, sortable rows, so a project's own `DataTable` or page shows
them with its own columns and filters. A ready-made dashboard (`flowui`) may
ship as a separate, optional module; core never depends on it.

Row security is `Scope` (§7), enforced inside these APIs; a project's filters
only narrow what it shows.

## 12. Register-time validation

`flow.Register` fails the boot, naming the method and suggesting the likely
intended name, on any of:

- no Start node (at least one is required);
- a `To` that is not a node method of this type;
- an unreachable node;
- duplicate `On` names on one node;
- a View with no outcome;
- an End with outgoing edges;
- a Guard, Assign, Decide or form whose signature does not fit R;
- a `p.Go` target that is not in the Decide's `To`;
- a Join not reachable from its Split;
- a `Sub.Flow` that is not registered;
- a `Template` file that does not exist or does not parse against `*flow.Task[R]`.

`flowtest` runs the same checks in a test, so a broken graph fails `go test`
before it fails a boot.

## 13. Packaging and phases

- `github.com/vuka-lang/flow/v2`: the engine; depends on orm and web, never on ui.
- `github.com/vuka-lang/flow/v2/migrations`: the tables.
- `github.com/vuka-lang/flow/v2/flowtest`: test helpers.

Phases:

1. **Engine and tables**: activation, tasks, the mirror, conflicts,
   undo/revive/cancel, timers, split/join, subprocesses, the lists, the hooks.
   Acceptance: every node's semantics tested in a transaction against SQLite,
   Postgres and MySQL; the lists one query each.
2. **Declaration**: attribute reading, the clone model, convention methods,
   the handle, the checks of §12. Acceptance: each rule of §12 has a failing
   and a passing case.
3. **Task UI**: `Page` links and `flow.TaskFrom`, `Template` rendering with
   `*flow.Task[R]`, live updates of open task pages. Acceptance: browser tests
   for starting, claiming, completing, undoing through both. An optional
   dashboard module follows separately.
4. **A real app**: an approval flow, then three more, moved onto it with
   behaviour unchanged; what they need beyond this RFC is added or recorded.
5. **Docs**: a guide page per part, the attribute reference.

## 14. Open questions

- Whether tasks' `data` and the process's `data` stay JSON, or flows may
  declare typed task data.
- How long done processes and tasks are kept, and whether an app may archive
  them to another table.
- Reminders for overdue tasks: a hook only, or mail and SMS through web's
  senders.

## 15. Migrating from the previous draft

The engine, stores, tokens, version checks, migrations, undo and `flowtest`
stay. `flowtest` re-targets the new declaration. Node identity is
the method name, so stored node names keep working where a method keeps the
name its node had. What goes:

- the public `StartN`/`ViewN`/… node field types, and `Next(&f.X)`;
- the `Button`/`Confirm`/`On(...)` outcome chains, now `@flow.On`;
- `Col`/`ColOf`, `Filter`/`ListFilter`/`Status`/`Newest` and every other
  presentation setter on the flow: lists are the project's, on the handle's
  APIs (§11), and `Status`/`Scope` move to the type (§7);
- `Render` and any task UI inside the library;
- `Graph()`.

## 16. Compatibility

flow is new. Under the [compatibility promise](/project/compatibility) from
its first release: its attributes, the handle's methods, the hooks, the table
layout (changed only through shipped migrations), and the validation errors.

## Earlier drafts

Two earlier shapes were dropped. Spec structs and, after them, node fields with
a `Graph()` method put the outcomes far from the node that handles them, so a
reader jumped between two places to follow one step. Type-level `@flow.Route`
headers had the same distance and gave nothing to hang per-outcome
configuration on (permission, title, guard, undo). A nested `@flow.Switch{Cases}`
literal was hard to read and hard to check, and a transition hook that replaced
`Sets` made the status column depend on code instead of a declaration; both
gave way to `@flow.Decide` with `p.Go`, and `Sets` plus an optional `Moved`.
