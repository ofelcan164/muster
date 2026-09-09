package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ofelcan/muster/internal/model"
	"github.com/ofelcan/muster/internal/state"
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
	fmt.Fprintf(w, "MUSTER  %s · %s  %d need you\n",
		plural(s.Counts.Repos, "repo"), plural(s.Counts.Agents, "agent"), s.Counts.NeedsYou)
	fmt.Fprintf(w, "snapshot %s old · herdr %s · daemon pid %d\n",
		compactDur(age), orDash(s.HerdrVersion), s.DaemonPID)
	for _, warn := range s.Warnings {
		fmt.Fprintf(w, "  ! %s\n", warn)
	}
	fmt.Fprintln(w)

	// Ranked ribbon. Absent entirely when nothing needs you, which is the
	// point: an empty ribbon is a signal, not a gap to fill.
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
			if a.BlockedOn != "" {
				fmt.Fprintf(w, "         blocked on: %s\n", a.BlockedOn)
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
			sort.Strings(labels)
			fmt.Fprintf(w, "      %s · %s\n", plural(len(r.OtherPanes), "pane"), strings.Join(labels, " · "))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "ORCHESTRATOR")
	if !s.Orch.Found {
		fmt.Fprintln(w, "  none marked · run the mark-orchestrator action on its pane")
	} else {
		fmt.Fprintf(w, "  ⌂ %s · %s %s · via %s · %s\n",
			s.Orch.Name, s.Orch.Status, compactDur(now.Sub(s.Orch.StatusSince)),
			s.Orch.DetectedBy, s.Orch.PaneID)
		if s.Orch.LastMessage != "" {
			fmt.Fprintf(w, "    told  \"%s\"\n", s.Orch.LastMessage)
		}
		if s.Orch.LastSaid != "" {
			fmt.Fprintf(w, "    said  \"%s\"\n", s.Orch.LastSaid)
		}
	}
}

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

func plural(n int, noun string) string {
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
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// ageText renders an agent's age, or a dash when the daemon has never watched
// this pane change status and so has no honest number to show.
func ageText(a model.Agent, now time.Time) string {
	if !a.AgeKnown {
		return "-"
	}
	return compactDur(a.Age(now))
}

func attnAge(a model.Attention) string {
	if !a.AgeKnown {
		return "-"
	}
	return compactDur(a.Age)
}

// compactDur formats an age the way the design writes them: 4m, 22m, 3h.
func compactDur(d time.Duration) string {
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
