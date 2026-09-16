package daemon

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ofelcan164/muster/internal/chain"
	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

// ReadSnapshot loads the snapshot the daemon maintains. This is the whole of
// what the overlay does at open time, and it is one read and one decode.
func ReadSnapshot() (*model.Snapshot, error) {
	b, err := os.ReadFile(state.SnapshotPath())
	if err != nil {
		return nil, err
	}
	var s model.Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

var statusIcon = map[model.Status]string{
	model.StatusBlocked: "▲",
	model.StatusDone:    "●",
	model.StatusWorking: "◐",
	model.StatusIdle:    "○",
	model.StatusUnknown: "◌",
}

func icon(s model.Status) string {
	if i, ok := statusIcon[s]; ok {
		return i
	}
	return "◌"
}

// Dump renders the snapshot as text. This is the proving ground for the data
// model: if this output is not right, no amount of styling in the overlay will
// save it.
func Dump(w io.Writer, s *model.Snapshot, now time.Time) {
	age := now.Sub(s.GeneratedAt)
	fmt.Fprintf(w, "MUSTER  %s · %s · %s  %d need you\n",
		Plural(s.Counts.Repos, "repo"), Plural(s.Counts.Workspaces, "workspace"),
		Plural(s.Counts.Agents, "agent"), s.Counts.NeedsYou)
	fmt.Fprintf(w, "snapshot %s old · herdr %s · daemon pid %d\n",
		CompactDur(age), orDash(s.HerdrVersion), s.DaemonPID)
	fmt.Fprintln(w)

	// Ranked ribbon. The overlay drops it entirely when nothing needs you,
	// since an empty ribbon is a signal rather than a gap to fill. Here it says
	// so out loud: a dump with a section missing reads as a dump that failed.
	if len(s.Attention) == 0 {
		fmt.Fprintln(w, "NEEDS YOU")
		fmt.Fprintln(w, "  (nothing)")
	} else {
		fmt.Fprintln(w, "NEEDS YOU")
		for i, a := range s.Attention {
			repo := repoName(s, a.RepoKey)
			fmt.Fprintf(w, "  %d %s %-10s %-14s %-8s %-5s %s\n",
				i+1, icon(a.Status), repo, a.Agent,
				strings.ToUpper(string(a.Status)), attnAge(a), a.Detail)
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "REPOS")
	if len(s.Repos) == 0 {
		fmt.Fprintln(w, "  (none discovered)")
	}
	for _, r := range s.Repos {
		header := fmt.Sprintf("  [%d] %s %s", r.GridSlot, r.Sigil, strings.ToUpper(r.Display))
		if r.Branch != "" {
			header += "  " + r.Branch
		}
		if !r.IsGit {
			header += "  (no repo)"
		}
		if r.IsWorktree {
			header += "  (worktree)"
		}
		fmt.Fprintln(w, header)
		fmt.Fprintf(w, "      %s\n", dimPath(r.Root))

		if len(r.Agents) == 0 {
			fmt.Fprintln(w, "      · no agents")
		}
		for _, a := range r.Agents {
			marker := " "
			if a.IsOrchestrator {
				marker = "⌂"
			}
			fmt.Fprintf(w, "     %s%s %-14s %-8s %-5s %s\n",
				marker, icon(a.Status), a.Name,
				strings.ToUpper(string(a.Status)), ageText(a, now),
				taskLine(a))
			if a.DependsOn != "" {
				fmt.Fprintf(w, "         depends on %s\n", edgeText(a, now))
			}
			if a.Note != "" {
				fmt.Fprintf(w, "         ✎ %s\n", a.Note)
			}
		}
		if len(r.OtherPanes) > 0 {
			labels := make([]string, 0, len(r.OtherPanes))
			for _, p := range r.OtherPanes {
				labels = append(labels, p.Label)
			}
			slices.Sort(labels)
			fmt.Fprintf(w, "      %s · %s\n", Plural(len(r.OtherPanes), "pane"), strings.Join(labels, " · "))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "ORCHESTRATOR")
	if !s.Orch.Found {
		fmt.Fprintln(w, "  none marked · run the mark-orchestrator action on its pane")
	} else {
		fmt.Fprintf(w, "  ⌂ %s · %s %s · via %s · %s\n",
			s.Orch.Name, s.Orch.Status, CompactDur(now.Sub(s.Orch.StatusSince)),
			s.Orch.DetectedBy, s.Orch.PaneID)
		if s.Orch.LastMessage != "" {
			fmt.Fprintf(w, "    told  \"%s\"\n", s.Orch.LastMessage)
		}
		if s.Orch.LastSaid != "" {
			fmt.Fprintf(w, "    said  \"%s\"\n", s.Orch.LastSaid)
		}
	}
}

// Show is the orchestrator's view of where work stands: the usual order, then
// one block per agent. A target narrows it to a pane id, a repo/agent, or every
// agent in a repo. Without one it shows every agent on either end of an edge,
// which is the question an orchestrator is usually asking.
func Show(w io.Writer, s *model.Snapshot, c *chain.Chain, target string, now time.Time) error {
	type ref struct {
		repo  model.Repo
		agent model.Agent
	}
	var all []ref
	neededBy := map[string][]ref{}
	for _, r := range s.Repos {
		for _, a := range r.Agents {
			all = append(all, ref{r, a})
			if a.DependsOnRepo != "" {
				neededBy[a.DependsOnRepo] = append(neededBy[a.DependsOnRepo], ref{r, a})
			}
		}
	}
	var picked []ref
	for _, x := range all {
		onEdge := x.agent.DependsOn != "" || len(neededBy[x.repo.Key]) > 0
		if target == "" && onEdge || target != "" && isTarget(target, x.repo, x.agent) {
			picked = append(picked, x)
		}
	}
	if target != "" && len(picked) == 0 {
		return fmt.Errorf("no agent matches %q: pass a pane id, repo/agent, or a repo", target)
	}

	order := "none recorded"
	if !c.Empty() {
		order = chain.Format(c.Stages)
		var meta []string
		if c.SetBy != "" {
			meta = append(meta, "set by "+c.SetBy)
		}
		if !c.SetAt.IsZero() {
			meta = append(meta, CompactDur(now.Sub(c.SetAt))+" ago")
		}
		if len(meta) > 0 {
			order += "  (" + strings.Join(meta, ", ") + ")"
		}
	}
	fmt.Fprintf(w, "usual order  %s\n", order)
	if len(picked) == 0 {
		fmt.Fprintln(w, "\nno agent depends on another repo")
		return nil
	}

	for _, x := range picked {
		a := x.agent
		fmt.Fprintf(w, "\n%s/%s  %s  %s %s\n", repoLabel(x.repo), a.Name, a.PaneID, a.Status, ageText(a, now))
		fmt.Fprintf(w, "  task        %s\n", taskLine(a))
		if a.DependsOn != "" {
			fmt.Fprintf(w, "  depends on  %s\n", edgeText(a, now))
			for _, r := range s.Repos {
				if a.DependsOnRepo == "" || r.Key != a.DependsOnRepo {
					continue
				}
				if len(r.Agents) == 0 {
					fmt.Fprintf(w, "              no agent open in %s\n", repoLabel(r))
				}
				for _, u := range r.Agents {
					fmt.Fprintf(w, "              %s/%s  %s  %s %s · %s\n",
						repoLabel(r), u.Name, u.PaneID, u.Status, ageText(u, now), taskLine(u))
				}
			}
		}
		if hs := neededBy[x.repo.Key]; len(hs) > 0 {
			parts := make([]string, len(hs))
			for i, h := range hs {
				parts[i] = fmt.Sprintf("%s/%s (%s %s)", repoLabel(h.repo), h.agent.Name, h.agent.Status, ageText(h.agent, now))
			}
			fmt.Fprintf(w, "  needed by   %s\n", strings.Join(parts, ", "))
		}
		for _, row := range s.Attention {
			if row.PaneID == a.PaneID {
				fmt.Fprintf(w, "  ribbon      %s · %s\n", row.Reason, row.Detail)
			}
		}
		if a.Note != "" {
			fmt.Fprintf(w, "  note        %s\n", a.Note)
		}
	}
	return nil
}

// isTarget matches what an orchestrator would type: a pane id, a repo by the
// name Muster shows or its full name, or repo/agent.
func isTarget(target string, r model.Repo, a model.Agent) bool {
	if target == a.PaneID {
		return true
	}
	for _, name := range []string{r.Display, r.Name} {
		if name != "" && (strings.EqualFold(target, name) || strings.EqualFold(target, name+"/"+a.Name)) {
			return true
		}
	}
	return false
}

// edgeText is what an agent depends on and whether it has landed.
func edgeText(a model.Agent, now time.Time) string {
	if a.LandedAt.IsZero() {
		return a.DependsOn + " · can't land yet"
	}
	return a.DependsOn + " · landed " + CompactDur(now.Sub(a.LandedAt)) + " ago"
}

func repoLabel(r model.Repo) string { return cmp.Or(r.Display, r.Name, r.Key) }

// taskLine shows the task and, when it came from a lower rung of the fallback
// ladder, says so. Showing doubt is better than showing false confidence.
func taskLine(a model.Agent) string {
	if a.Task == "" {
		return "(no task line)"
	}
	switch a.TaskSource {
	case model.TaskFromOrchestratorStale:
		return a.Task + "  [stale]"
	case model.TaskFromSelfReport:
		return a.Task + "  [self-reported]"
	case model.TaskFromTerminalTitle:
		return a.Task + "  [title]"
	default:
		return a.Task
	}
}

func repoName(s *model.Snapshot, key string) string {
	for _, r := range s.Repos {
		if r.Key == key {
			return r.Display
		}
	}
	return key
}

// Plural writes a count with its noun: 1 agent, 3 agents.
func Plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func dimPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	// The separator matters: a plain prefix test turns /home/ana2/x into ~2/x
	// for the user /home/ana.
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}

// ageText renders an agent's age, or a dash when the daemon has never watched
// this pane change status and so has no honest number to show.
func ageText(a model.Agent, now time.Time) string {
	if !a.AgeKnown {
		return "-"
	}
	return CompactDur(a.Age(now))
}

func attnAge(a model.Attention) string {
	if !a.AgeKnown {
		return "-"
	}
	return CompactDur(a.Age)
}

// CompactDur formats an age the way the design writes them: 4m, 22m, 3h.
func CompactDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
