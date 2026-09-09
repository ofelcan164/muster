// Package model defines the snapshot musterd writes and the overlay reads.
//
// The snapshot is the whole contract between daemon and client. It is written
// atomically and is small enough to read and decode in well under a
// millisecond, because the overlay must open in under 30ms and may do no work
// beyond reading this file.
package model

import "time"

// SchemaVersion is bumped whenever the snapshot shape changes so a stale client
// can tell it does not understand the file rather than misreading it.
const SchemaVersion = 1

type Status string

const (
	StatusIdle    Status = "idle"
	StatusWorking Status = "working"
	StatusBlocked Status = "blocked"
	StatusDone    Status = "done"
	StatusUnknown Status = "unknown"
)

// TaskSource records which rung of the fallback ladder a task line came from,
// so the overlay can dim or annotate it rather than presenting every line with
// equal confidence.
type TaskSource string

const (
	TaskFromOrchestrator      TaskSource = "orchestrator"       // tier 1
	TaskFromOrchestratorStale TaskSource = "orchestrator_stale" // tier 2
	TaskFromSelfReport        TaskSource = "self_report"        // tier 3
	TaskFromTerminalTitle     TaskSource = "terminal_title"     // tier 4
	TaskFromNone              TaskSource = "none"               // tier 5
)

type Agent struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`

	// Name is herdr's agent name when one has been set, else a stable label
	// derived from the pane.
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "claude", "codex", ...
	Status  Status `json:"status"`
	Focused bool   `json:"focused"`

	Task       string     `json:"task,omitempty"`
	TaskSource TaskSource `json:"task_source"`
	BlockedOn  string     `json:"blocked_on,omitempty"`

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
// shown as a one-line footer per repo.
type Pane struct {
	PaneID  string `json:"pane_id"`
	Label   string `json:"label"`
	Command string `json:"command,omitempty"`
	Alive   bool   `json:"alive"`
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
	Border     string `json:"border"`
	GridSlot   int    `json:"grid_slot"`

	WorkspaceIDs []string `json:"workspace_ids"`
	Agents       []Agent  `json:"agents"`
	OtherPanes   []Pane   `json:"other_panes"`
}

// Reason is the triage rule that put a row in the ribbon.
type Reason string

const (
	ReasonBlocked        Reason = "blocked"
	ReasonGateUntold     Reason = "gate_untold"
	ReasonProcessStopped Reason = "process_stopped"
	ReasonDoneUnseen     Reason = "done_unseen"
	ReasonIdleNeverDone  Reason = "idle_never_done"
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
	// Detail is the human sentence: the blocking question, or why a gate is
	// open with nobody moving through it.
	Detail string `json:"detail"`
	// Downstream lists agents waiting on this one, for the gate rule.
	Downstream []string `json:"downstream,omitempty"`
}

// Stopped is a non-agent pane whose process went away.
type Stopped struct {
	PaneID  string `json:"pane_id"`
	RepoKey string `json:"repo_key"`
	Label   string `json:"label"`
	Process string `json:"process"`
}

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
	// DetectedBy is "token" or "name"; the token wins because it survives a
	// rename and can carry more than a string later.
	DetectedBy  string    `json:"detected_by,omitempty"`
	StatusSince time.Time `json:"status_since,omitempty"`
}

// Snapshot is the file at $HERDR_PLUGIN_STATE_DIR/snapshot.json.
type Snapshot struct {
	Schema       int       `json:"schema"`
	GeneratedAt  time.Time `json:"generated_at"`
	DaemonPID    int       `json:"daemon_pid"`
	HerdrVersion string    `json:"herdr_version,omitempty"`

	Repos     []Repo       `json:"repos"`
	Attention []Attention  `json:"attention"`
	Orch      Orchestrator `json:"orchestrator"`

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

	// Warnings are daemon-side problems worth surfacing in the overlay title
	// bar, such as a degraded subscription.
	Warnings []string `json:"warnings,omitempty"`

	Counts Counts `json:"counts"`
}

type Counts struct {
	Repos     int `json:"repos"`
	Agents    int `json:"agents"`
	NeedsYou  int `json:"needs_you"`
	Working   int `json:"working"`
	NonAgents int `json:"non_agents"`
}
