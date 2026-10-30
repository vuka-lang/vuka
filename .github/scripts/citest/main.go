// Command citest runs CI's go test steps.
//
//	citest run [-group name] [-title t] -- <go test flags and packages>
//	citest check -- <packages>
//
// run runs go test (the tests of one group of .github/e2e-groups.txt, with
// -group); when the only failures are tests listed in .github/flaky.txt it
// runs exactly those once more, and the job passes if they pass then. Each
// retry is written to $GITHUB_STEP_SUMMARY: the test, its first failure's
// message and its issue. Any other failure — a test not listed, a build
// failure, a panic, a timeout — fails at once.
//
// check lists the packages' tests (go test -list) and fails unless each is
// run by exactly one group, every group runs a test, and the workflow runs
// every group.
//
// Both read the files relative to the repository root, found from the
// working directory.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	groupsFile   = ".github/e2e-groups.txt"
	flakyFile    = ".github/flaky.txt"
	workflowFile = ".github/workflows/ci.yml"
	restGroup    = "rest" // every test no other group names
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "check":
		err = check(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "citest:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: citest run [-group name] [-title t] -- <go test args>\n       citest check -- <packages>")
	os.Exit(2)
}

// repoRoot is the directory holding .github, at or above the working one.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, ".github")); err == nil && st.IsDir() {
			return dir, nil
		}
		up := filepath.Dir(dir)
		if up == dir {
			return "", errors.New("no .github directory at or above the working directory")
		}
		dir = up
	}
}

type group struct {
	name    string
	lines   []line // the group's regular expressions, one per test name or names
	pattern string // the lines as one, anchored: ^(?:a|b)$
	re      *regexp.Regexp
}

// readGroups reads "[name]" headers, each followed by regular expressions
// for whole top-level test names; the rest group has none.
func readGroups(root string) ([]group, error) {
	lines, err := readLines(filepath.Join(root, groupsFile))
	if err != nil {
		return nil, err
	}
	var gs []group
	seen := map[string]bool{}
	for _, l := range lines {
		if strings.HasPrefix(l.text, "[") && strings.HasSuffix(l.text, "]") {
			name := strings.TrimSpace(l.text[1 : len(l.text)-1])
			if name == "" || seen[name] {
				return nil, fmt.Errorf("%s:%d: group %q empty or twice", groupsFile, l.n, name)
			}
			seen[name] = true
			gs = append(gs, group{name: name})
			continue
		}
		if len(gs) == 0 {
			return nil, fmt.Errorf("%s:%d: a pattern before any [group]", groupsFile, l.n)
		}
		g := &gs[len(gs)-1]
		if g.name == restGroup {
			return nil, fmt.Errorf("%s:%d: the %s group runs what the others don't: it lists nothing", groupsFile, l.n, restGroup)
		}
		if strings.ContainsAny(l.text, "/ \t") {
			return nil, fmt.Errorf("%s:%d: a group names top-level tests (no / or spaces)", groupsFile, l.n)
		}
		if _, err := regexp.Compile(l.text); err != nil {
			return nil, fmt.Errorf("%s:%d: %v", groupsFile, l.n, err)
		}
		g.lines = append(g.lines, l)
	}
	for i := range gs {
		g := &gs[i]
		if g.name == restGroup {
			continue
		}
		if len(g.lines) == 0 {
			return nil, fmt.Errorf("%s: group %s lists no test", groupsFile, g.name)
		}
		alts := make([]string, len(g.lines))
		for j, l := range g.lines {
			alts[j] = l.text
		}
		g.pattern = "^(?:" + strings.Join(alts, "|") + ")$"
		g.re = regexp.MustCompile(g.pattern)
	}
	return gs, nil
}

type flaky struct{ name, issue string }

// readFlaky reads lines of "TestName[/subtest] issue-URL".
func readFlaky(root string) ([]flaky, error) {
	lines, err := readLines(filepath.Join(root, flakyFile))
	if err != nil {
		return nil, err
	}
	var fs []flaky
	for _, l := range lines {
		f := strings.Fields(l.text)
		if len(f) != 2 || !strings.HasPrefix(f[0], "Test") || !strings.HasPrefix(f[1], "https://github.com/") {
			return nil, fmt.Errorf("%s:%d: want \"TestName[/subtest] https://github.com/…/issues/N\"", flakyFile, l.n)
		}
		fs = append(fs, flaky{f[0], f[1]})
	}
	return fs, nil
}

