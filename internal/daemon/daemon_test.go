package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ofelcan/muster/internal/herdr"
	"github.com/ofelcan/muster/internal/model"
)

// newTestDaemon builds a daemon with no client. Everything under test here is
// pure transformation of a session snapshot, so the socket is never needed.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	return New(nil, nil)
}

func pane(id, ws, cwd string) herdr.Pane {
	return herdr.Pane{PaneID: id, WorkspaceID: ws, TabID: ws + ":t1", Cwd: cwd}
}

func agentPane(id, ws, cwd, status string) herdr.Agent {
	return herdr.Agent{
		PaneID: id, WorkspaceID: ws, TabID: ws + ":t1",
		Cwd: cwd, Agent: "claude", AgentStatus: status,
	}
}

// gitRepo creates a real repository on disk, because discovery reads .git
// rather than asking git, and a fake directory would not exercise it.
func gitRepo(t *testing.T, dir, origin, branch string) string {
	t.Helper()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(git, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("HEAD", "ref: refs/heads/"+branch+"\n")
	cfg := "[core]\n\trepositoryformatversion = 0\n"
	if origin != "" {
		cfg += "[remote \"origin\"]\n\turl = " + origin + "\n"
	}
	write("config", cfg)
	return dir
}

func TestDiscoversReposFromWorkspaceCwds(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	api := gitRepo(t, filepath.Join(root, "api"), "git@github.com:acme/api.git", "feat/billing")
	web := gitRepo(t, filepath.Join(root, "web"), "https://github.com/acme/web.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{
			{WorkspaceID: "w1", Number: 1},
			{WorkspaceID: "w2", Number: 2},
		},
		Panes: []herdr.Pane{pane("w1:p1", "w1", api), pane("w2:p1", "w2", web)},
		Agents: []herdr.Agent{
			agentPane("w1:p1", "w1", api, "working"),
			agentPane("w2:p1", "w2", web, "idle"),
		},
	}

	agents := d.buildAgents(snap, time.Now())
	repos := d.buildRepos(snap, agents, model.Orchestrator{})

	if len(repos) != 2 {
		t.Fatalf("want 2 repos, got %d", len(repos))
	}
	byKey := map[string]model.Repo{}
	for _, r := range repos {
		byKey[r.Key] = r
	}
	if r, ok := byKey["acme/api"]; !ok {
		t.Errorf("acme/api not discovered, got keys %v", keysOf(byKey))
	} else {
		if r.Branch != "feat/billing" {
			t.Errorf("branch = %q, want feat/billing", r.Branch)
		}
		if !r.IsGit {
			t.Error("expected IsGit")
		}
		if len(r.Agents) != 1 {
			t.Errorf("want 1 agent, got %d", len(r.Agents))
		}
	}
	if _, ok := byKey["acme/web"]; !ok {
		t.Errorf("acme/web not discovered, got keys %v", keysOf(byKey))
	}
}

// Two workspaces open on one repository are one card, not two.
func TestTwoWorkspacesOneRepo(t *testing.T) {
	d := newTestDaemon(t)
	api := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}, {WorkspaceID: "w2", Number: 2}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", api), pane("w2:p1", "w2", api)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", api, "idle"), agentPane("w2:p1", "w2", api, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if len(repos) != 1 {
		t.Fatalf("want 1 repo, got %d", len(repos))
	}
	if len(repos[0].Agents) != 2 {
		t.Errorf("want both agents on the one card, got %d", len(repos[0].Agents))
	}
	if len(repos[0].WorkspaceIDs) != 2 {
		t.Errorf("want 2 workspace ids, got %v", repos[0].WorkspaceIDs)
	}
}

