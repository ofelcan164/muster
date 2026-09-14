package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/model"
)

func testNow() time.Time { return time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC) }

func healthySnapshot(now time.Time) *model.Snapshot {
	return &model.Snapshot{
		GeneratedAt: now.Add(-4 * time.Second),
		Repos: []model.Repo{{
			Key:     "acme/api",
			Display: "api",
			Agents: []model.Agent{{
				PaneID:      "w1:p1",
				WorkspaceID: "w1",
				Name:        "claude",
				Status:      model.StatusWorking,
			}},
		}},
		Workspaces: []model.Workspace{{ID: "w1", Number: 1, Label: "api"}},
	}
}

func healthyDeps() deps {
	return deps{
		Dir:            "/tmp/muster-test-state",
		Now:            testNow(),
		DaemonRunning:  true,
		DaemonPID:      1234,
		Exe:            exeOK,
		ExePath:        "/plugin/bin/musterd",
		Musterd:        "/plugin/bin/musterd",
		KeysLetter:     "m",
		KeysPresent:    true,
		SkillInstalled: true,
		Snapshot:       healthySnapshot(testNow()),
	}
}

func fixKinds(r report) string {
	var out []string
	for _, f := range r.fixes {
		out = append(out, map[fixKind]string{fixStop: "stop", fixStart: "start"}[f.kind])
	}
	return strings.Join(out, ",")
}

func hasBad(r report) bool {
	return exitFor(r) != 0
}

func TestCheckHealthy(t *testing.T) {
	rep := check(healthyDeps())
	if hasBad(rep) || len(rep.fixes) != 0 {
		t.Fatalf("healthy deps reported: %+v", rep)
	}
}

func TestCheckDaemonDown(t *testing.T) {
	d := healthyDeps()
	d.DaemonRunning = false
	d.DaemonPID = 0
	d.Snapshot = nil
	d.SnapshotErr = os.ErrNotExist
	d.SnapshotMissing = true
	if got := fixKinds(check(d)); got != "start" {
		t.Fatalf("want start, got %q", got)
	}
}

func TestCheckOrphanedDaemon(t *testing.T) {
	d := healthyDeps()
	d.Exe = exeDeleted
	rep := check(d)
	if got := fixKinds(rep); got != "stop,start" {
		t.Fatalf("want stop,start, got %q", got)
	}
	if !hasBad(rep) {
		t.Fatal("orphaned daemon is not reported")
	}
}

// A reinstall rebuilds the binary under the daemon, which restarts itself.
// Nothing to fix, and above all no state to touch.
func TestCheckReplacedDaemon(t *testing.T) {
	d := healthyDeps()
	d.Exe = exeReplaced
	rep := check(d)
	if hasBad(rep) || len(rep.fixes) != 0 {
		t.Fatalf("a rebuild in progress reads as broken: %+v", rep)
	}
}

// A daemon from another copy is never swapped for this copy's, even stale.
func TestCheckDaemonElsewhere(t *testing.T) {
	d := healthyDeps()
	d.Exe = exeElsewhere
	d.ExePath = "/src/muster/bin/musterd"
	d.Snapshot.GeneratedAt = testNow().Add(-90 * time.Second)
	rep := check(d)
	if !hasBad(rep) || len(rep.advice) == 0 {
		t.Fatalf("daemon elsewhere not reported with advice: %+v", rep)
	}
	if len(rep.fixes) != 0 {
		t.Fatalf("daemon elsewhere proposed fixes: %+v", rep.fixes)
	}
}

func TestCheckStaleSnapshot(t *testing.T) {
	d := healthyDeps()
	d.Snapshot.GeneratedAt = testNow().Add(-90 * time.Second)
	if got := fixKinds(check(d)); got != "stop,start" {
		t.Fatalf("want stop,start, got %q", got)
	}
}

// A daemon that has not written yet may have started a moment ago, so no
// restart: the log says the rest.
func TestCheckRunningWithoutSnapshot(t *testing.T) {
	d := healthyDeps()
	d.Snapshot = nil
	d.SnapshotErr = os.ErrNotExist
	d.SnapshotMissing = true
	rep := check(d)
	if len(rep.fixes) != 0 || len(rep.advice) == 0 {
		t.Fatalf("want advice and no fixes, got %+v", rep)
	}
}

func TestCheckOrphanAgentsFresh(t *testing.T) {
	d := healthyDeps()
	// Agents the snapshot's workspace list does not contain: counted but
	// undrawable. With a fresh snapshot and a healthy daemon no local fix
	// helps, so this must be advice, not a fix.
	d.Snapshot.Workspaces = nil
	rep := check(d)
	if !hasBad(rep) {
		t.Fatal("orphan agents are not reported")
	}
	if len(rep.fixes) != 0 || len(rep.advice) == 0 {
		t.Fatalf("fresh orphans want advice and no fixes, got %+v", rep)
	}
}

