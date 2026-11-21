# RFC: flow, approval workflows over your records

Status: approved design (2026-11-20), declaration layer revised 2026-11-21
(outcome structs, fall-through edges, decisions as Actions: see §3, §4, §5 and
"Earlier drafts"). Each phase of §13 lands on its own.

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
methods, marked by attributes, and they are written in the order the process
goes. A `Start` or an `Action` with no `To` goes on to the next node below it;
`To` is written only where the route leaves that order. A View's decisions are
structs of their own, each carrying `@flow.Outcome`. The attributes are the
whole declaration: there is no wiring method, no registration call and no code
generation.

```vuka
package orders

@flow.Meta{Name: "order", Title: "Orders", Version: 1}
type OrderProcess struct {
	flow.Workflow[Order]

	Billing *BillingService // plain pointer fields are injected services
	Mail    *MailService
}

@flow.Start{Can: "orders.submit"}
func (p *OrderProcess) Submitted() {}

@flow.Start // a second entry, started from code
func (p *OrderProcess) Imported() {}

@flow.View{Can: "orders.review", Sets: StatusReview, NoUndo: true}
func (p *OrderProcess) Review() {}

@flow.Outcome{Of: (*OrderProcess).Review, To: (*OrderProcess).Charge, Can: "orders.approve",
	Title: "Approve order", Done: "Order approved", Guard: inStock}
type Approve struct {
	Note string `form:"note"`
}

func (a Approve) Apply(ctx context.Context, p *OrderProcess) error {
	p.Record().Note = a.Note
	return p.Save(ctx)
}

@flow.Outcome{Of: (*OrderProcess).Review, To: (*OrderProcess).Rejected, Can: "orders.reject",
	Title: "Reject order", Done: "Order rejected", Undo: flow.CanUndo}
type Reject struct {
	Note string `form:"note" validate:"required"`
}

@flow.Action
func (p *OrderProcess) Charge(ctx context.Context) error {
	return p.Billing.Charge(ctx, p.Record().ID)
}

@flow.End{Sets: StatusDone}
func (p *OrderProcess) Done() {}

@flow.End{Sets: StatusRejected}
func (p *OrderProcess) Rejected() {}
```

- **Flows are found, not registered.** `@flow.Meta` marks the flow's type, and
  the flow is found through vuka's registry, which lists the types that carry
  an attribute (`vuka.TypesWith[flow.Meta]()`). It is resolved and checked when
  it is first used or when `flow.Validate()` runs (§12).
- **A node's name is its method name**, unless the attribute's `Name` pins
  another: `@flow.View{Name: "Review"}` on a method renamed to `Inspect` keeps
  the tasks stored as `order.Review` working. Without `Name`, renaming a method
  is a data migration (§8.5).
- **Edges are method expressions** (`(*OrderProcess).Review`), so the compiler
  checks them, the editor jumps to them and renames them. A cycle is an edge
  like any other.
- **Source order is the default route.** Nodes are ordered by file name, then
  line. `Start` and `Action` with no `To` go on to the next node that is not a
  `Start`, so stacked entries all lead to the first step. A `Start` or `Action`
  that is last and has no `To` is an error ("falls off the end"). `End`
  never has a `To`; `Split`, `Join`, `Timer`, `Sub` and decisions name their
  targets. Moving a method moves where its predecessor goes, so a test can pin the
  resolved graph with `flowtest.Graph(Orders)` (§12).
- **An outcome is a struct.** `@flow.Outcome` goes on a struct type; `Of` names
  its View by method expression. The struct is the outcome's form: bound and
  validated like a View's form (§5). Its name, as tasks store it, is the type's
  lowercased (`Approve` is `"approve"`), so a database written by an earlier
  declaration keeps working; `Name` overrides. The optional `Apply` method is
  the outcome's code (§5).
- **Outcome structs are found by their attribute** (`vuka.TypesWith[flow.Outcome]()`)
  and filed under the View their `Of` names, so they may be in any file or
  package. The buttons are in declaration order, which is the registry's
  registration order: in a package, files by name and then source order;
  across packages, Go's init order. An outcome with no `Of`, or with an `Of` of
  another flow, is reported by the flow its `To` names.