// flakyFor is the entry covering the test name: the test itself or a parent.
func flakyFor(fs []flaky, name string) (flaky, bool) {
	for _, f := range fs {
		if name == f.name || strings.HasPrefix(name, f.name+"/") {
			return f, true
		}
	}
	return flaky{}, false
}

type line struct {
	n    int
	text string
}

func readLines(path string) ([]line, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ls []line
	for i, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			ls = append(ls, line{i + 1, l})
		}
	}
	return ls, nil
}

// --- check

func check(args []string) error {
	fl := flag.NewFlagSet("check", flag.ExitOnError)
	fl.Parse(args)
	pkgs := fl.Args()
	if len(pkgs) == 0 {
		pkgs = []string{"./..."}
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	gs, err := readGroups(root)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", append([]string{"test", "-list", "."}, pkgs...)...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go test -list: %v", err)
	}
	var tests []string
	for _, t := range strings.Fields(string(out)) {
		// -run selects tests, examples and fuzz tests' seed corpora.
		if strings.HasPrefix(t, "Test") || strings.HasPrefix(t, "Example") || strings.HasPrefix(t, "Fuzz") {
			tests = append(tests, t)
		}
	}
	slices.Sort(tests)
	tests = slices.Compact(tests)

	var problems []string
	count := map[string]int{}
	hasRest := false
	for _, g := range gs {
		hasRest = hasRest || g.name == restGroup
	}
	for _, t := range tests {
		var in []string
		for _, g := range gs {
			if g.re != nil && g.re.MatchString(t) {
				in = append(in, g.name)
			}
		}
		switch {
		case len(in) > 1:
			problems = append(problems, fmt.Sprintf("%s is in %d groups: %s", t, len(in), strings.Join(in, ", ")))
		case len(in) == 1:
			count[in[0]]++
		case hasRest:
			count[restGroup]++
		default:
			problems = append(problems, fmt.Sprintf("%s is in no group", t))
		}
	}
	for _, g := range gs {
		for _, l := range g.lines {
			re := regexp.MustCompile("^(?:" + l.text + ")$")
			if !slices.ContainsFunc(tests, re.MatchString) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s names no test", groupsFile, l.n, l.text))
			}
		}
	}
	wf, err := os.ReadFile(filepath.Join(root, workflowFile))
	if err != nil {
		return err
	}
	for _, g := range gs {
		if count[g.name] == 0 {
			problems = append(problems, fmt.Sprintf("group %s runs no test", g.name))
		}
		if !regexp.MustCompile(`(?m)(group:|-group) *` + regexp.QuoteMeta(g.name) + `\s*$`).Match(wf) {
			problems = append(problems, fmt.Sprintf("%s runs no job for group %s", workflowFile, g.name))
		}
	}
	fs, err := readFlaky(root)
	if err != nil {
		return err
	}
	for _, f := range fs {
		top, _, _ := strings.Cut(f.name, "/")
		if !slices.Contains(tests, top) {
			problems = append(problems, fmt.Sprintf("%s lists %s, which isn't a test", flakyFile, f.name))
		}
	}
	for _, g := range gs {
		fmt.Printf("%-16s %3d tests\n", g.name, count[g.name])
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s:\n\t%s", groupsFile, strings.Join(problems, "\n\t"))
	}
	fmt.Printf("every one of %d tests is in exactly one group\n", len(tests))
	return nil
}

// --- run

// event is go test -json's (test2json's) record.
type event struct {
	Action  string
	Package string
	Test    string
	Elapsed float64
	Output  string
	// FailedBuild names the package whose build failed, on a package's fail.
	FailedBuild string
}

type result struct {
	failed   map[string]bool // tests (and subtests) that failed
	output   map[string][]string
	elapsed  map[string]float64 // top-level tests' durations
	fatal    []string           // what fails the run whatever the tests: panics, timeouts, build failures
	exitErr  error
	duration time.Duration
}

