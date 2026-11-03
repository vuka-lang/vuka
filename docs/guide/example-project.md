# Example Project

[[toc]]

## Introduction

[`examples/jobboard`](https://github.com/vuka-lang/vuka/tree/main/examples/jobboard)
is a small job board. Employers post jobs; job seekers search them, read
them and apply with a cover letter and a CV; an admin closes the jobs that
shouldn't be there. Each feature of the app uses one part of Vuka, and this
page walks through them in the order a request meets them.

The app keeps its data in SQLite and prints its mail to the log, so it runs
with nothing else installed but Go and the Tailwind CLI.

## Running It {#running}

The app reads its settings from `.env`. The example ships `.env.example`,
which needs no change to run:

```sh
cd examples/jobboard
cp .env.example .env
vuka run
```

Open `http://localhost:8080`. The app applies its migrations as it starts
and makes the admin `.env` names (`admin@example.com`, with
`ADMIN_PASSWORD`). Sign up once as an employer and once as a job seeker,
in two browsers, to see an application reach the employer's dashboard.

`vuka test ./...` runs the app's tests; each opens a fresh database.

## The Layout of the Project {#layout}

The project is what `vuka new jobboard` writes, with a `jobs` module added
beside the `users` one:

```text
main.vuka              the options web.New takes
pages/
  _layout.vuka.html    the document of every page
  (account)/           /login and /signup, with a guard
  (site)/              the site's header and every other page
    _layout.vuka.html
    index.vuka.html    /
    applications.vuka.html
    jobs/[job]/        /jobs/{job} and /jobs/{job}/apply, with a layout
    employer/          /employer, /employer/new, /employer/{job}, guarded
    admin/             /admin, guarded
  _parts/              JobSearch, the live search on /
modules/
  jobs/                models, the Board service, forms, a cron, GraphQL, an email
  users/               users, sign-up, the session and token stores
migrations/            0001_initial.go, 0002_categories.go
```

`main.vuka` is wiring only. The pages, the GraphQL endpoint and the
services are options of one app:

```vuka
		users.Admin(),           // ADMIN_EMAIL and ADMIN_PASSWORD: the first admin
		pages.Routes(),          // the pages under pages/
		web.GraphQL("/graphql"), // the @graphql.Query operations: jobs
```

```vuka
func services(app *web.App) {
	app.Service(users.NewUsers)
	app.Service(jobs.NewBoard)
}
```

See [Directory Structure](/web/getting-started) and
[Dependency Injection](/web/dependency-injection).

## Pages and Layouts {#pages}

Every file under `pages/` is a route, its path the URL. Two folders in
parentheses are [route groups](/web/folder-routes#route-groups): they add
nothing to the URL, and each brings the layout and guard of its own pages.
`(site)/_layout.vuka.html` is the header of every page but the sign-in
pages. It shows each visitor the links their permissions open:

```html
<nav class="flex items-center gap-1" aria-label="Main">
  <ui.Link href={urls.Root.Path()} active="exact" class={navLink}>Jobs</ui.Link>
  if perms.Has("jobs.apply") {
    <ui.Link href={urls.Applications.Path()} active="prefix" class={navLink}>My applications</ui.Link>
  }
  if perms.Has("jobs.post") {
    <ui.Link href={urls.Employer.Path()} active="prefix" class={navLink}>Employer</ui.Link>
  }
  if perms.Has("jobs.moderate") {
    <ui.Link href={urls.Admin.Path()} active="prefix" class={navLink}>Admin</ui.Link>
  }
</nav>
```

`ui.Link` moves between pages without reloading the document, and `active`
marks the link of the page shown ([Navigation](/web/navigation)). The
links come from `pages/urls`, which vuka writes: a renamed page breaks the
build, not the link ([Typed URLs](/web/folder-routes#typed-urls)).

`jobs/[job]/` is a path parameter. Its layout loads the job once for both
pages below it, and answers not found for an unknown one:

```html
<script lang="go">
package job

type Layout struct {
	Job      int64
	Children ui.Node
	job      jobs.Job
}

func (l *Layout) Load(ctx context.Context, board *jobs.Board) error {
	job, err := board.Job(ctx, l.Job)
	if errors.Is(err, orm.ErrNotFound) {
		return web.NotFound
	}
	l.job = job
	return err
}
</script>
```

See [Folder Routes](/web/folder-routes).

## Signing In {#auth}

The users module is the one `vuka new` writes: `auth.Module` signs people
in with it, and keeps their sessions in the database. Each role grants
permissions:

```vuka
var Roles = map[string][]string{
	string(Role.Admin):    {"*"},
	string(Role.Employer): {"jobs.post"},
	string(Role.Seeker):   {"jobs.apply"},
}
```

Every page needs a sign-in unless it is marked `@auth.Public`: the job
list, a job's page and the sign-up page are. The sign-up page creates the
user and signs them in. It isn't live, so its form posts over HTTP and
`auth.SignIn` can set the session cookie:

```html
<script lang="go">
@auth.Public
type Signup struct {
	Form  users.Signup
	Users *users.Users
}

func (p *Signup) save(ctx context.Context, f users.Signup) error {
	u := p.Users.Create(ctx, users.NewUser{Name: f.Name, Email: f.Email, Password: f.Password, Role: users.Role(f.Role)}).Get()?
	auth.SignIn(ctx, strconv.FormatInt(u.ID, 10))?
	if u.Role == users.Role.Employer {
		return web.Redirect(urls.Employer.Path())
	}
	return web.Redirect(urls.Root.Path())
}
</script>
```

Signing in and out are auth's own endpoints, `POST /auth/login` and
`POST /auth/logout`. See [Authentication](/web/auth).

## Guards {#guards}

A signed-in visitor still needs the right permission for some folders. A
`Guard` checks every page, form and event below its folder, and renders
nothing. `admin/guard.go` lets only moderators in:

```go
package admin

import (
	"context"

	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/auth"
)

// Guard lets only moderators into /admin.
type Guard struct{}

func (g *Guard) Load(ctx context.Context) error {
	if !auth.Can(ctx, "jobs.moderate") {
		return web.Forbidden
	}
	return nil
}
```

`employer/guard.go` does the same with `jobs.post`. Below it,
`employer/[job]/guard.vuka` binds the path's parameter and answers not
found for a job of another employer's, so the applications page never
sees one:

```vuka
type Guard struct {
	Job int64
}

func (g *Guard) Load(ctx context.Context) error {
	mine := jobs.Job.Objects.Filter(jobs.Job.ID.Eq(g.Job), orm.Q{"employer": users.Of(user).ID}).Exists(ctx)?
	if !mine {
		return web.NotFound
	}
	return nil
}
```

A visitor who isn't signed in never reaches a guard: auth sends them to
`/login` first. See [Guarding a Folder](/web/folder-routes#guards).

## Models {#models}

The jobs module's models are in `modules/jobs/model.vuka`. A job belongs
to a category and an employer (foreign keys), asks for skills (a
many-to-many), is worked in one of three ways (an enum) and may show a
salary (an `Option`):

```vuka
type Job struct {
	orm.Base     @orm.Meta{Ordering: []string{"-posted", "id"}}
	ID           int64
	Title        string @orm.Char{Max: 120}
	Description  string @orm.LongText
	Location     string @orm.Char{Max: 80}
	Kind         Kind
	Salary       Option[int64]
	Category     orm.FK[Category] @orm.Rel{OnDelete: orm.Restrict, Related: "jobs"}
	Skills       orm.M2M[Skill]
	Employer     orm.FK[users.User] `graphql:"-"` @orm.Rel{OnDelete: orm.Cascade}
	Status       Status             @orm.Index
	Closes       time.Time          @orm.DateField
	Posted       time.Time          @orm.Datetime{AutoNowAdd: true}
	Search       orm.TSVector       `graphql:"-"` @orm.SearchOf{Fields: []string{"title:A", "description:B"}, Config: "english"} @orm.FullText{Config: "english"}
	Applications []Application      `graphql:"-"`
}
```

```vuka
enum Kind string {
	FullTime = "full-time"
	PartTime = "part-time"
	Contract = "contract"
}

func (Kind.FullTime) Label() string { return "Full time" }
```

`Search` is the full-text document of the title and the description. On
SQLite it is an FTS5 table that triggers keep in step with `jobs`; on
Postgres a generated `tsvector` with a GIN index. See [Models](/orm/models)
and [Search](/orm/search).

## Migrations {#migrations}

`go tool orm makemigrations` compared the models with the migrations
before it and wrote `migrations/0001_initial.go`: every table, the
foreign keys, the unique pair of an application's job and applicant, and
the FTS5 table with its triggers. `go tool orm makemigrations --empty
categories` wrote the next file to fill in; it adds the categories and
skills the board starts with:

```go
// Generated by orm makemigrations. It is the program's own: review it, and edit it as you need.

package migrations

import m "github.com/vuka-lang/orm/v2/migration"

func init() {
	m.Register(m.Migration{
		Name:         "0002_categories",
		Dependencies: []string{"0001_initial"},
		Operations: []m.Operation{
			m.RunSQL{
				SQL: `INSERT INTO categories (name) VALUES ('Engineering'), ('Design'), ('Sales'), ('Support');
INSERT INTO skills (name, category_id) SELECT 'Go', id FROM categories WHERE name = 'Engineering';
INSERT INTO skills (name, category_id) SELECT 'PostgreSQL', id FROM categories WHERE name = 'Engineering';
INSERT INTO skills (name, category_id) SELECT 'TypeScript', id FROM categories WHERE name = 'Engineering';
INSERT INTO skills (name, category_id) SELECT 'Figma', id FROM categories WHERE name = 'Design';
INSERT INTO skills (name, category_id) SELECT 'User research', id FROM categories WHERE name = 'Design';
INSERT INTO skills (name, category_id) SELECT 'Negotiation', id FROM categories WHERE name = 'Sales';
INSERT INTO skills (name, category_id) SELECT 'CRM', id FROM categories WHERE name = 'Sales';
INSERT INTO skills (name, category_id) SELECT 'Customer service', id FROM categories WHERE name = 'Support';
INSERT INTO skills (name, category_id) SELECT 'Writing', id FROM categories WHERE name = 'Support';`,
				ReverseSQL: `DELETE FROM categories;`,
			},
		},
	})
}
```

`main.vuka` passes `ormweb.Set(db).MigrateOnStart(true)`, so the app
applies pending migrations as it starts. See [Migrations](/orm/migrations).

## Queries {#queries}

The `Board` service in `modules/jobs/service.vuka` holds every query the
pages make. The job list searches the documents, ranks what it finds, and
reads one page of it with the total:

```vuka
func (b *Board) Search(ctx context.Context, q string, category int64, page int) (orm.PageResult[Job], error) {
	jobs := Job.Objects.Filter(Job.Status.Eq(Status.Open)).SelectRelated(Job.Category.String())
	if category != 0 {
		jobs = jobs.Filter(Job.Category.ID.Eq(category))
	}
	if q = strings.TrimSpace(q); q != "" {
		doc, query := orm.SearchVector(Job.Title.Name(), Job.Description.Name()), orm.SearchQuery(q)
		jobs = jobs.Filter(orm.Match(doc, query)).Order(orm.SearchRank(doc, query).Desc())
	}
	return orm.Paginate(ctx, jobs, orm.PageRequest{Page: page, Size: 10})
}
```

Beside the list, each category shows its open jobs. One query counts them,
categories with none included:

```vuka
func (b *Board) Categories(ctx context.Context) ([]CategoryCount, error) {
	open := Category.Jobs.Where(Category.Jobs.Status.Eq(Status.Open)).Count()
	return Category.Objects.Annotate("open", open).Values[CategoryCount]("id", "name", "open").All(ctx)
}
```

See [Queries](/orm/queries), its [aggregates](/orm/queries#values-and-aggregates)
and [pagination](/orm/queries#pagination).

## Transactions {#transactions}

Posting a job saves the job, then links its skills. `@orm.Transaction`
runs the method in a transaction, so a skill that fails leaves no job
behind:

```vuka
@orm.Transaction
func (b *Board) Post(ctx context.Context, employer int64, in PostJob) Result[Job] {
```

Applying saves the application in a transaction too, and
`orm.OnCommit` emails the employer and tells their open pages only once it
is committed:

```vuka
	orm.OnCommit(ctx, func(ctx context.Context) {
		b.Mail.SendLater(ctx, mail.Message{
			To:      []string{employer.Email},
			Subject: "New application: " + job.Title,
			HTML:    NewApplication{Employer: employer.Name, Job: job.Title, Applicant: applicant.Name},
		})
		web.Broadcast(ctx, EmployerTopic(employer.ID), Applied{Job: job.ID, Title: job.Title, Applicant: applicant.Name})
	})?
```

See [Transactions](/orm/transactions).

## Forms {#forms}

A form is a struct. The job form's tags are its rules, its `Clean` method
checks what the tags can't, and its `<Field>Choices` methods fill the
selects. `Skills` follows `Category` (`depends:`), so choosing a category
offers that category's skills:

```vuka
type PostJob struct {
	ui.FormState
	Title       string    `validate:"required,max=120"`
	Category    string    `validate:"required"`
	Skills      []string  `depends:"Category" help:"Choose a category first"`
	Kind        string    `label:"Type" input:"radio" validate:"required"`
	Location    string    `label:"Location" validate:"required,max=80" placeholder:"Remote, or a city"`
	Salary      int64     `label:"Yearly salary" help:"Leave it empty to not show one"`
	Closes      time.Time `label:"Applications close"`
	Description string    `input:"textarea" validate:"required,max=5000"`
}
```

```vuka
func (f *PostJob) SkillsChoices(ctx context.Context) ([]ui.Option, error) {
	id, err := strconv.ParseInt(f.Category, 10, 64)
	if err != nil {
		return nil, nil
	}
	rows := Skill.Objects.Filter(Skill.Category.ID.Eq(id)).All(ctx)?
```

`employer/new.vuka.html` renders it whole, with `<ui.Form for={Form}
onSubmit={Save} submitLabel="Post the job" />`.

The application form holds the CV in a `ui.File`. The browser uploads the
file as soon as it is chosen, and the form carries a signed reference to
it:

```vuka
type ApplyForm struct {
	ui.FormState
	Letter string  `label:"Cover letter" input:"textarea" validate:"required,max=4000"`
	CV     ui.File `label:"CV" accept:".pdf,.doc,.docx,.txt" max:"2MB" validate:"required"`
}
```

The submit handler reads the file with `Open` and passes it on:

```html
<script lang="go">
func (p *Apply) save(ctx context.Context, f jobs.ApplyForm) error {
	job := p.board.Job(ctx, p.Job)?
	cv := f.CV.Open()?
	defer cv.Close()
	match p.board.Apply(ctx, job, users.Of(user), f.Letter, f.CV.Name(), cv) {
	case Ok(_):
		web.Flash(ctx, "success", "Your application to "+job.Title+" is on its way.")?
		return web.Redirect(urls.Applications.Path())
	case Err(e) if errors.Is(e, jobs.ErrApplied) || errors.Is(e, jobs.ErrClosed):
		return p.Form.Fail(e.Error())
	case Err(e):
		return e
	}
}
</script>
```

The flash message shows once, on the page the visitor lands on. See
[Forms](/ui/forms) and its [choices](/ui/forms#choices).

## A Live Page {#live}

The employer's dashboard, `employer/index.vuka.html`, embeds `ui.Live`:
its state stays on the server and the page changes in place. Its mount hook
subscribes it to the employer's topic, which `Apply` broadcasts on:

```html
<script lang="go">
type Index struct {
	ui.Live
	board    *jobs.Board
	me       int64
	postings []jobs.Posting
	arrived  []jobs.Applied
}

@ui.Mount
func (p *Index) mount(ctx context.Context) error {
	p.me = users.Of(user).ID
	p.Subscribe(jobs.EmployerTopic(p.me))
	return p.load(ctx)
}

func (p *Index) Update(ctx context.Context, a jobs.Applied) error {
	p.arrived = append([]jobs.Applied{a}, p.arrived...)
	return p.load(ctx)
}
</script>
```

Each application adds a line to every dashboard the employer has open and
counts it in the table. See [Live Pages](/web/live) and
[Broadcasting to Pages](/web/live#broadcasts).

## State in the Address {#url-state}

The job list, `/`, isn't a live page: it renders the `JobSearch` component
from `pages/_parts/`, a [private folder](/web/folder-routes#private-folders).
The component is live, and keeps its search, its category and its page in
the address with `@ui.URL`, so a search can be bookmarked and shared:

```html
<script lang="go">
@auth.Public
type JobSearch struct {
	ui.Live
	q        string @ui.URL("q")
	category int64  @ui.URL("category")
	at       int    @ui.URL("page").Default(1)
	board    *jobs.Board
}

func (s *JobSearch) Results(ctx context.Context) (orm.PageResult[jobs.Job], error) {
	return s.board.Search(ctx, s.q, s.category, s.at)
}

func (s *JobSearch) Search(q string) { s.q, s.at = q, 1 }
</script>
```

Typing in the search box calls `Search`, and the address follows:
`/?q=postgresql`. The markup calls `Results()` once per render, so the
results follow the fields whether an event or the address changed them.
`@auth.Public` matters here as on a page: without it, the component's
events would need a sign-in. See [URL State](/web/url-state).

## A Cron {#cron}

Jobs close on their date. A cron in the jobs module closes the expired
ones each night at one:

```vuka
@web.Cron("0 1 * * *")
func closeExpired(ctx context.Context, db *orm.Databases, b *Board) error {
	n := b.CloseExpired(orm.WithDatabases(ctx, db), time.Now())?
	if n > 0 {
		slog.InfoContext(ctx, "closed expired jobs", "n", n)
	}
	return nil
}
```

A cron runs outside any request, so it puts the app's databases on its
context itself. See [Crons](/web/crons).

## Mail {#mail}

The email an employer gets is a shape, `modules/jobs/new_application.vuka.html`:

```html
<script lang="go">
package jobs

import "github.com/vuka-lang/ui/v2"

// NewApplication is the email an employer gets for each application.
type NewApplication struct {
	Employer  string
	Job       string
	Applicant string
}
</script>
```

`mail.Provide()` in `main.vuka` reads `MAIL_DRIVER`: `log`, in
`.env.example`, prints each message instead of sending it. See
[Mail](/web/mail).

## GraphQL {#graphql}

The open jobs are also a read-only GraphQL query, for clients other than
the pages:

```vuka
@graphql.Query
@auth.Public
func Jobs(ctx context.Context, page graphql.Page) (graphql.Connection[Job], error) {
	return graphql.Paginate(ctx, Job.Objects.Filter(Job.Status.Eq(Status.Open)), page)
}
```

The schema follows the model: `category` and `skills` load through their
relations, and the fields tagged `graphql:"-"` — the employer, the search
document, the applications — are left out:

```sh
curl localhost:8080/graphql -H 'Content-Type: application/json' \
  -d '{"query": "{ jobs(first: 5) { totalCount edges { node { title kind salary category { name } } } } }"}'
```

See [GraphQL](/web/graphql).

## Islands {#islands}

The app has no [islands](/web/islands). An island is a component of a
frontend framework, built by Vite from packages npm installs, and the
example keeps to what runs without a network.

## Tests {#tests}

The tests run with `vuka test ./...`. The jobs module's tests call the
`Board` on a fresh database from `ormtest`, with only the tables they
need:

```vuka
	ctx := ormtest.Open(t, orm.Objects[users.User](), orm.Objects[Category](), orm.Objects[Skill](), orm.Objects[Job](), orm.Objects[Application]())
```

`main_test.vuka` starts the whole app on its own SQLite file. `viewtest`
fills and submits forms as a visitor would; this test checks that choosing
a category offers its skills, and where a job posted sends the employer:

```vuka
	p := viewtest.Mount[*employer.New](t, app, "/employer/new")
	p.Fill(&p.Page.Form.Title, "Backend engineer").Fill(&p.Page.Form.Category, strconv.FormatInt(eng.ID, 10))
	if !strings.Contains(p.HTML(), ">Go</label>") {
		t.Fatal("choosing Engineering doesn't offer its skills")
	}
```

`TestApplication` signs in an employer and a job seeker, opens the
dashboard's connection, posts an application with a CV as a browser
without script does, and checks the dashboard's update, the email and the
pages that list it. `TestCloseExpired` runs the cron with `crontest.Run`.
See [Testing Web Apps](/web/testing), [Testing Forms](/ui/forms#testing)
and [Database Testing](/orm/testing).