- **A View with one outcome** uses the sugar fields on `@flow.View` itself
  (`To`, `Label`, `Title`, `Confirm`, `Done`, `Guard`, `Form`, `View`, `Undo`)
  and needs no struct: a lone "Request approval" button is one line. A View is
  sugar or structs, not both.
- **A View with no code of its own** is written `func (p *T) M() {}`.
- **Functions in attributes** (`inStock`) are ordinary Go functions; their
  signatures are checked at boot (§12).
- A flow over a record with no table of its own uses a plain struct for R: the
  record is kept as JSON on the process.

## 4. Attributes

`@flow.Meta` goes on the flow's type and `@flow.Outcome` on an outcome struct;
the others go on methods.

| Attribute | Fields | Meaning |
|---|---|---|
| `@flow.Meta` (type) | Name, Title, Version | the flow's identity; Version is recorded on each process |
| `@flow.Start` | To, Can | an entry node; startable from the UI when `Can` is set, always from code; with no `To` it falls through |
| `@flow.View` | Can, Sets, Assign, Due, Page, Template, NoUndo; single-outcome sugar: To, Label, Title, Confirm, Done, Guard, Form, View, Undo | a human task |
| `@flow.Outcome` (struct) | Of, To, Can, Label, Title, Confirm, Done, Guard, View, Page, Template, Undo, Name | one decision of the View `Of` names; the struct is its form |
| `@flow.Action` | To, Sets | a code node; its body runs in the transition's transaction, and an error rolls the move back; with no `To` it falls through. If its method returns `(flow.Next, error)` it is a decision and `To` is `[]any` of the possible targets |
| `@flow.Split` | To []any | parallel fan-out, one token per target |
| `@flow.Join` | To | continues once every token that left the split has arrived |
| `@flow.Timer` | After, To | continues after a delay |
| `@flow.Sub` | Flow, To | starts a child flow; continues when the child ends |
| `@flow.End` | Sets | finishes the process; any task still open is cancelled |

Every node attribute (`Start`, `View`, `Action`, `Split`, `Join`, `Timer`,
`Sub`, `End`) also takes **Name**, the node's stored name, which defaults to
the method's.

Field types:

- **Of** and **To** are method expressions of the flow's struct. `Of` is
  required on an outcome and must be a View of this flow.
- **Name** (outcome) is the stored name; the default is the type name
  lowercased. **Label** is the button's text, the humanised type name by default.
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
| View | `func (p *T) M(ctx context.Context[, in FormStruct]) error`, or `func (p *T) M()` |
| Action | `func (p *T) M(ctx context.Context) error` |
| Decision (an Action) | `func (p *T) M(ctx context.Context) (flow.Next, error)` |
| Outcome's code | `func (a Outcome) Apply(ctx context.Context, p *T) error` (value or pointer receiver) |
| Start, End, Split, Join, Timer, Sub | empty body |