func run(args []string) error {
	fl := flag.NewFlagSet("run", flag.ExitOnError)
	name := fl.String("group", "", "the group of "+groupsFile+" to run")
	title := fl.String("title", "", "the summary's title (default: the group, else the go test arguments)")
	fl.Parse(args)
	goArgs := fl.Args()
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if *name != "" {
		gs, err := readGroups(root)
		if err != nil {
			return err
		}
		sel, err := selectGroup(gs, *name)
		if err != nil {
			return err
		}
		goArgs = append(sel, goArgs...)
	}
	if *title == "" {
		*title = *name
		if *title == "" {
			*title = "go test " + strings.Join(goArgs, " ")
		}
	}
	fs, err := readFlaky(root)
	if err != nil {
		return err
	}

	first := goTest(goArgs)
	if first.exitErr == nil {
		summary(*title, first, nil, nil)
		return nil
	}
	leaves := failedLeaves(first.failed)
	var unlisted []string
	retry := map[string]bool{}
	for _, t := range leaves {
		if _, ok := flakyFor(fs, t); !ok {
			unlisted = append(unlisted, t)
		}
		top, _, _ := strings.Cut(t, "/")
		retry[top] = true
	}
	switch {
	case len(first.fatal) > 0:
		summary(*title, first, nil, nil)
		return fmt.Errorf("go test failed: %s", strings.Join(first.fatal, "; "))
	case len(leaves) == 0:
		summary(*title, first, nil, nil)
		return fmt.Errorf("go test failed: %v", first.exitErr)
	case len(unlisted) > 0:
		summary(*title, first, nil, nil)
		return fmt.Errorf("failed, not listed in %s: %s", flakyFile, strings.Join(unlisted, ", "))
	}
	names := make([]string, 0, len(retry))
	for t := range retry {
		names = append(names, t)
	}
	sort.Strings(names)
	runRe := "^(" + strings.Join(names, "|") + ")$"
	fmt.Printf("\n::group::retrying the known-flaky %s\n", strings.Join(names, ", "))
	// The last -run wins; -skip= undoes a group's skip.
	again := goTest(append(slices.Clone(goArgs), "-run", runRe, "-skip=", "-count=1"))
	fmt.Println("::endgroup::")
	summary(*title, first, leaves, &again)
	for _, t := range leaves {
		f, _ := flakyFor(fs, t)
		state := "passed on the retry"
		if again.failed[t] || again.exitErr != nil {
			state = "failed again"
		}
		fmt.Printf("::warning title=flaky test %s::%s %s (%s): %s\n", t, t, state, f.issue, firstMessage(first.output[t]))
	}
	if again.exitErr != nil {
		return fmt.Errorf("the known-flaky %s failed again", strings.Join(names, ", "))
	}
	return nil
}

// selectGroup is the go test flags running the group's tests.
func selectGroup(gs []group, name string) ([]string, error) {
	for _, g := range gs {
		if g.name != name {
			continue
		}
		if g.name != restGroup {
			return []string{"-run", g.pattern}, nil
		}
		var others []string
		for _, o := range gs {
			if o.name != restGroup {
				others = append(others, o.pattern)
			}
		}
		return []string{"-skip", strings.Join(others, "|")}, nil
	}
	return nil, fmt.Errorf("%s has no group %s", groupsFile, name)
}

// failedLeaves is the failed tests none of whose subtests failed: a
// parent fails with its subtest.
func failedLeaves(failed map[string]bool) []string {
	var ls []string
	for t := range failed {
		leaf := true
		for o := range failed {
			if strings.HasPrefix(o, t+"/") {
				leaf = false
				break
			}
		}
		if leaf {
			ls = append(ls, t)
		}
	}
	sort.Strings(ls)
	return ls
}