// Same basename, different org. These must not merge.
func TestSameBasenameDifferentOrg(t *testing.T) {
	d := newTestDaemon(t)
	root := t.TempDir()
	a := gitRepo(t, filepath.Join(root, "web-a"), "git@github.com:acme/web.git", "main")
	b := gitRepo(t, filepath.Join(root, "web-b"), "git@github.com:other/web.git", "main")

	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}, {WorkspaceID: "w2", Number: 2}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", a), pane("w2:p1", "w2", b)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", a, "idle"), agentPane("w2:p1", "w2", b, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if len(repos) != 2 {
		t.Fatalf("want 2 distinct repos, got %d", len(repos))
	}
	// Colliding short names must both widen to the full owner/name.
	for _, r := range repos {
		if r.Display != r.Name {
			t.Errorf("colliding repo %q should display its owner, got %q", r.Name, r.Display)
		}
	}
	if repos[0].Sigil == repos[1].Sigil {
		t.Error("distinct repos must get distinct sigils")
	}
}

func TestDisplayNameDropsOwnerWhenUnique(t *testing.T) {
	d := newTestDaemon(t)
	api := gitRepo(t, filepath.Join(t.TempDir(), "api"), "git@github.com:acme/api.git", "main")
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", api)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", api, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if repos[0].Display != "api" {
		t.Errorf("Display = %q, want api", repos[0].Display)
	}
	if repos[0].Key != "acme/api" {
		t.Errorf("Key must stay the full owner/name, got %q", repos[0].Key)
	}
}

// A repo with no origin falls back to its directory name.
func TestRepoWithoutOrigin(t *testing.T) {
	d := newTestDaemon(t)
	infra := gitRepo(t, filepath.Join(t.TempDir(), "infra"), "", "main")
	snap := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", infra)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", infra, "idle")},
	}
	repos := d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{})
	if len(repos) != 1 || repos[0].Name != "infra" {
		t.Fatalf("want a repo named infra, got %+v", repos)
	}
}

// A scratch workspace earns a card only while an agent is running in it.
func TestScratchWorkspaceHiddenUntilItHasAgents(t *testing.T) {
	d := newTestDaemon(t)
	scratch := t.TempDir()

	empty := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", scratch)},
	}
	if repos := d.buildRepos(empty, map[string]model.Agent{}, model.Orchestrator{}); len(repos) != 0 {
		t.Fatalf("empty scratch workspace should not take a grid cell, got %+v", repos)
	}

	busy := &herdr.Snapshot{
		Workspaces: []herdr.Workspace{{WorkspaceID: "w1", Number: 1}},
		Panes:      []herdr.Pane{pane("w1:p1", "w1", scratch)},
		Agents:     []herdr.Agent{agentPane("w1:p1", "w1", scratch, "working")},
	}
	repos := d.buildRepos(busy, d.buildAgents(busy, time.Now()), model.Orchestrator{})
	if len(repos) != 1 {
		t.Fatalf("scratch workspace with an agent should appear, got %d", len(repos))
	}
	if repos[0].IsGit {
		t.Error("scratch workspace should not be marked as git")
	}
}

// Grid slots are the whole reason the grid is worth having. They must not move.
func TestGridSlotsAreStableAndDeterministic(t *testing.T) {
	root := t.TempDir()
	a := gitRepo(t, filepath.Join(root, "a"), "git@github.com:acme/a.git", "main")
	b := gitRepo(t, filepath.Join(root, "b"), "git@github.com:acme/b.git", "main")
	c := gitRepo(t, filepath.Join(root, "c"), "git@github.com:acme/c.git", "main")

	build := func(d *Daemon) map[string]int {
		snap := &herdr.Snapshot{
			Workspaces: []herdr.Workspace{
				{WorkspaceID: "w3", Number: 3},
				{WorkspaceID: "w1", Number: 1},
				{WorkspaceID: "w2", Number: 2},
			},
			Panes: []herdr.Pane{pane("w1:p1", "w1", a), pane("w2:p1", "w2", b), pane("w3:p1", "w3", c)},
			Agents: []herdr.Agent{
				agentPane("w1:p1", "w1", a, "idle"),
				agentPane("w2:p1", "w2", b, "idle"),
				agentPane("w3:p1", "w3", c, "idle"),
			},
		}
		got := map[string]int{}
		for _, r := range d.buildRepos(snap, d.buildAgents(snap, time.Now()), model.Orchestrator{}) {
			got[r.Key] = r.GridSlot
		}
		return got
	}

	d := newTestDaemon(t)
	first := build(d)
	// Slots are allocated in workspace-number order, not map order, so the same
	// session always lays out the same way.
	if first["acme/a"] != 0 || first["acme/b"] != 1 || first["acme/c"] != 2 {
		t.Fatalf("slots not in workspace order: %v", first)
	}
	for i := 0; i < 20; i++ {
		if got := build(d); got["acme/a"] != first["acme/a"] ||
			got["acme/b"] != first["acme/b"] || got["acme/c"] != first["acme/c"] {
			t.Fatalf("slots moved between reconciles: %v then %v", first, got)
		}
	}
}