- **An outcome struct is the form.** The values a person typed are bound to its
  fields (by field name, `form:` or `json:` tag, any case, trimmed, converted to
  the field's type) and checked by web's binder, the app's validator and the
  struct's own `Validate` method. A struct with no fields to ask for is a plain
  button. Fields two outcomes share live in a struct both embed.
- **`Apply` is the outcome's own code.** `a` is the bound struct and `p` the
  flow's struct for this step, with `p.Record()`, `p.Save` and the injected
  services. It runs after `Can`, `Guard` and the form's checks, inside the
  transition's transaction, before `Sets`, `Moved` and the token move. Returning
  an error, or `flow.Refuse`, vetoes the move and rolls it back.
- **A View's body is the code common to its outcomes**, and with `Apply` it is
  often nothing. When both exist **the body runs first and `Apply` second**, on
  the same copy of the struct: the body is the shared step, whose error stops
  the move before anything specific to the decision is done, and `Apply`, the
  closest to the transition, writes last before `Sets`. `p.Outcome()` names the
  outcome in both.
- **With outcome structs the View's method takes no form parameter**: each
  outcome has its own struct, and a parameter would be a third type. A
  single-outcome View (sugar) may take one, and `Form` on it must then be that
  type. An outcome struct that is not the method's parameter type, when the
  method has one, is an error.
- **A decision is an Action** whose method returns `(flow.Next, error)`. Its
  `To` is the list of nodes the body may choose from with `p.Go(target)`; the
  kind is read from the signature, so there is no separate attribute. Whether
  `p.Go` named one of them is checked when the step runs.
- **Later nodes read the submitted form** with `p.Form() any` (a pointer to the
  outcome's struct) and `flow.FormAs[F](p) (F, bool)`. In the View's body, in
  `Apply` and in an Action in the same transition it is the bound form in memory;
  in a node reached in a later transition it is decoded from the stored task data
  into the outcome's struct. It is the form of the last completed View, nil when
  that had none.

## 6. flow.Workflow[R]: the embedded base, and the instance

- **The process struct is the only noun.** `flow.Workflow[R]` carries the
  flow's commands (§10), and they promote, so they are called on the struct.
- **`flow.Of[*T]() *T`** returns the flow's one shared instance, memoized. It
  is made at once and resolved on first use, so a package-level
  `var Orders = flow.Of[*OrderProcess]()` is safe even though Go initialises it
  before the `init` that records the attributes. The instance is the zero value
  of the struct. Its pointer and interface fields are services, filled once
  (only those still nil) from `flow.Provide` or the app container.
- **Injection.** A web app with `flowui.Use()` has every flow's instance in its
  container (`flow.Instances()`), so a hook or handler parameter of type
  `*OrderProcess` is injected. `flowui.Use()` also runs `flow.Validate()` at
  app start, so `main` needs no flow line.
- **`flow.Template(&T{…})` is the one escape hatch**, for a struct that must be
  built by hand. It is called before the flow is first used or validated, and
  it makes that value the instance (or copies it into the instance `Of`
  already made). Fields it sets are kept rather than injected. After the flow
  has resolved, it panics.
- **A bare `&OrderProcess{}` is no flow.** Its commands return an error naming
  `flow.Of` and injection. They never panic and never silently do nothing.
- Attributes are read through vuka's registry, which records them at program
  start, so there is no init-order problem and no separate registration file.
- On every activation the template is **cloned**, and the clone's
  `Workflow[R]` holds the live activation along with the engine. A clone
  belongs to one transition, and state written on it does not leak into
  another process. The commands work on the clone too, and there they join
  the transition's transaction (§9).
- Methods on the clone:

| Method | Returns |
|---|---|
| `p.Record() *R` | the record the process is about |
| `p.Outcome() string` | the outcome that fired, in a View body, an outcome's `Apply` or `Moved` |
| `p.Say(msg)` | a message shown to the acting user after the move |
| `p.Go(method) flow.Next` | the chosen target, from a decision |
| `p.Actor() int64` | the acting user |
| `p.ProcessID()` | the process id |

## 7. Convention methods

Looked up by name when the flow resolves; each is optional.

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
the step that reached it. Action (decisions too), Split and Start complete at once and
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

`Can` + `Guard` → form checks → View body → outcome `Apply` (or Action body) → `Sets` → `Moved` → token moves.

- An error at any step rolls everything back.
- Writes to other tables made with that `ctx` join the transaction, so a body
  can update the order, insert an invoice and move the process atomically.
- `OtherFlow.Start` or `Advance(ctx, …)` called from a body joins it too, so a
  cross-flow move is atomic: either both processes moved or neither did. The
  same holds for the clone's own commands: `p.Advance(ctx, &other, "approve")`
  in an Action rolls back with the step.
- External side effects (HTTP calls, SMS, mail) cannot roll back. Put them last
  in the body, or in their own Action node, and make them safe to repeat.
  `orm.OnCommit` runs work only after the transaction commits.

## 10. The commands

The instance is the API:

```vuka
Orders := flow.Of[*OrderProcess]()            // or injected

Orders.Start(ctx, rec)                        // the first Start node
Orders.StartAt(ctx, rec, (*T).Imported)       // a named Start node
Orders.StartProcess(ctx, rec, node)           // either, giving the process
Orders.Adopt(ctx, rec)                        // a record already in a state
Orders.Advance(ctx, rec, "approve")           // move a record from code
Orders.Complete(ctx, task, "approve", form)   // complete a task
Orders.Claim / Assign / Unassign / BeginTask(ctx, task, …)
Orders.Task(ctx, task, form) / Tasks(ctx, "queue", opts…) / Steps(ctx, recID)
Orders.Process / Active / History(ctx, recID)
Orders.Undo / CancelTask / Revive(ctx, task) / Cancel(ctx, process)
Orders.Name() / Err() / FlowView()
```

### Reserved names

Because the commands are methods of the struct, a node method, another method
or a field named like one would shadow it. That is an error with a
suggestion: "node Start shadows the Start command; rename the method and pin
its stored name with Name". The reserved names are the commands above and the
step-scoped methods of §6: `Active`, `Actor`, `Adopt`, `Advance`, `Assign`,
`BeginTask`, `Cancel`, `CancelTask`, `Claim`, `Complete`, `Err`, `FlowView`,
`Form`, `Go`, `History`, `Name`, `Outcome`, `Process`, `ProcessID`, `Record`,
`Revive`, `Save`, `Say`, `Start`, `StartAt`, `StartProcess`, `Steps`, `Task`,
`Tasks`, `Unassign`, `Undo`. The command that marks a task started is
`BeginTask`, so that a Start node may be called `Begin`.

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
`@flow.Outcome`, says where its task UI lives in one of two ways:

- **`Page: "/orders/view"`** reuses a page the app already has. The link to the
  task carries it (`?task=<id>`). On that page, `flow.TaskFrom(ctx)` returns
  the open task, or nil when the page was not opened from a workflow link, so
  the page can show the outcome buttons and hide what the task does not allow.
- **`Template: "review.vuka.html"`** is a `.vuka.html` file in the flow's
  package, rendered with a typed context `*flow.Task[R]`: the record, the form
  (each outcome's own struct), the allowed outcomes with their labels,
  confirmations and guard refusals, the actor, the process id, and helpers to
  complete an outcome. The template uses any of the project's own components.
  The file is checked at compile time where vuka allows a file-typed attribute
  field, and when the flow resolves otherwise (§12).

With neither set, the task completes from code (`Orders.Complete`) or through a
minimal plain form, with no ui kit.

Inboxes, dashboard tabs and record lists are built by the project on the
instance's list and task APIs (§10): `Orders.Tasks(ctx, "inbox" | "queue" | "archive")` returns
paged, searchable, sortable rows, so a project's own `DataTable` or page shows
them with its own columns and filters. A ready-made dashboard (`flowui`) may
ship as a separate, optional module; core never depends on it.

Row security is `Scope` (§7), enforced inside these APIs; a project's filters
only narrow what it shows.

## 12. Validation

A flow is checked when it resolves, at first use or `flow.Validate()`.
`Validate` finds every flow by `@flow.Meta` and fails the boot (through
`flowui.Use` at app start, or `main`), naming the method and suggesting the
likely intended name, on any of:

- no Start node (at least one is required);
- a `To` that is not a node method of this type;
- an unreachable node;
- a `Start` or `Action` with no `To` that is the last node ("falls off the
  end");
- a type with `@flow.Meta` that does not embed `flow.Workflow[R]`;
- a node, method or field named like a command (§10, reserved names), or two
  nodes stored under one name;
- a View with no outcome (neither sugar nor an outcome struct naming it);
- an outcome type that is not a struct, carries `@flow.Outcome` twice, has no
  `Of` or `To`, whose `Of` is not a
  View of this flow (a method of another struct, a method that is no node, a
  node of another kind), whose name another outcome of the View has, that is not
  the View's form parameter type when the View has one, or whose `Apply` is not
  `func(ctx context.Context, p *T) error` (a near miss such as `Aply` is
  reported as a probable `Apply`);
- a View's sugar fields together with outcome structs;
- an End with outgoing edges;
- a Guard, Assign or form whose signature does not fit R;
- an `Action` with a list in `To` whose method does not return `flow.Next`, a
  method that returns `flow.Next` with no list in `To`;
- a `p.Go` target that is not in the decision's `To` (when the step runs);
- a Join not reachable from its Split;
- a `Sub.Flow` that is not declared (a name or an instance), or two flows of one name;
- a `Template` file that does not exist or does not parse against `*flow.Task[R]`.

`flowtest` runs the same checks in a test, so a broken graph fails `go test`
before it fails a boot. `flowtest.Graph(Orders)` returns the resolved graph as stable
text, nodes in source order with their kind and where each leads, so an app can
pin it in a golden test and an accidental rewiring (moving a method changes
what falls through to what) fails loudly:

```
order
  Submitted  Start   -> Review
  Imported   Start   -> Review
  Review     View    approve -> Charge, reject -> Rejected
  Charge     Action  -> Done
  Done       End
  Rejected   End
```

A View lists its outcomes by name, a decision its choices joined by `|`, a
Split its branches joined by `&`. A node with a pinned `Name` reads as its
method followed by the stored name, `Inspect(Review)`.

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
   the instance and its commands, the checks of §12. Acceptance: each rule of §12 has a failing
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
- the `Button`/`Confirm`/`On(...)` outcome chains, now `@flow.Outcome` structs;
- `Col`/`ColOf`, `Filter`/`ListFilter`/`Status`/`Newest` and every other
  presentation setter on the flow: lists are the project's, on the instance's
  APIs (§11), and `Status`/`Scope` move to the type (§7);
- `flow.Register` and the `Handle` type: flows are found by `@flow.Meta`, and
  the commands are on the struct (§6, §10);
- `Render` and any task UI inside the library;
- `Graph()`.

## 16. Compatibility

flow is new. Under the [compatibility promise](/project/compatibility) from
its first release: its attributes, the commands and their reserved names, the hooks, the table
layout (changed only through shipped migrations), and the validation errors.

## Earlier drafts

Several earlier shapes were dropped. Spec structs and, after them, node fields
with a `Graph()` method put the outcomes far from the node that handles them, so
a reader jumped between two places to follow one step. Type-level `@flow.Route`
headers had the same distance and gave nothing to hang per-outcome
configuration on (permission, title, guard, undo). A nested `@flow.Switch{Cases}`
literal was hard to read and hard to check, and a transition hook that replaced
`Sets` made the status column depend on code instead of a declaration; both
gave way to a decision with `p.Go`, and `Sets` plus an optional `Moved`.

The next draft repeated `@flow.On` on the View, one attribute per outcome. It
read well for two buttons and badly for the rest: the attributes pile up above
a method whose body is a `switch p.Outcome()`, the outcome's form is a separate
struct named by a `Form` field, and the code of a decision lives away from the
decision. The current shape puts each decision in one place, a struct that is
its declaration, its form and (through `Apply`) its code, which is the unit a
reader and a validator already treat as one. It brings back some of the distance
of `@flow.Route`, and pays for it in two ways: the outcome names its View with
`Of` (a method expression, so the compiler and the editor follow it), and
the registry now lists the types carrying an attribute, so the structs are
found rather than passed to a `Register` call. That registry call also removed
`Register` itself, and with it the separate handle: the struct the flow is
declared on is the value its commands run on. The
other two changes of that revision are small. Fall-through makes the common
straight line of `Start` and `Action` nodes need no `To`, and `flowtest.Graph`
is the guard that moving a method does not rewire a flow unnoticed. `@flow.Decide`
folded into `@flow.Action` because a decision is an action whose body picks the
target, which its signature already says.
