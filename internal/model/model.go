// Package model defines the snapshot musterd writes and the overlay reads.
//
// The snapshot is the whole contract between daemon and client. It is written
// atomically and is small enough to read and decode in well under a
// millisecond, because the overlay must open in under 30ms and may do no work
// beyond reading this file.
package model

import "time"

// Status is what an agent is doing: herdr's agent_status, passed through
// unchanged.
type Status string

const (
	// StatusIdle is an agent at its prompt with nothing to do.
	StatusIdle Status = "idle"
	// StatusWorking is an agent in the middle of a turn.
	StatusWorking Status = "working"
	// StatusBlocked is an agent waiting on you, at a permission prompt or a
	// question. Agent.Question holds the prompt when the daemon could read it.
	StatusBlocked Status = "blocked"
	// StatusDone is an agent that finished work you have not looked at yet.
	StatusDone Status = "done"
	// StatusUnknown is herdr not being able to tell. Ribbon rows for a stopped
	// process carry it too, because that pane runs no agent.
	StatusUnknown Status = "unknown"
)

// TaskSource records which rung of the fallback ladder a task line came from,
// so the overlay can dim or annotate it rather than presenting every line with
// equal confidence.
type TaskSource string

// The tiers run from most trusted to least, and an agent gets the first one
// that has a line. Tier 2 is a tier 1 line written before the agent entered its
// current status, so it may no longer be true.
const (
	TaskFromOrchestrator      TaskSource = "orchestrator"       // tier 1
	TaskFromOrchestratorStale TaskSource = "orchestrator_stale" // tier 2
	TaskFromSelfReport        TaskSource = "self_report"        // tier 3
	TaskFromTerminalTitle     TaskSource = "terminal_title"     // tier 4
	TaskFromNone              TaskSource = "none"               // tier 5
)

// Agent is one pane herdr has detected an agent in, whichever agent that is.
// Each is a row on its repo's card.
type Agent struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`

	// Name is herdr's agent name when one has been set, else a stable label
	// derived from the pane.
	Name string `json:"name"`
	// PaneLabel is the name the pane was given with a rename, empty until then.
	// Only the tile's pane chip reads it.
	PaneLabel string `json:"pane_label,omitempty"`
	// Kind is herdr's own label for the agent, passed through unread. Nothing
	// in Muster branches on it, so an agent herdr has never seen before still
	// gets a card.
	Kind    string `json:"kind"`
	Status  Status `json:"status"`
	Focused bool   `json:"focused"`

	Task       string     `json:"task,omitempty"`
	TaskSource TaskSource `json:"task_source"`
	DependsOn  string     `json:"depends_on,omitempty"`

	// DependsOnRepo is the key of the repo DependsOn names: the text before any "#",
	// matched against the repos on screen. Empty when nothing matched, which
	// leaves DependsOn to be shown as written.
	DependsOnRepo string `json:"depends_on_repo,omitempty"`
	// LandedAt is when the daemon first saw a landed token equal to DependsOn,
	// which is the orchestrator saying the work it depends on reached main.
	// Zero until then.
	LandedAt time.Time `json:"landed_at,omitempty"`

	// Question is the prompt a blocked agent is waiting on, read from its pane
	// because herdr does not expose it anywhere else.
	Question string `json:"question,omitempty"`
	Note     string `json:"note,omitempty"`

	// StatusSince is when the daemon first observed the current status. herdr
	// exposes no transition timestamp, so this is measured by the daemon and
	// persists across daemon restarts via the state file.
	StatusSince    time.Time `json:"status_since"`
	StateChangeSeq uint64    `json:"state_change_seq"`

	// AgeKnown is false when the daemon found the agent already in this status
	// rather than watching it get there. herdr has no transition timestamp, so
	// an age measured from first sight is a lower bound, not a fact.
	AgeKnown bool `json:"age_known"`

	IsOrchestrator bool   `json:"is_orchestrator,omitempty"`
	LastMessage    string `json:"last_message,omitempty"`
}

// Age is how long the agent has held its current status. Only meaningful when
// AgeKnown is true.
func (a Agent) Age(now time.Time) time.Duration {
	if a.StatusSince.IsZero() {
		return 0
	}
	return now.Sub(a.StatusSince)
}

// Pane is a non-agent pane: dev servers, log tails, shells. Ignored for triage,
// shown as a one-line footer per repo, and grouped by WorkspaceID onto an
// empty workspace tile when its workspace holds no agent at all.
type Pane struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Command     string `json:"command,omitempty"`
}

// Workspace is one of herdr's workspaces, carried through so the overlay can
// draw a tile for one that holds no agent at all: a repo card alone has
// nowhere to put it.
type Workspace struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Label  string `json:"label"`
}

// Repo is one discovered repository. Identity is keyed on (repo, worktree) from
// the first commit so linked worktrees can grow their own rows later without a
// data model change.
type Repo struct {
	// Key is the stable identity: "owner/name" plus a worktree suffix when the
	// checkout is a linked worktree. Colour, sigil and grid slot all hash from
	// it, so a repo looks the same on every machine.
	Key  string `json:"key"`
	Name string `json:"name"`

	// Display is what the UI draws: the short name, widened to the full
	// owner/name only when another repo would otherwise share the label.
	Display string `json:"display"`

	Root         string `json:"root"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	IsWorktree   bool   `json:"is_worktree,omitempty"`

	// IsGit is false for a scratch workspace with no repository. It still gets
	// a card, keyed on its cwd, so its agents are never hidden.
	IsGit bool `json:"is_git"`

	ColorIndex int    `json:"color_index"`
	Sigil      string `json:"sigil"`
	GridSlot   int    `json:"grid_slot"`

	WorkspaceIDs []string `json:"workspace_ids"`
	Agents       []Agent  `json:"agents"`
	OtherPanes   []Pane   `json:"other_panes"`
}

