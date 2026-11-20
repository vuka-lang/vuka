# RFC: flow, processes as declared graphs

Status: draft. No code accompanies this document; each phase of §11 lands on
its own.

## 1. Goal

An app declares a business process — a permit that is requested, reviewed,
approved or rejected; a leave request; a document signed by three people in
parallel — once, as a graph of typed nodes, and gets:

- the state of every running process, stored and queryable;
- each human step as a task: who may do it, who it is assigned to, its form,
  its history and how long it waited;
- automatic steps (decisions, code, timers, parallel branches, child processes)
  run by the engine;
- the lists every user needs — my tasks, tasks I may take, tasks I did — as
  one indexed query each, ready to render;
- mistakes in the graph reported by the compiler, the editor and `vuka vet`,
  not at run time.

The module is `github.com/vuka-lang/flow/v2`, released with the set
(vuka, ui, web, orm, flow under one version).

Out of scope: a visual editor, BPMN import and export, and running a process
across several apps.

## 2. What it builds on

| Piece | Used for |
|---|---|
| Attributes and field references ([fields](/features/fields)) | Declaring nodes on struct fields; naming the record's fields for forms, columns and filters |
| Code generation, as the orm generates model code | Typed node methods and node names, with no reflection at run time |
| Diagnostics, `vuka vet`, `vuka lsp` | Graph rules as errors with codes; definition, hover and completion on nodes |
| orm | The process and task tables, their migrations, conditions, transactions (`orm.Atomic`, `orm.OnCommit`) |
| web (`auth`, `cron`, live broadcasts) | Permissions, the timer scheduler, pushing task changes to open pages |
| ui (forms from field attributes, `DataTable`, live components) | Task forms, the task lists, the action menu |

An admin app built a prototype of this engine inside its own code before this
RFC. What it taught is in the design: a process's state must be guarded
against two users acting at once (§6.4); "what lands in my list" and "what I
may do" are different rules (§7.3); a status column the rest of the app reads
must stay correct (§6.3); and every flow declared the same plumbing — loading,
ids, saving, searching — which belongs in the engine.

## 3. Declaring a flow

A flow is a struct. Its embedded `flow.Flow[R]` names the record a process is
about (the *artifact*); every other field is a node, configured by
attributes.

```vuka
package permits

import "github.com/vuka-lang/flow/v2"

type PermitFlow struct {
	flow.Flow[entities.JobPermit] @flow.Meta{Name: "permit", Title: "Job permits", Status: entities.JobPermit.Status}

	Start   flow.Start    @flow.Can(PermAdd) @flow.Next(PermitFlow.Request)
	Request flow.View     @flow.Title("Request approval") @flow.Can(PermAdd) @flow.Sets(StatusWaiting) @flow.Next(PermitFlow.Review)
	Review  flow.View     @flow.Title("Review permit") @flow.Can(PermApprove) @flow.Assign(deskOfficer)
	                      @flow.Outcomes("approve", "reject") @flow.Form(entities.JobPermit.StatusComment) @flow.Next(PermitFlow.Check)
	Check   flow.If       @flow.When(approved) @flow.Then(PermitFlow.Publish) @flow.Else(PermitFlow.Rejected)
	Publish flow.Function @flow.Run(createAdverts) @flow.Sets(StatusApproved) @flow.Next(PermitFlow.Approved)

	Approved flow.End @flow.Title("Approved")
	Rejected flow.End @flow.Title("Rejected") @flow.Sets(StatusRejected)
}

var Permits = flow.Register[PermitFlow]()
```

- **A node's name is its field name.** Tasks store `permit.Review`; renaming a
  field is a data migration (§6.5).
- **Edges are field references** (`PermitFlow.Review`), so the compiler checks
  them, the editor jumps to them and renames them. A cycle is an edge like any
  other.
- **Functions in attributes** (`approved`, `createAdverts`, `deskOfficer`) are
  ordinary Go functions; their signatures are checked against the node
  (`func(ctx context.Context, a *flow.Activation[entities.JobPermit]) bool` for
  `When`).
- A flow without a table of its own embeds `flow.Flow[LeaveRequest]` with a
  plain struct: the artifact is kept as JSON on the process.

### 3.1 Node types

