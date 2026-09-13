package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/model"
	"github.com/ofelcan164/muster/internal/state"
)

// BenchmarkReadSnapshot measures the whole of what the overlay does at open
// time, so the README's timing claims can be re-run rather than believed. The
// two sizes are the ones it quotes: a session like the one it was written on,
// and a synthetic 25 repos with 100 agents.
func BenchmarkReadSnapshot(b *testing.B) {
	for _, size := range []struct{ repos, agents int }{{5, 8}, {25, 100}} {
		b.Run(fmt.Sprintf("%drepos_%dagents", size.repos, size.agents), func(b *testing.B) {
			state.SetDir(b.TempDir())
			b.Cleanup(func() { state.SetDir("") })
			write(b, benchSnapshot(size.repos, size.agents))

			b.ReportAllocs()
			for b.Loop() {
				if _, err := ReadSnapshot(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func write(b *testing.B, s *model.Snapshot) {
	b.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(state.SnapshotPath(), raw, 0o644); err != nil {
		b.Fatal(err)
	}
}

func benchSnapshot(repos, agents int) *model.Snapshot {
	now := time.Now()
	s := &model.Snapshot{GeneratedAt: now, DaemonPID: os.Getpid()}
	for i := range repos {
		r := model.Repo{
			Key: fmt.Sprintf("acme/service-%d", i), Name: fmt.Sprintf("service-%d", i),
			Display: fmt.Sprintf("service-%d", i), Root: fmt.Sprintf("/home/dev/work/service-%d", i),
			Branch: "main", IsGit: true, ColorIndex: i % 8, Sigil: "◆",
			GridSlot: i, WorkspaceIDs: []string{fmt.Sprintf("w%d", i)},
		}
		for j := 0; j < agents/repos; j++ {
			r.Agents = append(r.Agents, model.Agent{
				PaneID: fmt.Sprintf("w%d:p%d", i, j), WorkspaceID: fmt.Sprintf("w%d", i),
				Name: fmt.Sprintf("agent-%d-%d", i, j), Kind: "claude",
				Status: model.StatusWorking, Task: "wiring the retry path through the client",
				TaskSource: model.TaskFromOrchestrator, StatusSince: now.Add(-time.Minute),
				StateChangeSeq: uint64(j), AgeKnown: true,
			})
		}
		r.OtherPanes = []model.Pane{{PaneID: fmt.Sprintf("w%d:p9", i), Label: "vite", Command: "vite"}}
		s.Repos = append(s.Repos, r)
	}
	for i := range min(repos, 6) {
		s.Attention = append(s.Attention, model.Attention{
			Rank: 1, Reason: model.ReasonBlocked, RepoKey: s.Repos[i].Key,
			PaneID: s.Repos[i].Agents[0].PaneID, Agent: s.Repos[i].Agents[0].Name,
			Status: model.StatusBlocked, Age: 3 * time.Minute, AgeKnown: true,
			Detail: "waiting on you: overwrite the existing migration?",
		})
	}
	return s
}