func TestCheckMissingKeys(t *testing.T) {
	d := healthyDeps()
	d.KeysPresent = false
	rep := check(d)
	if !hasBad(rep) {
		t.Fatal("missing keys are not reported")
	}
	if len(rep.fixes) != 0 || len(rep.advice) == 0 {
		t.Fatalf("keys live in the user's config: want advice and no fixes, got %+v", rep)
	}
}

func TestClassify(t *testing.T) {
	dir := t.TempDir()
	here := filepath.Join(dir, "musterd")
	if err := os.WriteFile(here, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "gone", "musterd")
	for _, c := range []struct {
		path    string
		deleted bool
		musterd string
		want    string
	}{
		{here, false, here, exeOK},
		{here, true, here, exeReplaced}, // rebuilt in place
		{gone, true, here, exeDeleted},  // checkout removed
		{"/src/bin/musterd", false, here, exeElsewhere},
		{"/src/bin/musterd", false, "", exeOK}, // nothing beside this muster to compare
	} {
		if got := classify(c.path, c.deleted, c.musterd); got != c.want {
			t.Errorf("classify(%q, %v, %q) = %s, want %s", c.path, c.deleted, c.musterd, got, c.want)
		}
	}
}

func TestConfirm(t *testing.T) {
	for _, yes := range []string{"y\n", "Y\n", "yes\n", " Yes \n"} {
		if !confirm(strings.NewReader(yes)) {
			t.Errorf("%q reads as no", yes)
		}
	}
	for _, no := range []string{"n\n", "no\n", "\n", "", "yesterday\n"} {
		if confirm(strings.NewReader(no)) {
			t.Errorf("%q reads as yes", no)
		}
	}
	if confirm(nil) {
		t.Error("nil stdin reads as yes")
	}
}

// stubGather replays canned deps per call, repeating the last.
func stubGather(seq ...deps) func(string) deps {
	calls := 0
	return func(string) deps {
		defer func() { calls++ }()
		return seq[min(calls, len(seq)-1)]
	}
}

func logRunner(calls *[]string) runner {
	return runner{
		stop:  func(time.Duration) error { *calls = append(*calls, "stop"); return nil },
		start: func() error { *calls = append(*calls, "start"); return nil },
	}
}

func noWait(time.Time) {}

func TestRunOrphanedYes(t *testing.T) {
	broken := healthyDeps()
	broken.Exe = exeDeleted
	var calls []string

	var out strings.Builder
	code := runWith(&out, strings.NewReader("y\n"),
		Options{Dir: "/tmp/muster-test-state", CanPrompt: true},
		stubGather(broken, healthyDeps()), logRunner(&calls), noWait)
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out.String())
	}
	if got := strings.Join(calls, ","); got != "stop,start" {
		t.Fatalf("want stop,start, got %s", got)
	}
	if !strings.Contains(out.String(), "apply these fixes?") {
		t.Fatalf("no confirmation prompt in:\n%s", out.String())
	}
}

func TestRunDeclined(t *testing.T) {
	broken := healthyDeps()
	broken.Exe = exeDeleted
	var calls []string

	var out strings.Builder
	code := runWith(&out, strings.NewReader("n\n"),
		Options{Dir: "/tmp/muster-test-state", CanPrompt: true},
		stubGather(broken), logRunner(&calls), noWait)
	if code == 0 {
		t.Fatal("declined fixes exit 0")
	}
	if len(calls) != 0 {
		t.Fatalf("declined but applied %v", calls)
	}
	if !strings.Contains(out.String(), "no changes made") {
		t.Fatalf("no refusal note in:\n%s", out.String())
	}
}

func TestRunNonTerminal(t *testing.T) {
	broken := healthyDeps()
	broken.DaemonRunning = false
	var calls []string

	var out strings.Builder
	code := runWith(&out, nil, Options{Dir: "/tmp/muster-test-state"},
		stubGather(broken), logRunner(&calls), noWait)
	if code == 0 {
		t.Fatal("unappliable fixes exit 0")
	}
	if len(calls) != 0 {
		t.Fatalf("applied without a terminal: %v", calls)
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Fatalf("no --yes hint in:\n%s", out.String())
	}
}

func TestRunNoStateDir(t *testing.T) {
	var out strings.Builder
	if code := runWith(&out, nil, Options{}, nil, runner{}, noWait); code == 0 {
		t.Fatal("missing state dir exits 0")
	}
}