| Node | What it does |
|---|---|
| `Start` | A person starts a process, through a form (`Form`) or a component (`Render`) |
| `StartCode` | Code starts it: `Permits.StartCode.Run(ctx, artifact)` |
| `View` | A human task: a form, outcomes, assignment, a due time |
| `If` | Decides right away: `When(fn)`, `Then`, `Else` |
| `Switch` | Decides right away: `Case(fn, node)…`, `Default(node)` |
| `Function` | Runs code inside the transaction, then continues |
| `Wait` | Waits for code to resume it: `Permits.Paid.Resume(ctx, task, data)` (a webhook, a payment) |
| `Split` | Opens several branches; `Always(node)` and `Next(node, when)` |
| `Join` | Continues once every branch that reached it has arrived |
| `Timer` | Continues after a delay or at a date read from the artifact |
| `Subprocess` | Starts another flow for records this one names, and waits for them |
| `End` | Finishes the process; any task still open is cancelled |

### 3.2 Attributes

| Attribute | On | Meaning |
|---|---|---|
| `Meta{Name, Title, Status}` | the embedded `flow.Flow` | the flow's name, its title, and an optional status field to mirror (§6.3) |
| `Can(perms…)` | `Start`, `View`, `Wait` | who may act; checked through the app's permission hook (§8) |
| `Assign(fn)`, `Assign(PermitFlow.Request)` | `View` | pick the assignee: a function, or whoever finished that node last |
| `Title`, `Description` | any | how the node and its tasks read in lists and history |
| `Form(fields…)`, `Render(fn)` | `Start`, `View` | the generated form from the artifact's fields, or a component |
| `Outcomes(names…)` | `View` | one button per outcome; the choice is in the task's data for the next decision |
| `Guard(fn)` | `View` (per outcome with `Guard("approve", fn)`) | refuse with a message; the button shows disabled with the reason |
| `Filter(fn)`, `ListFilter(fn)` | the flow, `View` | which records a node applies to; which land in a user's lists (§7.3) |
| `Sets(state)` | any | write the mirrored status field when the node is reached |
| `When`, `Then`, `Else`, `Case`, `Default` | `If`, `Switch` | the decision |
| `Run(fn)` | `Function` | the code |
| `After(d)`, `At(field)` | `Timer` | a delay, or a date field of the artifact |
| `Due(d)` | `View` | overdue after; shown in lists, and a reminder hook |
| `Undo(fn)` | `View`, `Function` | what reverses the node's effects when its task is undone |
| `Columns(fields…)` | the flow, `View` | the columns its lists show; each `flow.Col(field).Search().Sort()` |
| `Next(node)` | all but `If`, `Switch`, `End` | the edge out |

Every attribute is a typed Go value restricted to fields (`@vuka.Targets`);
one on the wrong node type is a compile error (§4).

## 4. Compiler support

Two compiler changes, and diagnostics.