// Reason is the triage rule that put a row in the ribbon.
type Reason string

const (
	// ReasonBlocked is rank 1, an agent waiting on you.
	ReasonBlocked Reason = "blocked"
	// ReasonLanded is rank 2. Work an agent depends on landed, the
	// orchestrator has ended a turn since it recorded that, and the dependent
	// agent has not moved.
	ReasonLanded Reason = "landed"
	// ReasonProcessStopped is rank 3, a non-agent pane whose process went away.
	ReasonProcessStopped Reason = "process_stopped"
	// ReasonDoneUnseen is rank 4, an agent that finished and you have not
	// looked at since.
	ReasonDoneUnseen Reason = "done_unseen"
	// ReasonIdleNeverDone is rank 5, an agent that did some work, never
	// reported done, and has been idle for at least ten minutes.
	ReasonIdleNeverDone Reason = "idle_never_done"
)

// Attention is one row of the ranked ribbon.
type Attention struct {
	Rank     int           `json:"rank"`
	Reason   Reason        `json:"reason"`
	RepoKey  string        `json:"repo_key"`
	PaneID   string        `json:"pane_id"`
	Agent    string        `json:"agent"`
	Status   Status        `json:"status"`
	Age      time.Duration `json:"age_ns"`
	AgeKnown bool          `json:"age_known"`
	// Detail is the human sentence: the blocking question, or what landed
	// with nobody moving on it.
	Detail string `json:"detail"`
	// Dependents lists every agent that still depends on the work that landed,
	// as repo/agent, for the landed rule.
	Dependents []string `json:"dependents,omitempty"`
}

// Stopped is a non-agent pane whose process went away.
type Stopped struct {
	PaneID  string `json:"pane_id"`
	RepoKey string `json:"repo_key"`
	Label   string `json:"label"`
	Process string `json:"process"`
}

// Orchestrator is the agent that hands work to the others. The overlay draws
// it in a strip of its own rather than in the grid. Found is false when the
// daemon found none, by token or by name, and the other fields are then empty.
type Orchestrator struct {
	Found  bool   `json:"found"`
	PaneID string `json:"pane_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Status Status `json:"status,omitempty"`
	// LastMessage is what the orchestrator was told to do, off the task ladder.
	// LastSaid is what it last said back, read from its pane. Both, because the
	// prompt alone cannot tell you whether it answered.
	LastMessage string `json:"last_message,omitempty"`
	LastSaid    string `json:"last_said,omitempty"`
	// SaidAt is when the daemon read LastSaid, not when the agent typed it.
	// The read fires as soon as the status settles, so the two are a second or
	// so apart, and a message with no age on it cannot be told from a stale one.
	SaidAt time.Time `json:"said_at,omitempty"`
	// DetectedBy is "token" or "name"; the token wins because it survives a
	// rename and can carry more than a string later.
	DetectedBy  string    `json:"detected_by,omitempty"`
	StatusSince time.Time `json:"status_since,omitempty"`
}

// StaleAfter is when a snapshot stops being trustworthy. The daemon rewrites
// every five seconds, so anything this old means it is not running.
const StaleAfter = 30 * time.Second

// Snapshot is the file at $HERDR_PLUGIN_STATE_DIR/snapshot.json.
type Snapshot struct {
	GeneratedAt  time.Time `json:"generated_at"`
	DaemonPID    int       `json:"daemon_pid"`
	HerdrVersion string    `json:"herdr_version,omitempty"`

	Repos      []Repo       `json:"repos"`
	Workspaces []Workspace  `json:"workspaces"`
	Attention  []Attention  `json:"attention"`
	Orch       Orchestrator `json:"orchestrator"`

	// FocusedWorkspace is herdr's FocusedWorkspaceID, straight through. The
	// grid marks this workspace's tiles so you can tell where you are without
	// leaving the overlay.
	FocusedWorkspace string `json:"focused_workspace,omitempty"`

	// FocusedPane is where you are now. PreviousAgent is the agent you were in
	// before it, which is what the back key returns you to. Both are tracked by
	// the daemon because herdr keeps no focus history of its own.
	FocusedPane   string `json:"focused_pane,omitempty"`
	PreviousAgent string `json:"previous_agent,omitempty"`

	// FocusHistory is the recently focused agents, most recent first. The back
	// key resolves against this rather than PreviousAgent alone, so pressing it
	// twice in quick succession still toggles even if the daemon has not
	// reconciled in between.
	FocusHistory []string `json:"focus_history,omitempty"`

	Counts Counts `json:"counts"`
}

// Counts are totals over the whole snapshot, so a reader does not have to walk
// Repos for them.
type Counts struct {
	Repos      int `json:"repos"`
	Workspaces int `json:"workspaces"`
	Agents     int `json:"agents"`
	// NeedsYou is the number of rows in Attention. The overlay draws at most
	// four of them.
	NeedsYou int `json:"needs_you"`
	// Working is the number of agents whose status is working.
	Working int `json:"working"`
	// NonAgents is the number of OtherPanes across every repo.
	NonAgents int `json:"non_agents"`
}
