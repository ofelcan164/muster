package herdr

// Record shapes below mirror herdr's success_response schema. They cover more
// than Muster reads: the shape of a record is documentation of the API, and the
// fields nobody reads yet are the ones that say what else is on offer. herdr
// omits empty maps and null objects, so every optional field has to tolerate
// being absent.

type Scroll struct {
	ViewportRows int `json:"viewport_rows"`
}

type Pane struct {
	PaneID                string            `json:"pane_id"`
	WorkspaceID           string            `json:"workspace_id"`
	TabID                 string            `json:"tab_id"`
	Agent                 string            `json:"agent"`
	AgentStatus           string            `json:"agent_status"`
	Cwd                   string            `json:"cwd"`
	ForegroundCwd         string            `json:"foreground_cwd"`
	Label                 string            `json:"label"`
	Title                 string            `json:"title"`
	TerminalTitle         string            `json:"terminal_title"`
	TerminalTitleStripped string            `json:"terminal_title_stripped"`
	Focused               bool              `json:"focused"`
	Revision              uint64            `json:"revision"`
	Tokens                map[string]string `json:"tokens"`
	StateLabels           map[string]string `json:"state_labels"`
	Scroll                *Scroll           `json:"scroll"`
}

type Agent struct {
	PaneID                string            `json:"pane_id"`
	WorkspaceID           string            `json:"workspace_id"`
	TabID                 string            `json:"tab_id"`
	Agent                 string            `json:"agent"`
	AgentStatus           string            `json:"agent_status"`
	Name                  string            `json:"name"`
	Cwd                   string            `json:"cwd"`
	ForegroundCwd         string            `json:"foreground_cwd"`
	Title                 string            `json:"title"`
	TerminalTitle         string            `json:"terminal_title"`
	TerminalTitleStripped string            `json:"terminal_title_stripped"`
	Focused               bool              `json:"focused"`
	Revision              uint64            `json:"revision"`
	StateChangeSeq        uint64            `json:"state_change_seq"`
	Tokens                map[string]string `json:"tokens"`
	StateLabels           map[string]string `json:"state_labels"`
}

// WorkspaceWorktree is herdr's own git view of a workspace. When it is present
// Muster uses it instead of shelling out to git for the branch.
type WorkspaceWorktree struct {
	Path             string `json:"path"`
	Branch           string `json:"branch"`
	Label            string `json:"label"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
	IsDetached       bool   `json:"is_detached"`
	IsBare           bool   `json:"is_bare"`
	OpenWorkspaceID  string `json:"open_workspace_id"`
}

type Workspace struct {
	WorkspaceID string             `json:"workspace_id"`
	Label       string             `json:"label"`
	Number      int                `json:"number"`
	ActiveTabID string             `json:"active_tab_id"`
	AgentStatus string             `json:"agent_status"`
	Focused     bool               `json:"focused"`
	PaneCount   int                `json:"pane_count"`
	TabCount    int                `json:"tab_count"`
	Tokens      map[string]string  `json:"tokens"`
	Worktree    *WorkspaceWorktree `json:"worktree"`
}

type Tab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number"`
	PaneCount   int    `json:"pane_count"`
}

type Snapshot struct {
	Version            string      `json:"version"`
	Protocol           int         `json:"protocol"`
	FocusedPaneID      string      `json:"focused_pane_id"`
	FocusedTabID       string      `json:"focused_tab_id"`
	FocusedWorkspaceID string      `json:"focused_workspace_id"`
	Workspaces         []Workspace `json:"workspaces"`
	Tabs               []Tab       `json:"tabs"`
	Panes              []Pane      `json:"panes"`
	Agents             []Agent     `json:"agents"`
}

// SessionSnapshot is the daemon's authoritative read of the world.
//
// The event stream is only ever a hint that something changed, never a log:
// 0.8.2 replayed closed panes on every subscribe, and 0.9.0 promises nothing
// about ordering either, so state is always rebuilt from here.
func (c *Client) SessionSnapshot() (*Snapshot, error) {
	var out struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.Call("session.snapshot", struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out.Snapshot, nil
}

// PaneRead returns recent pane content. Per herdr's documented behaviour a CLI
// or API read does not mark an agent as seen, so the daemon can scan output
// continuously without consuming the unseen-work signal that "done" depends on.
func (c *Client) PaneRead(paneID, source string, lines int) (string, error) {
	params := map[string]any{
		"pane_id":    paneID,
		"source":     source,
		"strip_ansi": true,
	}
	if lines > 0 {
		params["lines"] = lines
	}
	var out struct {
		Read struct {
			PaneID    string `json:"pane_id"`
			Text      string `json:"text"`
			Revision  uint64 `json:"revision"`
			Truncated bool   `json:"truncated"`
		} `json:"read"`
	}
	if err := c.Call("pane.read", params, &out); err != nil {
		return "", err
	}
	return out.Read.Text, nil
}

// PaneForegroundProcess returns the name of the process running in a pane, or
// "" when the pane has none. 0.95 to 4ms per call, measured 2026-09-10 against
// a read buffer that has since shrunk to 16KiB.
//
// herdr reports the whole foreground process group; the last entry is the
// innermost process, which is the one worth showing. A pane sitting at a prompt
// reports its shell, which is how a stopped dev server becomes visible.
func (c *Client) PaneForegroundProcess(paneID string) (string, error) {
	var out struct {
		ProcessInfo struct {
			ForegroundProcesses []struct {
				Name    string `json:"name"`
				Cmdline string `json:"cmdline"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if err := c.Call("pane.process_info", map[string]any{"pane_id": paneID}, &out); err != nil {
		return "", err
	}
	procs := out.ProcessInfo.ForegroundProcesses
	if len(procs) == 0 {
		return "", nil
	}
	return procs[len(procs)-1].Name, nil
}

// PluginSource is where herdr installed a plugin from. Kind is "github" or
// "local", and a local one carries nothing else.
type PluginSource struct {
	Kind           string `json:"kind"`
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	RequestedRef   string `json:"requested_ref"`
	ResolvedCommit string `json:"resolved_commit"`
	ManagedPath    string `json:"managed_path"`
}

type Plugin struct {
	PluginID   string       `json:"plugin_id"`
	Name       string       `json:"name"`
	Version    string       `json:"version"`
	PluginRoot string       `json:"plugin_root"`
	Enabled    bool         `json:"enabled"`
	Source     PluginSource `json:"source"`
}

// Plugins lists every installed plugin, the same record `herdr plugin list
// --json` prints.
func (c *Client) Plugins() ([]Plugin, error) {
	var out struct {
		Plugins []Plugin `json:"plugins"`
	}
	if err := c.Call("plugin.list", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out.Plugins, nil
}