1. **Field references inside attribute arguments.** Today a reference inside an
   attribute's arguments isn't lowered ([fields](/features/fields#what-it-becomes)).
   It must be, for `@flow.Next(PermitFlow.Review)` and
   `@flow.Form(entities.JobPermit.StatusComment)` to type-check. The lowering
   is the one references already get; only the place is new.
2. **A flow struct is recognised** — a struct embedding `flow.Flow[R]` — so its
   node attributes are checked together, as a graph.

Diagnostics, each with a code and a help line, reported by `vuka build`,
`vuka vet` and the editor:

| Rule | Example message |
|---|---|
| a node no `Start` reaches | `PermitFlow.Audit is never reached: no edge leads to it` |
| a dead end | `PermitFlow.Review has no way out: give it @flow.Next` |
| an edge to a field that isn't a node, or of another flow | `@flow.Next(PermitFlow.Status): Status is not a node` |
| `If` without `Then`/`Else`, `Switch` without a `Default` | — |
| a `Join` no `Split` feeds, a `Split` with one branch | — |
| a human node with no `Can` | `PermitFlow.Review: anyone could act; add @flow.Can or @flow.Public` |
| an attribute on the wrong node type | `@flow.Run is for Function nodes` |
| a function whose signature doesn't fit | `approved: want func(ctx, *flow.Activation[JobPermit]) bool` |
| `Sets` without `Meta{Status}` | — |
| `Form`/`Columns` naming a field the artifact doesn't have | the field reference error, with its suggestion |

The editor gets definition and rename through edges (they are field
references), hover on a node showing its permission, assignment, outcomes and
edges, and completion of node names in edges.

## 5. Generated code

`vuka gen` writes, per flow struct, what reflection would otherwise do at run
time:

- the node table: names, types, edges, attribute values, in declaration order;
- a typed handle, so the registered flow is used through methods:

```vuka
Permits.Start.Run(ctx, permit)                  // a person starts it (permission checked)
Permits.Review.Complete(ctx, task, "approve", data)
Permits.Review.Claim(ctx, task) / Assign(ctx, task, user) / Unassign(ctx, task)
Permits.Process(ctx, permitID)                  // the process of a record
Permits.Active(ctx, permitID)                   // its open tasks
Permits.History(ctx, permitID)
Permits.Undo(ctx, task) / Cancel(ctx, process)
Permits.Inbox(ctx) / Queue(ctx) / Archive(ctx)
```

`flow.Register[PermitFlow]()` registers it for the cross-flow lists
(`flow.Inbox(ctx)` and friends), the task routes and the dashboard, and
returns the handle.

## 6. Runtime

### 6.1 Tables

The module ships its tables as orm migrations (`flow/migrations`), applied
with the app's own.

- `flow_process`: id, flow, status (`new`, `done`, `cancelled`), artifact id
  (text, so integer, UUID and composite keys fit), data (JSON), parent task,
  version (§6.4), created, finished, started by.
- `flow_task`: id, process, flow, node, status (`new`, `assigned`, `started`,
  `scheduled`, `done`, `cancelled`, `revived`, `error`), owner, outcome, token
  (§6.2), data (JSON), error text, created, assigned, started, finished,
  scheduled (due or timer time).
- `flow_task_previous`: task, previous task — the graph of what led to what.

Indexes serve the lists: `(status, owner)`, `(flow, node, status)`,
`(process)`, `(flow, artifact)`, `(status, scheduled)`.

### 6.2 Activation

Every node runs through one lifecycle: a task is created, optionally assigned,
started, completed, and its successors are activated — all in the transaction
of the step that reached it. `If`, `Switch`, `Function` and `Split` complete
at once and the engine continues; `View` waits for a person, `Wait` for
`Resume`, `Timer` for the scheduler (`web/cron`), `Join` for its tokens,
`Subprocess` for its children's ends.

`Split` gives each branch a token derived from its own; `Join` continues when
every token that left the split has arrived, or has been cancelled.

A `Function` or `Run` that fails puts its task in `error` with the error text,
and the transaction that reached it rolls back to before the step; `Revive`
retries it. A panic is recovered the same way.

### 6.3 The status mirror

Apps keep reading a record's status column. `Meta{Status: R.Field}` with
`Sets(state)` on nodes writes it in the same transaction as the step, so the
column is always the state the process is in. Nothing else writes it: code
that changes it directly gets a vet warning (`Status is mirrored by
PermitFlow; set it through the flow`).

### 6.4 Two users at once

Each step updates `flow_process` with `version = version + 1 WHERE version =
?` before anything else, and checks the row was matched — not changed, which
some drivers report instead (`flow.ErrConflict`: "This record was changed by
someone else. Reload it and try again."). A task's own status change is
guarded the same way, so two people completing one task, or a timer firing as
a person acts, can't both win.

### 6.5 Undo, revive, cancel, change

- **Undo** a done task: allowed when no task it led to has started; runs the
  node's `Undo`, cancels the successors, reopens a new task at that node. Undo
  of a `Subprocess` undoes its children first, or refuses with the reason.
- **Revive** a cancelled or failed task: a new task at the same node, with the
  same token and data.
- **Cancel** a process: every open task, if each can be cancelled.
- **Changing a flow**: tasks store node names only, so adding nodes and edges
  needs no migration. A removed node keeps its old tasks readable in history;
  a renamed one needs `flow.Rename(migration, "permit", "Review", "Approval")`
  in a migration.

## 7. Permissions and lists

### 7.1 Who may act

`Can(perms…)` is checked through the app's permission hook (§8). `Assign`
gives a task an owner when it opens; otherwise it waits unassigned. Someone
who holds the node's permission can claim it; an owner can unassign it.

### 7.2 The three lists

Each is one query on `flow_task`, joined to the artifact table only when a
filter, search or sort needs it:

- **Inbox**: tasks assigned to me, `assigned` or `started`.
- **Queue**: unassigned `new` tasks of nodes whose permission I hold, narrowed
  by the filters below.
- **Archive**: tasks I completed.

`flow.Inbox(ctx)` lists across flows; each flow's handle lists its own. A
list is paged, searchable and sortable by the `Columns` of the flow or node,
in SQL.

### 7.3 Filters

A filter is a function of the request: `func(ctx context.Context)
(flow.Cond[R], error)`, built from orm conditions on the artifact's fields,
permissions (`flow.Perm(…)`), and `flow.And`, `flow.Or`, `flow.Not`. It is
evaluated once per request.

- **`Filter`** narrows which records a node applies to: where it doesn't
  match, the node offers nothing and a step is refused. It is enforced
  everywhere — lists, actions, steps.
- **`ListFilter`** only narrows what lands in a user's lists: an approver who
  may act on any record still sees, by default, the ones assigned to their
  desk.

## 8. App hooks

The engine asks the app four things, through one value registered once
(`flow.Use(hooks)`), each with a default:

```go
type Hooks struct {
	User  func(ctx context.Context) (id string, ok bool)       // default: web/auth's identity
	Can   func(ctx context.Context, perm string) bool          // default: web/auth.Can
	Audit func(ctx context.Context, e flow.Event) error        // default: none; an app writes its audit log
	Names func(ctx context.Context, ids []string) (map[string]string, error) // display names for owners
}
```

## 9. Views

- **Forms** are generated from the fields `Form` names, through the
  attributes those fields already carry (label, required, input, choices,
  placeholder — `field.Describe`); the submitted values are written to the
  artifact before the step's code runs.
- **Outcomes** render one button each, with their guards' reasons, and a
  confirmation where `Confirm` is set.
- **`Render(fn)`** replaces the form with any component, which completes the
  task with `flow.Complete(ctx, task, outcome, data)`.
- **The ui kit** (`flow/ui`): `<flow.Tasks>` (a list as a `DataTable`, with
  claim and open), `<flow.Actions artifact={…}>` (what the user may do on a
  record, inline or as a floating menu), `<flow.TaskView>` (a task's form or
  component, in a page or a modal), `<flow.History>` and `<flow.Progress>`
  (the steps done, the current ones, what comes next). They are live
  components: a task's change is broadcast to the pages showing it.

## 10. Packaging

- `github.com/vuka-lang/flow/v2`: the engine; depends on orm and web.
- `github.com/vuka-lang/flow/v2/ui`: the components; depends on ui.
- `github.com/vuka-lang/flow/v2/migrations`: the tables.
- Compiler and vet support live in vuka; the code generator beside them, as
  the orm's does.
- Released with the set; an app requires flow at the set's version.

## 11. Phases

1. **Engine and tables**: nodes as Go values (no attributes yet), activation,
   tasks, the mirror, conflicts, undo/revive/cancel, timers, split/join,
   subprocesses, the three lists with filters and columns, the hooks.
   Acceptance: every node's semantics tested in a transaction against SQLite,
   Postgres and MySQL; the lists one query each.
2. **Declaration**: field references in attribute arguments, the flow struct,
   the attributes, generated code and handles, diagnostics, editor support.
   Acceptance: each rule of §4 has a failing and a passing golden case.
3. **Views**: generated forms, outcomes, `flow/ui`, live updates.
   Acceptance: browser tests for starting, claiming, completing, undoing.
4. **A real app**: a permit-style approval flow, then three more, moved onto
   it, with their behaviour unchanged; what they needed beyond this RFC is
   added or recorded.
5. **Docs**: a guide page per part, the attribute reference, the error pages.

## 12. Open questions

- Whether `Outcomes` plus an `If` is the right shape for a decision, or a
  `View` should name its target per outcome (`@flow.On("reject",
  PermitFlow.Rejected)`) — shorter, but two ways to say one thing.
- Whether tasks' `data` and the process's `data` stay JSON, or flows may
  declare typed task data.
- How long done processes and tasks are kept, and whether an app may archive
  them to another table.
- Reminders for overdue tasks: a hook only, or mail and SMS through web's
  senders.

## 13. Compatibility

flow is new. Under the [compatibility promise](/project/compatibility) from
its first release: its attributes, the handle's methods, the hooks, the table
layout (changed only through shipped migrations), and the diagnostics' codes.