func TestDominantCwdWinsOverAStrayShell(t *testing.T) {
	panes := []herdr.Pane{
		pane("w1:p1", "w1", "/repo"),
		pane("w1:p2", "w1", "/repo"),
		pane("w1:p3", "w1", "/tmp"),
		pane("w2:p1", "w2", "/elsewhere"),
	}
	if got := dominantCwd(panes, "w1"); got != "/repo" {
		t.Errorf("dominantCwd = %q, want /repo", got)
	}
}

func TestDominantCwdTieIsDeterministic(t *testing.T) {
	panes := []herdr.Pane{pane("w1:p1", "w1", "/b"), pane("w1:p2", "w1", "/a")}
	first := dominantCwd(panes, "w1")
	for i := 0; i < 50; i++ {
		if got := dominantCwd(panes, "w1"); got != first {
			t.Fatalf("tie break not deterministic: %q then %q", first, got)
		}
	}
}

func TestAgentNamesAreUnambiguous(t *testing.T) {
	// Two agents in different workspaces are both "p1". The fallback must not
	// collapse them into the same label.
	a := agentName(herdr.Agent{PaneID: "w5:p1"})
	b := agentName(herdr.Agent{PaneID: "w6:p1"})
	if a == b {
		t.Fatalf("distinct panes produced the same name %q", a)
	}
	named := agentName(herdr.Agent{PaneID: "w5:p1", Name: "migrations"})
	if named != "migrations" {
		t.Errorf("an explicit name must win, got %q", named)
	}
}

func TestOrchestratorTokenBeatsName(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", Name: "orchestrator", AgentStatus: "idle"},
			{PaneID: "w2:p1", Name: "something-else", AgentStatus: "idle",
				Tokens: map[string]string{"role": "orchestrator"}},
		},
	}
	orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now())
	if !orch.Found {
		t.Fatal("expected an orchestrator")
	}
	if orch.PaneID != "w2:p1" {
		t.Errorf("token should win over name, got %s via %s", orch.PaneID, orch.DetectedBy)
	}
	if orch.DetectedBy != "token" {
		t.Errorf("DetectedBy = %q, want token", orch.DetectedBy)
	}
}

func TestOrchestratorFallsBackToName(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Agents: []herdr.Agent{{PaneID: "w1:p1", Name: "orchestrator", AgentStatus: "idle"}},
	}
	orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now())
	if !orch.Found || orch.DetectedBy != "name" {
		t.Fatalf("want name detection, got %+v", orch)
	}
}

func TestNoOrchestratorIsNotGuessed(t *testing.T) {
	d := newTestDaemon(t)
	snap := &herdr.Snapshot{
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", Name: "chatty", AgentStatus: "idle"},
			{PaneID: "w2:p1", Name: "also-chatty", AgentStatus: "working"},
		},
	}
	if orch := d.findOrchestrator(snap, d.buildAgents(snap, time.Now()), time.Now()); orch.Found {
		t.Fatalf("orchestrator must not be guessed, got %+v", orch)
	}
}

func keysOf(m map[string]model.Repo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
