package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ofelcan164/muster/internal/model"
)

// mockupSnapshot is the session docs/workspace-tiles.html renders by hand:
// seven workspaces, nine tiles. rollout has three agents in three different
// repos, api-bugs and spike share the api and web repos rollout uses, and
// notes is a scratch workspace with no repository at all.
func mockupSnapshot() *model.Snapshot {
	now := time.Now()
	a := func(pane, ws, name string, st model.Status) model.Agent {
		return model.Agent{
			PaneID: pane, WorkspaceID: ws, Name: name, Status: st,
			StatusSince: now.Add(-4 * time.Minute), AgeKnown: true,
		}
	}
	p := func(id, ws, label string) model.Pane {
		return model.Pane{PaneID: id, WorkspaceID: ws, Label: label}
	}

	return &model.Snapshot{
		GeneratedAt:      now,
		FocusedWorkspace: "w6",
		Workspaces: []model.Workspace{
			{ID: "w1", Number: 1, Label: "rollout"},
			{ID: "w2", Number: 2, Label: "mobile"},
			{ID: "w3", Number: 3, Label: "api-bugs"},
			{ID: "w4", Number: 4, Label: "web"},
			{ID: "w5", Number: 5, Label: "notes"},
			{ID: "w6", Number: 6, Label: "hub"},
			{ID: "w7", Number: 7, Label: "spike"},
		},
		Repos: []model.Repo{
			{
				Key: "acme/contracts", Name: "acme/contracts", Display: "contracts",
				Branch: "schema-v4", Sigil: "✦", GridSlot: 0, IsGit: true,
				WorkspaceIDs: []string{"w1"},
				Agents:       []model.Agent{a("w1:p1", "w1", "claude", model.StatusWorking)},
			},
			{
				Key: "acme/api", Name: "acme/api", Display: "api",
				Branch: "main", Sigil: "◆", GridSlot: 1, IsGit: true,
				WorkspaceIDs: []string{"w1", "w3", "w7"},
				Agents: []model.Agent{
					a("w1:p2", "w1", "claude", model.StatusBlocked),
					a("w3:p1", "w3", "reviewer", model.StatusDone),
				},
				OtherPanes: []model.Pane{p("w1:p4", "w1", "zsh"), p("w7:p1", "w7", "nvim")},
			},
			{
				Key: "acme/web", Name: "acme/web", Display: "web",
				Branch: "feat/retry", Sigil: "▣", GridSlot: 2, IsGit: true,
				WorkspaceIDs: []string{"w1", "w4", "w7"},
				Agents:       []model.Agent{a("w1:p3", "w1", "codex", model.StatusIdle)},
				OtherPanes:   []model.Pane{p("w4:p1", "w4", "nvim"), p("w4:p2", "w4", "zsh"), p("w7:p2", "w7", "zsh")},
			},
			{
				Key: "acme/mobile", Name: "acme/mobile", Display: "mobile",
				Branch: "main", Sigil: "⬢", GridSlot: 3, IsGit: true,
				WorkspaceIDs: []string{"w2"},
				Agents:       []model.Agent{a("w2:p1", "w2", "claude", model.StatusWorking)},
				OtherPanes:   []model.Pane{p("w2:p2", "w2", "tail")},
			},
			{
				Key: "acme/hub", Name: "acme/hub", Display: "hub",
				Branch: "main", Sigil: "⬡", GridSlot: 4, IsGit: true,
				WorkspaceIDs: []string{"w6"},
				Agents: []model.Agent{func() model.Agent {
					ag := a("w6:p1", "w6", "orchestrator", model.StatusIdle)
					ag.IsOrchestrator = true
					return ag
				}()},
			},
			{
				Key: "dir:notes", Name: "notes", Display: "notes",
				Sigil: "○", GridSlot: 5, IsGit: false, ColorIndex: -1,
				WorkspaceIDs: []string{"w5"},
				OtherPanes:   []model.Pane{p("w5:p1", "w5", "zsh")},
			},
		},
		Counts: model.Counts{Repos: 6, Workspaces: 7, Agents: 6},
	}
}

// One render of the mockup session, checked against docs/workspace-tiles.html:
// nine tiles, rollout's three agents each carrying their own repo, and the
// three empty workspaces each showing what their panes sit in.
func TestMockupSessionRendersNineTiles(t *testing.T) {
	m := New(mockupSnapshot(), "")
	m.sort = SortHerdr
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	out := plain(m.View())

	tiles := m.orderedTiles(m.visibleTiles())
	if len(tiles) != 9 {
		t.Fatalf("want 9 tiles, got %d: %v", len(tiles), labelsOf(tiles))
	}

	for _, want := range []string{
		"contracts", "claude", "api", "web", "codex", "mobile",
		"api-bugs", "reviewer", "hub", "orchestrator", "spike", "notes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mockup render is missing %q:\n%s", want, out)
		}
	}
	// The three empty workspaces show the panes sitting in them.
	for _, want := range []string{"nvim", "zsh", "tail"} {
		if !strings.Contains(out, want) {
			t.Errorf("mockup render dropped pane label %q:\n%s", want, out)
		}
	}
	// hub is where you opened Muster from.
	if !strings.Contains(out, "▸6") {
		t.Errorf("the focused workspace is not marked:\n%s", out)
	}
}