// goTest runs go test -json with args, printing what go test without -v
// would: the output of failed tests, a line per passed top-level test, and
// the packages' results.
func goTest(args []string) result {
	r := result{failed: map[string]bool{}, output: map[string][]string{}, elapsed: map[string]float64{}}
	start := time.Now()
	cmd := exec.Command("go", append([]string{"test", "-json"}, args...)...)
	fmt.Println("$ go test " + quoteArgs(args))
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		r.exitErr = err
		return r
	}
	if err := cmd.Start(); err != nil {
		r.exitErr = err
		r.fatal = append(r.fatal, err.Error())
		return r
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			fmt.Println(sc.Text())
			continue
		}
		top, _, _ := strings.Cut(e.Test, "/")
		switch e.Action {
		case "output":
			// A panic or a timeout ends the test binary: the tests after
			// it never ran.
			if strings.HasPrefix(e.Output, "panic: ") || strings.HasPrefix(e.Output, "fatal error: ") {
				r.fatal = append(r.fatal, firstLine(e.Output))
			}
			if e.Test == "" {
				fmt.Print(e.Output)
				continue
			}
			r.output[e.Test] = append(r.output[e.Test], e.Output)
			if e.Test != top {
				r.output[top] = append(r.output[top], e.Output)
			}
		case "fail":
			if e.FailedBuild != "" {
				r.fatal = append(r.fatal, e.FailedBuild+" doesn't build")
			}
			if e.Test != "" {
				r.failed[e.Test] = true
			}
			if e.Test == top && e.Test != "" {
				r.elapsed[top] = e.Elapsed
				fmt.Print(strings.Join(r.output[top], ""))
			}
		case "pass", "skip":
			if e.Test == top && e.Test != "" {
				r.elapsed[top] = e.Elapsed
				if e.Action == "pass" {
					fmt.Printf("--- PASS: %s (%.2fs)\n", top, e.Elapsed)
				}
			}
		}
	}
	r.exitErr = cmd.Wait()
	if r.exitErr != nil && strings.TrimSpace(stderr.String()) != "" && len(r.failed) == 0 {
		r.fatal = append(r.fatal, firstLine(stderr.String()))
	}
	r.duration = time.Since(start)
	return r
}

// firstMessage is a failed test's first line of its own: its first log or
// error, without go test's === and --- lines.
func firstMessage(out []string) string {
	for _, l := range out {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "=== ") || strings.HasPrefix(t, "--- ") {
			continue
		}
		if len(t) > 300 {
			t = t[:300] + "…"
		}
		return t
	}
	return "(no output)"
}

// summary writes the run's results to $GITHUB_STEP_SUMMARY: the counts and
// slowest tests, and the retried tests with the first failure's message.
func summary(title string, first result, retried []string, again *result) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", title)
	failed := 0
	for t := range first.elapsed {
		if first.failed[t] {
			failed++
		}
	}
	fmt.Fprintf(&b, "%d top-level tests, %d failed, in %s.\n\n", len(first.elapsed), failed, first.duration.Round(time.Second))
	if len(retried) > 0 {
		fs, _ := readFlakyHere()
		b.WriteString("**Known-flaky tests retried** (`" + flakyFile + "`):\n\n| test | first failure | issue | retry |\n|---|---|---|---|\n")
		for _, t := range retried {
			f, _ := flakyFor(fs, t)
			state := "passed"
			if again.failed[t] || again.exitErr != nil {
				state = "**failed**"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", t, mdCell(firstMessage(first.output[t])), f.issue, state)
		}
		b.WriteString("\n")
		for _, t := range retried {
			fmt.Fprintf(&b, "<details><summary><code>%s</code>: the first run's output</summary>\n\n```\n%s```\n</details>\n\n", t, tail(first.output[t], 60))
		}
	}
	type dur struct {
		t string
		s float64
	}
	var ds []dur
	for t, s := range first.elapsed {
		ds = append(ds, dur{t, s})
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].s > ds[j].s })
	if len(ds) > 10 {
		ds = ds[:10]
	}
	if len(ds) > 0 {
		b.WriteString("<details><summary>The slowest tests</summary>\n\n| test | seconds |\n|---|---|\n")
		for _, d := range ds {
			fmt.Fprintf(&b, "| `%s` | %.1f |\n", d.t, d.s)
		}
		b.WriteString("</details>\n\n")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "citest: summary:", err)
		return
	}
	defer f.Close()
	f.WriteString(b.String())
}

func readFlakyHere() ([]flaky, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	return readFlaky(root)
}

func tail(out []string, n int) string {
	s := strings.Join(out, "")
	ls := strings.SplitAfter(s, "\n")
	if len(ls) > n {
		ls = append([]string{"…\n"}, ls[len(ls)-n:]...)
	}
	return strings.ReplaceAll(strings.Join(ls, ""), "```", "'''")
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "`", "'")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func quoteArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " |()*^$?[]'\"\\") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		q[i] = a
	}
	return strings.Join(q, " ")
}
